package provider

import (
	"context"
	"net/http"
	"time"
)

// CodexSparkRateLimitReason 返回 Spark 模型限流的原因。
const CodexSparkRateLimitReason = "openai_codex_spark_rate_limit"

// ApplySparkRateLimit 将 Spark 配额窗口写入模型限流。
// x-codex-* 使用率和 reset 时间属于 Spark 模型，其他模型继续按各自的额度调度。
func (s *HealthService) ApplySparkRateLimit(ctx context.Context, provider *Record, modelKey string, statusCode int, spark bool, observe func() (OpenAI429Disposition, *time.Time)) bool {
	if s == nil || provider == nil || s.providerRepo == nil || statusCode != http.StatusTooManyRequests || !provider.IsOpenAIOAuthLike() {
		return false
	}
	if !spark || !provider.ShouldHandleErrorCode(statusCode) {
		return false
	}

	if modelKey == "" {
		return false
	}
	now := s.options.Now()
	disposition, resetAt := observe()
	// Spark 只有明确耗尽 5h/7d 窗口时才能使用上游长 reset；普通瞬时 429
	// Spark 使用短时冷却，即使响应携带全局 reset 头也按短时规则处理。
	if disposition != OpenAI429Quota5h && disposition != OpenAI429Quota7d {
		resetAt = nil
	}
	if resetAt == nil || !resetAt.After(now) {
		cooldown, ok := s.Fallback429Cooldown(ctx, provider)
		if !ok || cooldown <= 0 {
			cooldown = time.Duration(DefaultRateLimit429CooldownSeconds) * time.Second
		}
		reset := now.Add(cooldown)
		resetAt = &reset
	}
	if err := s.providerRepo.SetModelRateLimit(ctx, provider.ID, modelKey, *resetAt, CodexSparkRateLimitReason); err != nil {
		s.options.Warn("openai_codex_spark_model_rate_limit_set_failed", "provider_id", provider.ID, "model", modelKey, "error", err)
	}
	s.options.Info("openai_codex_spark_model_rate_limited", "provider_id", provider.ID, "model", modelKey, "reset_at", *resetAt)
	return true
}
