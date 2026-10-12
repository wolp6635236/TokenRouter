package provider

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	TempUnschedBodyMaxBytes    = 64 << 10
	TempUnschedMessageMaxBytes = 2048
)

// HealthStore 提供各项独立的健康状态写入操作。
type HealthStore interface {
	GetByID(context.Context, int64) (*Record, error)
	SetError(context.Context, int64, string) error
	SetOverloaded(context.Context, int64, time.Time) error
	SetRateLimited(context.Context, int64, time.Time) error
	SetTempUnschedulable(context.Context, int64, time.Time, string) error
	SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error
}
type HealthOptions struct {
	APIKeyHealthCounter         OpenAIAPIKeyHealthCache
	APIKeyHealthSettings        func(context.Context) (*OpenAIAPIKeyHealthBreakerSettings, error)
	APIKeyHealthWarn            func(string, ...any)
	UnauthorizedCooldownMinutes int
	InvalidateUnauthorizedToken func(context.Context, *Record) error
	SessionWindows              SessionWindowStore
	ClearWindowRateLimit        func(context.Context, int64) error
	RateLimit429Settings        func(context.Context) (*RateLimit429CooldownSettings, error)
	CNIntervalMinutes           int
	ForbiddenCounter            OpenAI403CounterCache
	ForbiddenSettings           func(context.Context) (*OpenAI403CooldownSettings, error)
	OverloadMinutes             int
	OverloadSettings            func(context.Context) (*OverloadCooldownSettings, error)
	HasThresholdSettings        func() bool
	Thresholds                  func(context.Context) map[string]int
	Now                         func() time.Time
	Warn, Info                  func(string, ...any)
	StreamSettings              func(context.Context) (*StreamTimeoutSettings, error, bool)
	Block                       func(*Record, time.Time, string)
	TimeoutCounter              TimeoutCounterCache
}

// HealthService 拥有通用规则与流超时阈值，供应商报文和模型规范化在外层完成。
type HealthService struct {
	providerRepo        HealthStore
	tempUnschedCache    TempUnschedCache
	timeoutCounterCache TimeoutCounterCache
	options             HealthOptions
}

type TempUnschedulableRuleMatch struct {
	Rule           TempUnschedulableRule
	RuleIndex      int
	MatchedKeyword string
}

func NewHealthService(store HealthStore, cache TempUnschedCache, options HealthOptions) *HealthService {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Warn == nil {
		options.Warn = func(string, ...any) {}
	}
	if options.APIKeyHealthWarn == nil {
		options.APIKeyHealthWarn = options.Warn
	}
	if options.Info == nil {
		options.Info = func(string, ...any) {}
	}
	return &HealthService{providerRepo: store, tempUnschedCache: cache, timeoutCounterCache: options.TimeoutCounter, options: options}
}

func (s *HealthService) notifyProviderSchedulingBlocked(v *Record, until time.Time, reason string) {
	if s.options.Block != nil {
		s.options.Block(v, until, reason)
	}
}

