package provider

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

// AntigravityModelLimitResult 模型级限流处理结果。
type AntigravityModelLimitResult struct {
	Handled      bool                                        // 是否已处理
	ShouldRetry  bool                                        // 是否等待后重试
	WaitDuration time.Duration                               // 等待时间
	SwitchError  *antigravity.AntigravityProviderSwitchError // 提供商切换错误
}

// AntigravityErrorInput 保存本次错误的模型、粘性会话和报文信息。
type AntigravityErrorInput struct {
	Context          context.Context
	Provider         *provider.Record
	Prefix           string
	Status           int
	Headers          http.Header
	Body             []byte
	RequestedModel   string
	OtherObservation HealthObservation
	Thinking         *bool
	Sticky           bool
	ClearSticky      func()
}

// AntigravityErrorObserver 组合模型窗口和提供商健康状态接口。
type AntigravityErrorObserver struct {
	Health          *provider.AntigravityHealth
	LogConfig       func() (bool, int)
	TruncateString  func(string, int) string
	ResetTime       func([]byte) *int64
	DefaultDuration func() time.Duration
	SetRateLimited  func(context.Context, int64, time.Time) error
	Other           *UpstreamHealth
}

// handleModelRateLimit 在一般错误处理之前记录模型限流。
// 仅处理 429/503，解析模型名和 retryDelay
// - MODEL_CAPACITY_EXHAUSTED: 返回 Handled=true（实际重试由 handleSmartRetry 处理）
// - RATE_LIMIT_EXCEEDED + retryDelay < 阈值: 返回 ShouldRetry=true，由调用方等待后重试
// - RATE_LIMIT_EXCEEDED + retryDelay >= 阈值: 设置模型限流 + 清除粘性会话 + 返回 SwitchError。
func (s *AntigravityErrorObserver) handleModelRateLimit(p *AntigravityErrorInput) *AntigravityModelLimitResult {
	if p.Status != 429 && p.Status != 503 {
		return &AntigravityModelLimitResult{Handled: false}
	}

	info := antigravity.ParseAntigravitySmartRetryInfo(p.Body)
	if info == nil || info.ModelName == "" {
		return &AntigravityModelLimitResult{Handled: false}
	}

	// MODEL_CAPACITY_EXHAUSTED：模型容量不足，所有提供商共享同一容量池
	// 切换提供商无意义，不设置模型限流（实际重试由 handleSmartRetry 处理）
	if info.IsModelCapacityExhausted {
		log.Printf("%s status=%d model_capacity_exhausted model=%s (not switching provider, retry handled by smart retry)",
			p.Prefix, p.Status, info.ModelName)
		return &AntigravityModelLimitResult{
			Handled: true,
		}
	}

	// RATE_LIMIT_EXCEEDED: < antigravityRateLimitThreshold: 等待后重试
	if info.RetryDelay < antigravity.AntigravityRateLimitThreshold {
		logging.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limit_wait model=%s wait=%v",
			p.Prefix, p.Status, info.ModelName, info.RetryDelay)
		return &AntigravityModelLimitResult{
			Handled:      true,
			ShouldRetry:  true,
			WaitDuration: info.RetryDelay,
		}
	}

	// RATE_LIMIT_EXCEEDED: >= antigravityRateLimitThreshold: 设置限流 + 清除粘性会话 + 切换提供商
	s.setModelRateLimitAndClearSession(p, info)

	return &AntigravityModelLimitResult{
		Handled: true,
		SwitchError: &antigravity.AntigravityProviderSwitchError{
			OriginalProviderID: p.Provider.ID,
			RateLimitedModel:   info.ModelName,
			IsStickySession:    p.Sticky,
		},
	}
}

// setModelRateLimitAndClearSession 设置模型限流并清除粘性会话。
func (s *AntigravityErrorObserver) setModelRateLimitAndClearSession(p *AntigravityErrorInput, info *antigravity.AntigravitySmartRetryInfo) {
	resetAt := time.Now().Add(info.RetryDelay)
	logging.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limited model=%s provider=%d reset_in=%v",
		p.Prefix, p.Status, info.ModelName, p.Provider.ID, info.RetryDelay)

	s.Health.SetAntigravityModelRateLimits(p.Context, s.Health.Store, p.Provider, info.ModelName, p.Prefix, p.Status, resetAt, false)

	// 清除粘性会话绑定
	if p.ClearSticky != nil {
		p.ClearSticky()
	}
}

