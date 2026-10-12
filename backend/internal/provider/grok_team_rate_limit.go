package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	grokTeamRateLimitDefaultTTL = 10 * time.Minute
	grokTeamRateLimitMaxTTL     = time.Hour
	grokTeamRateLimitMinTTL     = 30 * time.Second
)

var globalGrokTeamModelRateLimits = &grokTeamModelRateLimitStore{
	items: make(map[string]grokTeamModelRateLimit),
}

// 以下为 Grok OAuth 的进程内 team+model 限流覆盖：同一 team_id 的某提供商在模型上被限流后，

// 其它提供商在冷却期内跳过该模型。多实例各自从本实例 429 学习，短 TTL 让状态漂移自行收敛。
type grokTeamModelRateLimit struct {
	Until time.Time
}
type grokTeamModelRateLimitStore struct {
	mu    sync.Mutex
	items map[string]grokTeamModelRateLimit
}

func GrokTeamFingerprint(teamID string) string {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(teamID)))
	return hex.EncodeToString(sum[:8])
}

func GrokTeamModelRateLimitKey(teamFingerprint, model string) string {
	return teamFingerprint + "|" + strings.ToLower(strings.TrimSpace(model))
}

func ProviderGrokTeamID(provider *Record) string {
	if provider == nil {
		return ""
	}
	return strings.TrimSpace(provider.GetCredential("team_id"))
}

// MarkGrokTeamModelRateLimit 记录 team+model 在截止时间前应被跳过；team_id 或模型为空时无操作。
func MarkGrokTeamModelRateLimit(provider *Record, model string, until time.Time) {
	if provider == nil || !provider.IsGrokOAuth() {
		return
	}
	fp := GrokTeamFingerprint(ProviderGrokTeamID(provider))
	model = strings.TrimSpace(model)
	if fp == "" || model == "" || until.IsZero() {
		return
	}
	now := time.Now()
	if !until.After(now) {
		until = now.Add(grokTeamRateLimitDefaultTTL)
	}
	maxUntil := now.Add(grokTeamRateLimitMaxTTL)
	if until.After(maxUntil) {
		until = maxUntil
	}
	key := GrokTeamModelRateLimitKey(fp, model)
	globalGrokTeamModelRateLimits.mu.Lock()
	defer globalGrokTeamModelRateLimits.mu.Unlock()
	if cur, ok := globalGrokTeamModelRateLimits.items[key]; ok && cur.Until.After(until) {
		return
	}
	globalGrokTeamModelRateLimits.items[key] = grokTeamModelRateLimit{Until: until}
	// 顺带清理已过期条目。
	for k, v := range globalGrokTeamModelRateLimits.items {
		if !v.Until.After(now) {
			delete(globalGrokTeamModelRateLimits.items, k)
		}
	}
}

// IsGrokTeamModelRateLimited 判断提供商所属团队当前是否被指定模型限流。
func IsGrokTeamModelRateLimited(provider *Record, model string, now time.Time) bool {
	if provider == nil || !provider.IsGrokOAuth() {
		return false
	}
	fp := GrokTeamFingerprint(ProviderGrokTeamID(provider))
	model = strings.TrimSpace(model)
	if fp == "" || model == "" {
		return false
	}
	key := GrokTeamModelRateLimitKey(fp, model)
	globalGrokTeamModelRateLimits.mu.Lock()
	defer globalGrokTeamModelRateLimits.mu.Unlock()
	cur, ok := globalGrokTeamModelRateLimits.items[key]
	if !ok {
		return false
	}
	if !cur.Until.After(now) {
		delete(globalGrokTeamModelRateLimits.items, key)
		return false
	}
	return true
}

// ResolveGrokTeamRateLimitUntil 从提供商限流重置时间推导团队冷却窗口并限制合理范围。
func ResolveGrokTeamRateLimitUntil(resetAt, now time.Time) time.Time {
	if resetAt.After(now.Add(grokTeamRateLimitMinTTL)) {
		maxUntil := now.Add(grokTeamRateLimitMaxTTL)
		if resetAt.After(maxUntil) {
			return maxUntil
		}
		return resetAt
	}
	return now.Add(grokTeamRateLimitDefaultTTL)
}