func firstRequestedModel(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func MatchTempUnschedulableRules(provider *Record, statusCode int, responseBody []byte) []TempUnschedulableRuleMatch {
	if provider == nil || !provider.IsTempUnschedulableEnabled() || statusCode <= 0 || len(responseBody) == 0 {
		return nil
	}
	rules := provider.GetTempUnschedulableRules()
	if len(rules) == 0 {
		return nil
	}
	body := responseBody
	if len(body) > TempUnschedBodyMaxBytes {
		body = body[:TempUnschedBodyMaxBytes]
	}
	bodyLower := strings.ToLower(string(body))
	matches := make([]TempUnschedulableRuleMatch, 0, 1)
	for idx, rule := range rules {
		if rule.ErrorCode != statusCode || len(rule.Keywords) == 0 {
			continue
		}
		matchedKeyword := MatchTempUnschedKeyword(bodyLower, rule.Keywords)
		if matchedKeyword == "" {
			continue
		}
		matches = append(matches, TempUnschedulableRuleMatch{Rule: rule, RuleIndex: idx, MatchedKeyword: matchedKeyword})
	}
	return matches
}

func WasTempUnschedByStatusCode(reason string, statusCode int) bool {
	if statusCode <= 0 {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}

	var state TempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err != nil {
		return false
	}
	return state.StatusCode == statusCode
}

func MatchTempUnschedKeyword(bodyLower string, keywords []string) string {
	if bodyLower == "" {
		return ""
	}
	for _, keyword := range keywords {
		k := strings.TrimSpace(keyword)
		if k == "" {
			continue
		}
		if strings.Contains(bodyLower, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

func TruncateTempUnschedMessage(body []byte, maxBytes int) string {
	if maxBytes <= 0 || len(body) == 0 {
		return ""
	}
	if len(body) > maxBytes {
		body = body[:maxBytes]
	}
	return strings.TrimSpace(string(body))
}

func (s *HealthService) TriggerTempUnschedulable(ctx context.Context, provider *Record, rule TempUnschedulableRule, ruleIndex int, statusCode int, matchedKeyword string, responseBody []byte, requestedModel ...string) bool {
	if provider == nil {
		return false
	}
	if rule.DurationMinutes <= 0 {
		return false
	}

	now := s.options.Now()
	until := now.Add(time.Duration(rule.DurationMinutes) * time.Minute)

	state := &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      statusCode,
		MatchedKeyword:  matchedKeyword,
		RuleIndex:       ruleIndex,
		ErrorMessage:    TruncateTempUnschedMessage(responseBody, TempUnschedMessageMaxBytes),
	}

	reason := ""
	if raw, err := json.Marshal(state); err == nil {
		reason = string(raw)
	}
	if reason == "" {
		reason = strings.TrimSpace(state.ErrorMessage)
	}

	// 已知模型的失败写入模型键，使调度器只排除当前提供商与模型的组合。
	// 认证失败或模型未知时，使用下方的提供商临时停调规则。
	modelKey := firstRequestedModel(requestedModel)
	if modelKey != "" && statusCode != 401 {
		if err := s.providerRepo.SetModelRateLimit(ctx, provider.ID, modelKey, until, reason); err != nil {
			s.options.Warn("temp_unsched_model_rate_limit_set_failed", "provider_id", provider.ID, "model", modelKey, "error", err)
			// 规则已经命中，即使持久化失败也要切换当前请求；
			// 不得把模型级失败扩大成提供商级阻断。
			return true
		}
		s.options.Info("provider_model_temp_unschedulable", "provider_id", provider.ID, "model", modelKey, "until", until, "rule_index", ruleIndex, "status_code", statusCode)
		return true
	}

	s.notifyProviderSchedulingBlocked(provider, until, "temp_unschedulable")
	if err := s.providerRepo.SetTempUnschedulable(ctx, provider.ID, until, reason); err != nil {
		s.options.Warn("temp_unsched_set_failed", "provider_id", provider.ID, "error", err)
		return false
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, provider.ID, state); err != nil {
			s.options.Warn("temp_unsched_cache_set_failed", "provider_id", provider.ID, "error", err)
		}
	}

	s.options.Info("provider_temp_unschedulable", "provider_id", provider.ID, "until", until, "rule_index", ruleIndex, "status_code", statusCode)
	return true
}

// TryTempUnschedulable 允许调用方关闭重复 401 的默认升级。
// 池模式配置的 401 规则命中时，按该规则暂停提供商。
func (s *HealthService) TryTempUnschedulable(ctx context.Context, provider *Record, statusCode int, responseBody []byte, escalateRepeated401 bool, requestedModel ...string) bool {
	if provider == nil {
		return false
	}
	if !provider.IsTempUnschedulableEnabled() {
		return false
	}
	// 401 首次命中可临时不可调度（给 token 刷新窗口）；
	// 若历史上已因 401 进入过临时不可调度，则本次应升级为 error（返回 false 交由默认错误逻辑处理）。
	// 平台是否允许重复 401 升级由外层明确传入。
	if escalateRepeated401 && statusCode == 401 {
		reason := provider.TempUnschedulableReason
		// 缓存可能没有 reason，从 DB 回退读取
		if reason == "" {
			if dbAcc, err := s.providerRepo.GetByID(ctx, provider.ID); err == nil && dbAcc != nil {
				reason = dbAcc.TempUnschedulableReason
			}
		}
		if WasTempUnschedByStatusCode(reason, statusCode) {
			s.options.Info("401_escalated_to_error", "provider_id", provider.ID,
				"reason", "previous temp-unschedulable was also 401")
			return false
		}
	}
	for _, match := range MatchTempUnschedulableRules(provider, statusCode, responseBody) {
		if s.TriggerTempUnschedulable(ctx, provider, match.Rule, match.RuleIndex, statusCode, match.MatchedKeyword, responseBody, firstRequestedModel(requestedModel)) {
			return true
		}
	}

	return false
}

// HandleStreamTimeout 处理流数据超时
// 根据系统设置决定是否标记提供商为临时不可调度或错误状态
// 返回是否应该停止该提供商的调度。
func (s *HealthService) HandleStreamTimeout(ctx context.Context, provider *Record, model string) bool {
	if provider == nil {
		return false
	}

	if s.options.StreamSettings == nil {
		s.options.Warn("stream_timeout_setting_service_missing", "provider_id", provider.ID)
		return false
	}
	settings, err, available := s.options.StreamSettings(ctx)
	if !available {
		s.options.Warn("stream_timeout_setting_service_missing", "provider_id", provider.ID)
		return false
	}
	if err != nil {
		s.options.Warn("stream_timeout_get_settings_failed", "provider_id", provider.ID, "error", err)
		return false
	}

	if !settings.Enabled {
		return false
	}

	if settings.Action == StreamTimeoutActionNone {
		return false
	}

	// 增加超时计数
	var count int64 = 1
	if s.timeoutCounterCache != nil {
		count, err = s.timeoutCounterCache.IncrementTimeoutCount(ctx, provider.ID, settings.ThresholdWindowMinutes)
		if err != nil {
			s.options.Warn("stream_timeout_increment_count_failed", "provider_id", provider.ID, "error", err)
			// 继续处理，使用 count=1
			count = 1
		}
	}

	s.options.Info("stream_timeout_count", "provider_id", provider.ID, "count", count, "threshold", settings.ThresholdCount, "window_minutes", settings.ThresholdWindowMinutes, "model", model)

	// 检查是否达到阈值
	if count < int64(settings.ThresholdCount) {
		return false
	}

	// 达到阈值，执行相应操作
	switch settings.Action {
	case StreamTimeoutActionTempUnsched:
		return s.TriggerStreamTimeoutTempUnsched(ctx, provider, settings, model)
	case StreamTimeoutActionError:
		return s.TriggerStreamTimeoutError(ctx, provider, model)
	default:
		return false
	}
}

// TriggerStreamTimeoutTempUnsched 触发流超时临时不可调度。
func (s *HealthService) TriggerStreamTimeoutTempUnsched(ctx context.Context, provider *Record, settings *StreamTimeoutSettings, model string) bool {
	now := s.options.Now()
	until := now.Add(time.Duration(settings.TempUnschedMinutes) * time.Minute)

	state := &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      0, // 超时没有状态码
		MatchedKeyword:  "stream_timeout",
		RuleIndex:       -1, // 表示系统级规则
		ErrorMessage:    "Stream data interval timeout for model: " + model,
	}

	reason := ""
	if raw, err := json.Marshal(state); err == nil {
		reason = string(raw)
	}
	if reason == "" {
		reason = state.ErrorMessage
	}

	s.notifyProviderSchedulingBlocked(provider, until, "stream_timeout_temp_unschedulable")
	if err := s.providerRepo.SetTempUnschedulable(ctx, provider.ID, until, reason); err != nil {
		s.options.Warn("stream_timeout_set_temp_unsched_failed", "provider_id", provider.ID, "error", err)
		return false
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, provider.ID, state); err != nil {
			s.options.Warn("stream_timeout_set_temp_unsched_cache_failed", "provider_id", provider.ID, "error", err)
		}
	}

	// 重置超时计数
	if s.timeoutCounterCache != nil {
		if err := s.timeoutCounterCache.ResetTimeoutCount(ctx, provider.ID); err != nil {
			s.options.Warn("stream_timeout_reset_count_failed", "provider_id", provider.ID, "error", err)
		}
	}

	s.options.Info("stream_timeout_temp_unschedulable", "provider_id", provider.ID, "until", until, "model", model)
	return true
}

