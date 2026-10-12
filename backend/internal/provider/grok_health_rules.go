package provider

import (
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/usageview"
)

const (
	grokRateLimitFallbackCooldown    = 2 * time.Minute
	grokRateLimitRepeatCooldown      = 10 * time.Minute
	grokRateLimitSustainedCooldown   = 30 * time.Minute
	grokRateLimitMaxAdaptiveCooldown = time.Hour
	grokRateLimitBackoffQuietPeriod  = time.Hour
	grokMaxSchedulingResetHorizon    = 25 * time.Hour
)

func NormalizeGrokExhaustedWindowResets(snapshot *usageview.QuotaSnapshot, resetAt, now time.Time) {
	if snapshot == nil || !resetAt.After(now) {
		return
	}
	for _, window := range []*usageview.QuotaWindow{snapshot.Requests, snapshot.Tokens} {
		if window == nil || window.Remaining == nil || *window.Remaining > 0 {
			continue
		}
		candidate := time.Time{}
		if window.ResetUnix != nil && *window.ResetUnix > 0 {
			candidate = time.Unix(*window.ResetUnix, 0)
		} else if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(window.ResetAt)); err == nil {
			candidate = parsed
		}
		if !candidate.After(now) {
			candidate = resetAt
		}
		resetUnix := candidate.Unix()
		window.ResetUnix = &resetUnix
		window.ResetAt = candidate.UTC().Format(time.RFC3339)
	}
}

func GrokRateLimitResetAt(snapshot *usageview.QuotaSnapshot, now time.Time) (time.Time, bool) {
	if snapshot == nil {
		return time.Time{}, false
	}

	// Retry-After 加上观测时间得到固定的重试时刻，多次读取共用这个时刻。
	retryAfterExpired := false
	var resetAt time.Time
	if snapshot.RetryAfterSeconds != nil && *snapshot.RetryAfterSeconds > 0 {
		observedAt := now
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(snapshot.UpdatedAt)); err == nil {
			observedAt = parsed
		}
		retryAfterResetAt := observedAt.Add(time.Duration(*snapshot.RetryAfterSeconds) * time.Second)
		if retryAfterResetAt.After(now) {
			resetAt = retryAfterResetAt
		} else {
			retryAfterExpired = true
		}
	}

	exhausted := false
	for _, window := range []*usageview.QuotaWindow{snapshot.Requests, snapshot.Tokens} {
		if window == nil || window.Remaining == nil || *window.Remaining > 0 {
			continue
		}
		exhausted = true
		candidate := time.Time{}
		if window.ResetUnix != nil && *window.ResetUnix > 0 {
			candidate = time.Unix(*window.ResetUnix, 0)
		} else if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(window.ResetAt)); err == nil {
			candidate = parsed
		}
		if candidate.After(now) && candidate.After(resetAt) {
			resetAt = candidate
		}
	}
	if !resetAt.IsZero() {
		return resetAt, true
	}
	// Retry-After 加上快照时间得到固定的重试时刻。
	// 该时刻已过期时，仍可使用更晚的窗口重置时间。
	if retryAfterExpired {
		return time.Time{}, false
	}
	if exhausted || snapshot.StatusCode == 429 {
		return now.Add(grokRateLimitFallbackCooldown), true
	}
	return time.Time{}, false
}

func GrokRateLimitResetAtForProvider(provider *Record, snapshot *usageview.QuotaSnapshot, now time.Time) (time.Time, bool) {
	resetAt, limited := GrokRateLimitResetAt(snapshot, now)
	if !limited || (provider == nil || !provider.IsGrokOAuth()) || snapshot == nil || snapshot.StatusCode != 429 {
		return resetAt, limited
	}
	if provider.RateLimitedAt == nil || provider.RateLimitResetAt == nil {
		return resetAt, true
	}
	previousResetAt := *provider.RateLimitResetAt
	if previousResetAt.After(now) || now.Sub(previousResetAt) > grokRateLimitBackoffQuietPeriod {
		return resetAt, true
	}
	previousCooldown := previousResetAt.Sub(*provider.RateLimitedAt)
	if previousCooldown <= 0 {
		return resetAt, true
	}

	adaptiveCooldown := grokRateLimitRepeatCooldown
	switch {
	case previousCooldown >= grokRateLimitSustainedCooldown:
		adaptiveCooldown = grokRateLimitMaxAdaptiveCooldown
	case previousCooldown >= grokRateLimitRepeatCooldown:
		adaptiveCooldown = grokRateLimitSustainedCooldown
	}
	adaptiveResetAt := now.Add(adaptiveCooldown)
	if adaptiveResetAt.After(resetAt) {
		resetAt = adaptiveResetAt
	}
	return resetAt, true
}

