package provider

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// grokFreeQuotaGateCacheMinSweepAge 是条目可被清理前的最短存活时间。
const grokFreeQuotaGateCacheMinSweepAge = 5 * time.Minute

// FreeQuotaOptions 是免费档位调度准入的配置参数。
type FreeQuotaOptions struct {
	Enabled      bool
	TokenLimit   int64
	Percent      int
	WindowHours  int
	CacheSeconds int
}

// FreeQuotaCandidate 不携带凭据，只表达已判定的资格和提供商 ID。
type FreeQuotaCandidate struct {
	ID       int64
	Eligible bool
}

// FreeQuotaGate 拥有一个选择作用域的缓存与刷新去重；后台任务由应用管理。
type FreeQuotaGate struct {
	cache      sync.Map
	inFlight   sync.Map
	options    func() FreeQuotaOptions
	load       func(context.Context, []int64, time.Time) (map[int64]int64, error)
	background func(string, func()) bool
	now        func() time.Time
	observe    func(bool, string, ...any)
	metrics    *FreeQuotaMetrics
}

type grokFreeQuotaGateSettings struct {
	limitTokens int64
	gateTokens  int64
	window      time.Duration
	cacheTTL    time.Duration
}

type grokFreeQuotaGateCacheEntry struct {
	tokens    int64
	checkedAt time.Time
	known     bool
}

// FreeQuotaMetrics 保留各选择作用域共用的累计观测。
type FreeQuotaMetrics struct {
	QueryFailureTotal atomic.Int64
	BlockedTotal      atomic.Int64
}

// NewFreeQuotaGate 构造免费额度检查器，配置读取和异步回源在调用时执行。
func NewFreeQuotaGate(options func() FreeQuotaOptions, load func(context.Context, []int64, time.Time) (map[int64]int64, error), background func(string, func()) bool, now func() time.Time, observe func(bool, string, ...any), metrics *FreeQuotaMetrics) *FreeQuotaGate {
	if now == nil {
		now = time.Now
	}
	if metrics == nil {
		metrics = &FreeQuotaMetrics{}
	}
	if observe == nil {
		observe = func(bool, string, ...any) {}
	}
	if options == nil {
		options = func() FreeQuotaOptions { return FreeQuotaOptions{} }
	}
	return &FreeQuotaGate{options: options, load: load, background: background, now: now, observe: observe, metrics: metrics}
}

func resolveGrokFreeQuotaGateSettings(cfg FreeQuotaOptions) (grokFreeQuotaGateSettings, bool) {
	if !cfg.Enabled {
		return grokFreeQuotaGateSettings{}, false
	}
	limit := cfg.TokenLimit
	percent := cfg.Percent
	windowHours := cfg.WindowHours
	cacheSeconds := cfg.CacheSeconds
	if limit <= 0 || percent < 1 || percent > 100 || windowHours <= 0 || cacheSeconds < 0 {
		return grokFreeQuotaGateSettings{}, false
	}
	gate := calculateGrokFreeQuotaSoftGateTokens(limit, percent)
	if gate <= 0 {
		return grokFreeQuotaGateSettings{}, false
	}
	return grokFreeQuotaGateSettings{
		limitTokens: limit,
		gateTokens:  gate,
		window:      time.Duration(windowHours) * time.Hour,
		cacheTTL:    time.Duration(cacheSeconds) * time.Second,
	}, true
}

func calculateGrokFreeQuotaSoftGateTokens(limit int64, percent int) int64 {
	if limit <= 0 || percent <= 0 {
		return 0
	}
	return (limit/100)*int64(percent) + (limit%100)*int64(percent)/100
}

// IsExplicitGrokFreeOAuthProvider 判断免费层软门禁是否适用；只有凭据或 extra 中
// subscription_tier/plan_type 明确等于 free 的 OAuth 提供商命中，推断免费、basic 或空套餐均不命中。
func IsExplicitGrokFreeOAuthProvider(provider *Record) bool {
	if provider == nil || !provider.IsGrokOAuth() {
		return false
	}
	for _, tier := range []string{
		provider.GetCredential("subscription_tier"),
		provider.GetCredential("plan_type"),
		provider.GetExtraString("subscription_tier"),
		provider.GetExtraString("plan_type"),
	} {
		if strings.EqualFold(strings.TrimSpace(tier), "free") {
			return true
		}
	}
	return false
}

