package provider

import (
	"context"
	"strconv"
	"time"
)

const (
	ErrorPolicyNone            ErrorPolicyResult = iota // 未命中任何策略，继续默认逻辑
	ErrorPolicyCustomSkipped                            // 自定义错误码开启但未命中，返回通用错误
	ErrorPolicyCustomMatched                            // 自定义错误码命中，停止调度
	ErrorPolicyTempUnscheduled                          // 临时不可调度规则命中
	ErrorPolicyPoolBypassed                             // 池模式跳过默认本地状态，继续响应分类
)

// ErrorPolicyResult 表示错误策略检查的结果。
type ErrorPolicyResult int

// UpstreamErrorDecision 汇总配置的错误策略和默认的提供商状态处理结果。
// 网关必须使用 Policy 区分池模式绕过与自定义错误码未命中，不能只依赖 StopScheduling。
type UpstreamErrorDecision struct {
	Policy         ErrorPolicyResult
	StopScheduling bool
}

// ShouldReturnGenericError 表示应按自定义错误码约定返回通用 500，且不切换提供商。
func (d UpstreamErrorDecision) ShouldReturnGenericError() bool {
	return d.Policy == ErrorPolicyCustomSkipped
}

// ShouldFailover 表示当前请求应切换提供商。池模式配置的重试状态码可以把原本的
// 配置的错误策略命中时切换提供商，其他状态按入口的默认规则处理。
func (d UpstreamErrorDecision) ShouldFailover(provider *Record, statusCode int, defaultFailover bool) bool {
	if d.Policy == ErrorPolicyCustomSkipped {
		return false
	}
	if d.Policy == ErrorPolicyCustomMatched || d.Policy == ErrorPolicyTempUnscheduled {
		return true
	}
	if d.Policy == ErrorPolicyPoolBypassed {
		return defaultFailover || (provider != nil && provider.IsPoolModeRetryableStatus(statusCode))
	}
	return defaultFailover || d.StopScheduling
}

// ShouldFailoverWithDefaults 分别保留普通提供商的入口既有切号规则和池模式的上游错误分类。
// 各入口共用配置的错误策略，默认规则决定其他错误是否切换提供商。
func (d UpstreamErrorDecision) ShouldFailoverWithDefaults(
	provider *Record,
	statusCode int,
	nonPoolDefault bool,
	poolDefault bool,
) bool {
	switch d.Policy {
	case ErrorPolicyCustomSkipped:
		return false
	case ErrorPolicyCustomMatched, ErrorPolicyTempUnscheduled:
		return true
	case ErrorPolicyPoolBypassed:
		return poolDefault || (provider != nil && provider.IsPoolModeRetryableStatus(statusCode))
	default:
		return nonPoolDefault
	}
}

// RetryableOnSameProvider 判断池模式错误能否在当前提供商重试，配置的错误策略命中时返回 false。
func (d UpstreamErrorDecision) RetryableOnSameProvider(provider *Record, statusCode int) bool {
	return d.Policy == ErrorPolicyPoolBypassed && provider != nil && provider.IsPoolModeRetryableStatus(statusCode)
}

// ErrorDecisionWithoutPersistence 在错误状态服务未注入时保留纯配置决策。
// 此处通过自定义错误码和池模式判断重试资格，持久化由后续错误处理完成。
func ErrorDecisionWithoutPersistence(provider *Record, statusCode int) UpstreamErrorDecision {
	decision := UpstreamErrorDecision{Policy: ErrorPolicyNone}
	if provider == nil {
		return decision
	}
	if provider.IsCustomErrorCodesEnabled() {
		if provider.ShouldHandleErrorCode(statusCode) {
			decision.Policy = ErrorPolicyCustomMatched
			decision.StopScheduling = true
		} else {
			decision.Policy = ErrorPolicyCustomSkipped
		}
		return decision
	}
	if provider.IsPoolMode() {
		decision.Policy = ErrorPolicyPoolBypassed
	}
	return decision
}