func NormalizeGrokRateLimitResetAt(provider *Record, resetAt, now time.Time) time.Time {
	if !resetAt.After(now) {
		resetAt = now.Add(grokRateLimitFallbackCooldown)
	}
	if provider != nil && provider.RateLimitResetAt != nil && provider.RateLimitResetAt.After(resetAt) {
		resetAt = *provider.RateLimitResetAt
	}
	return resetAt
}

func IsSuccessfulGrokRateLimitRecovery(provider *Record, snapshot *usageview.QuotaSnapshot) bool {
	return (provider != nil && provider.IsGrokOAuth()) &&
		provider.RateLimitedAt != nil &&
		provider.RateLimitResetAt != nil &&
		snapshot != nil &&
		snapshot.StatusCode >= 200 &&
		snapshot.StatusCode < 300
}

// BuildGrokSchedulerExtraUpdates 派生 EvaluateProviderSchedulingThreshold 使用的
// grok_sched_* 调度快照，包括利用率百分比与重置时间。
// 利用率取请求数与令牌数窗口中约束最强的一项。
func BuildGrokSchedulerExtraUpdates(snapshot *usageview.QuotaSnapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	util, reset, ok := GrokSnapshotUtilization(snapshot)
	if !ok {
		return nil
	}
	updates := map[string]any{
		"grok_sched_utilization":      util,
		"grok_sched_usage_updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if reset != nil {
		// 防御：调度阈值暂停时长由 grok_sched_reset_at 决定。若上游返回脏的
		// reset 头（例如把相对毫秒 "6000" 误当相对秒解析出 ~33h 的未来时刻），
		// 不设上限会把耗尽提供商长时间锁死。xAI 配额窗口不会超过一天，因此对
		// 未来时刻做 grokMaxSchedulingResetHorizon 钳制；过去/无效值直接不写。
		now := time.Now()
		if reset.After(now) {
			capped := *reset
			if horizon := now.Add(grokMaxSchedulingResetHorizon); capped.After(horizon) {
				capped = horizon
			}
			updates["grok_sched_reset_at"] = capped.UTC().Format(time.RFC3339)
		}
	}
	return updates
}

// GrokSnapshotUtilization 返回请求数与令牌数额度窗口中的最高利用率（0 到 100），
// 以及该窗口的重置时间。
func GrokSnapshotUtilization(snapshot *usageview.QuotaSnapshot) (float64, *time.Time, bool) {
	if snapshot == nil {
		return 0, nil, false
	}
	best := -1.0
	var bestReset *time.Time
	consider := func(window *usageview.QuotaWindow) {
		if window == nil || window.Limit == nil || *window.Limit <= 0 || window.Remaining == nil {
			return
		}
		remaining := max(*window.Remaining, 0)
		util := (1 - float64(remaining)/float64(*window.Limit)) * 100
		if util < 0 {
			util = 0
		}
		if util > 100 {
			util = 100
		}
		if util > best {
			best = util
			if window.ResetUnix != nil {
				t := time.Unix(*window.ResetUnix, 0).UTC()
				bestReset = &t
			} else {
				bestReset = nil
			}
		}
	}
	consider(snapshot.Requests)
	consider(snapshot.Tokens)
	if best < 0 {
		return 0, nil, false
	}
	return best, bestReset, true
}
