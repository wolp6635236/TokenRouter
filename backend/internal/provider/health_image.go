package provider

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// 图片能力使用独立的缓存条目记录原因和冷却时间。
const (
	OpenAIImageRateLimitReason        = "openai_image_rate_limited"
	OpenAIImageCapabilityLossReason   = "openai_image_capability_lost"
	OpenAIImageCapabilityLossCooldown = 30 * time.Minute
	OpenAIImageGenerationRateLimitKey = "openai:image_generation"
)

// ApplyImageRateLimit 按图片能力记录 OpenAI 限流，其他能力保持当前状态。
func (s *HealthService) ApplyImageRateLimit(ctx context.Context, provider *Record, statusCode int, observe func() (bool, time.Time)) bool {
	if s == nil || provider == nil || s.providerRepo == nil {
		return false
	}
	if provider.Platform != capability.PlatformOpenAI {
		return false
	}
	// 池模式由上游提供商池负责能力切换，不能写入本地生图能力限流。
	if provider.IsPoolMode() {
		return false
	}
	if !provider.ShouldHandleErrorCode(statusCode) {
		s.options.Info("openai_image_rate_limit_skipped_by_error_code_policy", "provider_id", provider.ID, "status_code", statusCode)
		return false
	}
	matched, resetAt := observe()
	if !matched {
		return false
	}

	if err := s.providerRepo.SetModelRateLimit(ctx, provider.ID, OpenAIImageGenerationRateLimitKey, resetAt, OpenAIImageRateLimitReason); err != nil {
		s.options.Warn("openai_image_rate_limit_set_model_rate_limit_failed", "provider_id", provider.ID, "scope", OpenAIImageGenerationRateLimitKey, "error", err)
		return true
	}
	s.options.Info("openai_image_rate_limited", "provider_id", provider.ID, "scope", OpenAIImageGenerationRateLimitKey, "reset_at", resetAt, "reset_in", time.Until(resetAt).Truncate(time.Second))
	return true
}

// ApplyImageCapabilityLoss 在上游明确拒绝图片工具时暂时冷却提供商的图片调度。
func (s *HealthService) ApplyImageCapabilityLoss(ctx context.Context, provider *Record, statusCode int, capabilityLost bool) bool {
	if s == nil || provider == nil || s.providerRepo == nil {
		return false
	}
	if provider.Platform != capability.PlatformOpenAI {
		return false
	}
	if !provider.ShouldHandleErrorCode(statusCode) {
		s.options.Info("openai_image_capability_loss_skipped_by_error_code_policy", "provider_id", provider.ID, "status_code", statusCode)
		return false
	}
	if !capabilityLost {
		return false
	}

	resetAt := s.options.Now().Add(OpenAIImageCapabilityLossCooldown)
	if err := s.providerRepo.SetModelRateLimit(ctx, provider.ID, OpenAIImageGenerationRateLimitKey, resetAt, OpenAIImageCapabilityLossReason); err != nil {
		s.options.Warn("openai_image_capability_loss_set_model_rate_limit_failed", "provider_id", provider.ID, "scope", OpenAIImageGenerationRateLimitKey, "error", err)
		return true
	}
	s.options.Info("openai_image_capability_lost", "provider_id", provider.ID, "scope", OpenAIImageGenerationRateLimitKey, "reset_at", resetAt, "reset_in", time.Until(resetAt).Truncate(time.Second))
	return true
}