// Observe 解析单次失败并记录提供商状态。
func (s *AntigravityErrorObserver) Observe(p AntigravityErrorInput) *AntigravityModelLimitResult {
	ctx, prefix, value := p.Context, p.Prefix, p.Provider
	statusCode, body, requestedModel := p.Status, p.Body, p.RequestedModel
	// 遵守自定义错误码策略：未命中则跳过所有限流处理
	if !value.ShouldHandleErrorCode(statusCode) {
		return nil
	}
	// 模型级限流处理（优先）
	result := s.handleModelRateLimit(&p)
	if result.Handled {
		return result
	}

	// 503 仅处理模型限流（MODEL_CAPACITY_EXHAUSTED），非模型限流不做额外处理
	// 普通 503 按上游容量错误处理。
	if statusCode == 503 {
		return nil
	}

	// 429 先尝试解析模型限流，解析失败时按提供商限流处理。
	if statusCode == 429 {
		if logBody, maxBytes := s.LogConfig(); logBody {
			logging.LegacyPrintf("service.antigravity_gateway", "[Antigravity-Debug] 429 response body: %s", s.TruncateString(string(body), maxBytes))
		}

		resetAt := s.ResetTime(body)
		defaultDur := s.DefaultDuration()

		// 尝试解析模型 key 并设置模型级限流
		//
		// 注意：requestedModel 可能是"映射前"的请求模型名（例如 claude-opus-4-6），
		// 调度与限流判定使用的是 Antigravity 最终模型名（包含映射与 thinking 后缀）。
		// 此处使用最终模型名记录限流，后续调度按同一名称检查。
		modelKey := FinalAntigravityModel(value, requestedModel, p.Thinking)
		if strings.TrimSpace(modelKey) == "" {
			// 极少数情况下无法映射（理论上不应发生：能转发成功说明映射已通过），
			// 缺少最终模型名时使用回退模型记录限流。
			modelKey = antigravity.NormalizeAntigravityModelName(requestedModel)
		}
		if modelKey != "" {
			ra := s.resolveResetTime(resetAt, defaultDur)
			if !s.Health.SetAntigravityModelRateLimits(ctx, s.Health.Store, value, modelKey, prefix, statusCode, ra, false) {
				logging.LegacyPrintf("service.antigravity_gateway", "%s status=429 model_rate_limit_set_failed model=%s", prefix, modelKey)
			} else {
				logging.LegacyPrintf("service.antigravity_gateway", "%s status=429 model_rate_limited model=%s provider=%d reset_at=%v reset_in=%v",
					prefix, modelKey, value.ID, ra.Format("15:04:05"), time.Until(ra).Truncate(time.Second))
			}
			return nil
		}

		// 模型 key 解析失败时记录提供商限流。
		ra := s.resolveResetTime(resetAt, defaultDur)
		logging.LegacyPrintf("service.antigravity_gateway", "%s status=429 rate_limited provider=%d reset_at=%v reset_in=%v (fallback)",
			prefix, value.ID, ra.Format("15:04:05"), time.Until(ra).Truncate(time.Second))
		if err := s.SetRateLimited(ctx, value.ID, ra); err != nil {
			logging.LegacyPrintf("service.antigravity_gateway", "%s status=429 rate_limit_set_failed provider=%d error=%v", prefix, value.ID, err)
		}
		return nil
	}
	// 其他错误码继续使用 rateLimitService
	if s.Other == nil {
		return nil
	}
	shouldDisable := s.Other.ApplyUpstreamError(ctx, value, p.OtherObservation).StopScheduling
	if shouldDisable {
		logging.LegacyPrintf("service.antigravity_gateway", "%s status=%d marked_error", prefix, statusCode)
	}
	return nil
}

// resolveResetTime 根据解析的重置时间或默认时长计算重置时间点。
func (s *AntigravityErrorObserver) resolveResetTime(resetAt *int64, defaultDur time.Duration) time.Time {
	if resetAt != nil {
		return time.Unix(*resetAt, 0)
	}
	return time.Now().Add(defaultDur)
}

// AntigravityFallbackDuration 保持配置分钟数及环境秒数覆盖的原优先级。
func AntigravityFallbackDuration(minutes int, raw string) time.Duration {
	duration := antigravity.AntigravityDefaultRateLimitDuration
	if minutes > 0 {
		duration = time.Duration(minutes) * time.Minute
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds > 0 {
		duration = time.Duration(seconds) * time.Second
	}
	return duration
}