// CheckErrorPolicy 检查自定义错误码和临时不可调度规则。
// 自定义错误码开启时覆盖后续所有逻辑（包括临时不可调度）。
func (s *HealthService) CheckErrorPolicy(ctx context.Context, provider *Record, statusCode int, responseBody []byte, model string, escalateRepeated401 bool) ErrorPolicyResult {
	if provider == nil {
		return ErrorPolicyNone
	}
	if provider.IsCustomErrorCodesEnabled() {
		if provider.ShouldHandleErrorCode(statusCode) {
			return ErrorPolicyCustomMatched
		}
		s.options.Info("provider_error_code_skipped", "provider_id", provider.ID, "status_code", statusCode)
		return ErrorPolicyCustomSkipped
	}
	if provider.IsPoolMode() {
		// 池模式命中管理员配置的临时停调规则时，按该规则处理 401。
		if s.TryTempUnschedulable(ctx, provider, statusCode, responseBody, false, model) {
			return ErrorPolicyTempUnscheduled
		}
		return ErrorPolicyPoolBypassed
	}
	// 普通提供商使用全局过载处理规则，提供商配置的策略优先。
	if statusCode == 529 {
		return ErrorPolicyCustomMatched
	}
	if s.TryTempUnschedulable(ctx, provider, statusCode, responseBody, escalateRepeated401, model) {
		return ErrorPolicyTempUnscheduled
	}
	return ErrorPolicyNone
}

// ApplyAuthenticationFailure 处理认证类错误(401/403)，停止提供商调度。
func (s *HealthService) ApplyAuthenticationFailure(ctx context.Context, provider *Record, errorMsg string) {
	s.notifyProviderSchedulingBlocked(provider, time.Time{}, "auth_error")
	if err := s.providerRepo.SetError(ctx, provider.ID, errorMsg); err != nil {
		s.options.Warn("provider_set_error_failed", "provider_id", provider.ID, "error", err)
		return
	}
	s.options.Warn("provider_disabled_auth_error", "provider_id", provider.ID, "error", errorMsg)
}

// ApplyCustomErrorCode 处理自定义错误码，停止提供商调度。
func (s *HealthService) ApplyCustomErrorCode(ctx context.Context, provider *Record, statusCode int, errorMsg string) {
	msg := "Custom error code " + strconv.Itoa(statusCode) + ": " + errorMsg
	s.notifyProviderSchedulingBlocked(provider, time.Time{}, "custom_error_code")
	if err := s.providerRepo.SetError(ctx, provider.ID, msg); err != nil {
		s.options.Warn("provider_set_error_failed", "provider_id", provider.ID, "status_code", statusCode, "error", err)
		return
	}
	s.options.Warn("provider_disabled_custom_error", "provider_id", provider.ID, "status_code", statusCode, "error", errorMsg)
}

// ApplyOverload 处理 529 过载错误
// 根据配置决定是否暂停提供商调度及冷却时长。
func (s *HealthService) ApplyOverload(ctx context.Context, provider *Record) {
	var settings *OverloadCooldownSettings
	if s.options.OverloadSettings != nil {
		var err error
		settings, err = s.options.OverloadSettings(ctx)
		if err != nil {
			s.options.Warn("overload_settings_read_failed", "provider_id", provider.ID, "error", err)
			settings = nil
		}
	}
	// 回退到配置文件
	if settings == nil {
		cooldown := s.options.OverloadMinutes
		if cooldown <= 0 {
			cooldown = 10
		}
		settings = &OverloadCooldownSettings{Enabled: true, CooldownMinutes: cooldown}
	}

	if !settings.Enabled {
		s.options.Info("provider_529_ignored", "provider_id", provider.ID, "reason", "overload_cooldown_disabled")
		return
	}

	cooldownMinutes := settings.CooldownMinutes
	if cooldownMinutes <= 0 {
		cooldownMinutes = 10
	}

	until := s.options.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
	s.notifyProviderSchedulingBlocked(provider, until, "529")
	if err := s.providerRepo.SetOverloaded(ctx, provider.ID, until); err != nil {
		s.options.Warn("overload_set_failed", "provider_id", provider.ID, "error", err)
		return
	}

	s.options.Info("provider_overloaded", "provider_id", provider.ID, "until", until)
}