// TriggerStreamTimeoutError 触发流超时错误状态。
func (s *HealthService) TriggerStreamTimeoutError(ctx context.Context, provider *Record, model string) bool {
	errorMsg := "Stream data interval timeout (repeated failures) for model: " + model

	s.notifyProviderSchedulingBlocked(provider, time.Time{}, "stream_timeout_error")
	if err := s.providerRepo.SetError(ctx, provider.ID, errorMsg); err != nil {
		s.options.Warn("stream_timeout_set_error_failed", "provider_id", provider.ID, "error", err)
		return false
	}

	// 重置超时计数
	if s.timeoutCounterCache != nil {
		if err := s.timeoutCounterCache.ResetTimeoutCount(ctx, provider.ID); err != nil {
			s.options.Warn("stream_timeout_reset_count_failed", "provider_id", provider.ID, "error", err)
		}
	}

	s.options.Warn("stream_timeout_provider_error", "provider_id", provider.ID, "model", model)
	return true
}

// HandleTempUnschedulable 根据错误码策略和池模式决定停调或升级处理，调用方传入模型名。
func (s *HealthService) HandleTempUnschedulable(ctx context.Context, value *Record, status int, body []byte, model string) bool {
	if value == nil || !value.ShouldHandleErrorCode(status) {
		return false
	}
	return s.TryTempUnschedulable(ctx, value, status, body, !value.IsPoolMode() && value.Platform != PlatformAntigravity, model)
}