// Blocked 返回已知超限提供商；未命中与失败保持故障放行，并在后台加载。
func (g *FreeQuotaGate) Blocked(providers []FreeQuotaCandidate) map[int64]bool {
	if g == nil {
		return nil
	}
	settings, enabled := resolveGrokFreeQuotaGateSettings(g.options())
	if !enabled || len(providers) == 0 || g.load == nil {
		return nil
	}
	now := g.now().UTC()
	tokensByID := make(map[int64]int64)
	missingIDs := make([]int64, 0, len(providers))
	seenMissing := make(map[int64]struct{})
	for i := range providers {
		provider := &providers[i]
		if !provider.Eligible || provider.ID <= 0 {
			continue
		}
		if cached, ok := g.cache.Load(provider.ID); ok {
			entry, valid := cached.(grokFreeQuotaGateCacheEntry)
			if valid {
				age := now.Sub(entry.checkedAt)
				// cacheTTL 为 0 表示已知条目不过期；首次未命中仍放行并安排刷新。
				fresh := settings.cacheTTL <= 0 || (age >= 0 && age < settings.cacheTTL)
				if fresh {
					if entry.known {
						tokensByID[provider.ID] = entry.tokens
					}
					continue
				}
			}
		}
		// 缓存未命中或过期时本次请求放行，并异步刷新。
		if _, exists := seenMissing[provider.ID]; !exists {
			seenMissing[provider.ID] = struct{}{}
			missingIDs = append(missingIDs, provider.ID)
		}
	}

	if len(missingIDs) > 0 {
		g.refresh(settings, missingIDs)
	}

	blocked := make(map[int64]bool)
	for id, tokens := range tokensByID {
		if tokens >= settings.gateTokens {
			blocked[id] = true
		}
	}
	return blocked
}

// refresh 合并同一缓存作用域的在途加载，不改变查询的独立 context。
func (g *FreeQuotaGate) refresh(settings grokFreeQuotaGateSettings, providerIDs []int64) {
	toFetch := make([]int64, 0, len(providerIDs))
	for _, id := range providerIDs {
		if _, loaded := g.inFlight.LoadOrStore(id, struct{}{}); !loaded {
			toFetch = append(toFetch, id)
		}
	}
	if len(toFetch) == 0 {
		return
	}

	window := settings.window
	gateTokens := settings.gateTokens
	limitTokens := settings.limitTokens
	cacheTTL := settings.cacheTTL
	release := func() {
		for _, id := range toFetch {
			g.inFlight.Delete(id)
		}
	}
	// 应用关闭前等待查询任务完成，查询使用独立 context。
	if !g.background("provider/free_quota_gate.go:stats_refresh", func() {
		defer release()
		now := g.now().UTC()
		statsByID, err := g.load(context.Background(), toFetch, now.Add(-window))
		if err != nil {
			g.metrics.QueryFailureTotal.Add(1)
			// 保存负缓存防止热路径反复查询；known=false 会持续放行，直到成功刷新。
			for _, providerID := range toFetch {
				g.cache.Store(providerID, grokFreeQuotaGateCacheEntry{checkedAt: now})
			}
			g.observe(true, "grok_free_quota_soft_gate_stats_failed",
				"provider_count", len(toFetch),
				"window_hours", window.Hours(),
				"error", err)
			sweepGrokFreeQuotaGateCache(&g.cache, now, cacheTTL)
			return
		}
		for _, providerID := range toFetch {
			tokens := int64(0)
			if stats := statsByID[providerID]; stats > 0 {
				tokens = stats
			}
			g.cache.Store(providerID, grokFreeQuotaGateCacheEntry{tokens: tokens, checkedAt: now, known: true})
			if tokens >= gateTokens {
				g.metrics.BlockedTotal.Add(1)
				g.observe(false, "grok_free_quota_soft_gate_blocked",
					"provider_id", providerID,
					"tokens", tokens,
					"gate_tokens", gateTokens,
					"limit_tokens", limitTokens,
					"window_hours", window.Hours())
			}
		}
		sweepGrokFreeQuotaGateCache(&g.cache, now, cacheTTL)
	}) {
		release()
	}
}

// sweepGrokFreeQuotaGateCache 回收超过 TTL 较久的条目，删除或离开免费档位的提供商可随之释放内存。
// 仍活跃的提供商会在下次未命中时重新填充。
func sweepGrokFreeQuotaGateCache(cache *sync.Map, now time.Time, cacheTTL time.Duration) {
	if cache == nil || cacheTTL <= 0 {
		return
	}
	maxAge := max(cacheTTL*20, grokFreeQuotaGateCacheMinSweepAge)
	cache.Range(func(key, value any) bool {
		entry, ok := value.(grokFreeQuotaGateCacheEntry)
		if !ok || now.Sub(entry.checkedAt) > maxAge {
			cache.Delete(key)
		}
		return true
	})
}
