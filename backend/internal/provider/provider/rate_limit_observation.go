package provider

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	anthropicupstream "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini"
	openaiupstream "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// RateLimitObserver 解析供应商限流响应并调用健康状态写入函数。
type RateLimitObserver struct {
	Health          *providercore.HealthService
	Plans           providercore.OpenAIPlanWriter
	RetryOpenAI     func(*providercore.Record, http.Header, []byte) bool
	NextGeminiDaily func() *int64
}

// Observe429 处理 429 限流错误
// 解析响应头获取重置时间，标记提供商为限流状态。
func (s *RateLimitObserver) Observe429(ctx context.Context, provider *providercore.Record, headers http.Header, responseBody []byte) {
	// Spark 影子的限流状态由 /wham/usage 的 codex_bengalfox 用量驱动。
	// /responses 429 中的 x-codex-* 和 usage_limit_reached 属于全局窗口，不能用于暂停仍有配额的影子。
	// 此处跳过影子，由用量查询维护 codex_* 快照，并由调度资格检查处理额度耗尽。
	if provider.IsShadow() {
		return
	}
	if provider.Platform == capability.PlatformOpenAI && provider.IsOpenAIOAuthLike() {
		if s.RetryOpenAI != nil && s.RetryOpenAI(provider, headers, responseBody) {
			return
		}
	}
	// 国产供应商（kimi/zhipu/deepseek）的 429 走专用可恢复路径：余额不足 → 临时停调，
	// Coding Plan 窗口耗尽 → 冷却到快照重置点。未命中则继续默认 429 逻辑。
	if provider.IsCNProvider() {
		if s.observeCNQuota(ctx, provider, responseBody) {
			return
		}
	}
	// 1. OpenAI 平台：优先尝试解析 x-codex-* 响应头（用于 rate_limit_exceeded）
	if provider.Platform == capability.PlatformOpenAI {
		providercore.PersistOpenAIObservedPlan(ctx, s.Plans, provider, openaiupstream.ParseUsageLimitPlanType(responseBody), slog.Info, slog.Warn)
		s.PersistCodexSnapshot(ctx, provider, headers)
		if resetAt := providercore.OpenAI429ResetTime(openaiupstream.ParseCodexRateLimitHeaders(headers), time.Now, slog.Info); resetAt != nil {
			if !s.Health.ApplyObservedRateLimit(ctx, provider, *resetAt) {
				return
			}
			slog.Info("openai_provider_rate_limited", "provider_id", provider.ID, "reset_at", *resetAt)
			return
		}
	}

	// 2. Anthropic 平台：尝试解析 per-window 头（5h / 7d），选择实际触发的窗口
	if result := anthropicupstream.CalculateRateLimitReset(headers, slog.Info); result != nil {
		if !s.Health.ApplyObservedRateLimit(ctx, provider, result.ResetAt) {
			return
		}

		// 更新 session window：优先使用 5h-reset 头精确计算，否则从 resetAt 反推
		windowEnd := result.ResetAt
		if result.FiveHourReset != nil {
			windowEnd = *result.FiveHourReset
		}
		s.Health.UpdateRejectedSessionWindow(ctx, provider, windowEnd)

		slog.Info("anthropic_provider_rate_limited", "provider_id", provider.ID, "reset_at", result.ResetAt, "reset_in", time.Until(result.ResetAt).Truncate(time.Second))
		return
	}

	// 3. 尝试从响应头解析重置时间（Anthropic 聚合头，向后兼容）
	resetTimestamp := headers.Get("anthropic-ratelimit-unified-reset")

	// 4. 如果响应头没有，尝试从响应体解析（OpenAI usage_limit_reached, Gemini）
	if resetTimestamp == "" {
		switch provider.Platform {
		case capability.PlatformOpenAI:
			// 尝试解析 OpenAI 的 usage_limit_reached 错误
			if resetAt := openaiupstream.ParseUsageLimitResetTime(responseBody, time.Now); resetAt != nil {
				resetTime := time.Unix(*resetAt, 0)
				if !s.Health.ApplyObservedRateLimit(ctx, provider, resetTime) {
					return
				}
				slog.Info("provider_rate_limited", "provider_id", provider.ID, "platform", provider.Platform, "reset_at", resetTime, "reset_in", time.Until(resetTime).Truncate(time.Second))
				return
			}
		case capability.PlatformGemini, capability.PlatformAntigravity:
			// 尝试解析 Gemini 格式（用于其他平台）
			if resetAt := gemini.ParseGeminiRateLimitResetTime(responseBody, s.NextGeminiDaily); resetAt != nil {
				resetTime := time.Unix(*resetAt, 0)
				if !s.Health.ApplyObservedRateLimit(ctx, provider, resetTime) {
					return
				}
				slog.Info("provider_rate_limited", "provider_id", provider.ID, "platform", provider.Platform, "reset_at", resetTime, "reset_in", time.Until(resetTime).Truncate(time.Second))
				return
			}
		}

		// Anthropic 的 429 缺少重置时间时，使用管理端配置的秒级冷却。
		// 这类响应可能来自 Extra usage required 等错误，短期停调后可重新尝试，管理员也可关闭该冷却。
		if provider.Platform == capability.PlatformAnthropic {
			slog.Warn("rate_limit_429_no_reset_time",
				"provider_id", provider.ID,
				"platform", provider.Platform,
				"reason", "no rate limit reset time in headers, likely not a real rate limit")
			s.Health.Apply429Fallback(ctx, provider, "anthropic_no_reset_time")
			return
		}

		// 其他平台缺少重置时间时，使用配置的秒级冷却。
		s.Health.Apply429Fallback(ctx, provider, "no_reset_time")
		return
	}

	// 解析 Unix 时间戳
	ts, err := strconv.ParseInt(resetTimestamp, 10, 64)
	if err != nil {
		slog.Warn("rate_limit_reset_parse_failed", "reset_timestamp", resetTimestamp, "error", err)
		s.Health.Apply429Fallback(ctx, provider, "reset_parse_failed")
		return
	}

	resetAt := time.Unix(ts, 0)

	// 标记限流状态
	if !s.Health.ApplyObservedRateLimit(ctx, provider, resetAt) {
		return
	}

	// 根据重置时间反推 5h 窗口
	windowEnd := resetAt
	s.Health.UpdateRejectedSessionWindow(ctx, provider, windowEnd)

	slog.Info("provider_rate_limited", "provider_id", provider.ID, "reset_at", resetAt)
}

// PersistCodexSnapshot 跳过影子提供商和空响应头，将有效观测写入提供商快照。
func (s *RateLimitObserver) PersistCodexSnapshot(ctx context.Context, value *providercore.Record, headers http.Header) {
	if s == nil || s.Health == nil || value == nil || headers == nil || value.IsShadow() {
		return
	}
	s.Health.PersistCodexObservation(ctx, value, openaiupstream.ParseCodexRateLimitHeaders(headers))
}

func (s *RateLimitObserver) observeCNQuota(ctx context.Context, value *providercore.Record, body []byte) bool {
	if !value.IsCNProvider() {
		return false
	}
	if upstream.CNResponseIndicatesInsufficientBalance(body) {
		s.Health.ApplyCNInsufficientBalance(ctx, value, upstream.ExtractErrorMessage(body))
		return true
	}
	return s.Health.ApplyCNQuotaSnapshotCooldown(ctx, value)
}
