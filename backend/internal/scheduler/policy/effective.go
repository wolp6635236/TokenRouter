package policy

import (
	"fmt"
	"math"
)

const (
	defaultAdvancedSchedulerErrorRateAlpha = 0.2
	defaultAdvancedSchedulerTTFTAlpha      = 0.2

	// 消息串行和节流模式使用配置中的字符串值。
	MessageQueueSerialize = "serialize"
	MessageQueueThrottle  = "throttle"
)

// EffectiveSettings 是完成全局与分组覆盖合并后的请求级配置。
// 分组字段优先级最高；缺失字段继续继承设置仓库和静态配置的结果。
type EffectiveSettings struct {
	StickyWeightedEnabled       bool
	SubscriptionPriorityEnabled bool
	TopK                        int
	Weights                     ScoreWeights
	Feedback                    FeedbackConfig
	StickyEscape                StickyEscapeConfig
}

type ScoreWeights struct {
	Priority  float64
	Load      float64
	Queue     float64
	ErrorRate float64
	TTFT      float64
	// Reset 为会话窗口更早重置的提供商加分，默认为 0，表示关闭。
	Reset float64
	// QuotaHeadroom 倾向 Codex 7d 剩余额度更健康的提供商；0 表示关闭（默认）。
	QuotaHeadroom float64
	Previous      float64
	SessionSticky float64
}

type RuntimeSettings struct {
	StickyWeightedEnabled       bool
	SubscriptionPriorityEnabled bool
	LbTopKOverride              int
	WeightOverrides             map[string]float64
	EwmaErrorRateAlpha          float64
	EwmaErrorRateAlphaSet       bool
	EwmaTTFTAlpha               float64
	EwmaTTFTAlphaSet            bool
	StickyEscapeEnabled         bool
	StickyEscapeEnabledSet      bool
	StickyEscapeTTFTMs          float64
	StickyEscapeTTFTMsSet       bool
	StickyEscapeErrorRate       float64
	StickyEscapeErrorRateSet    bool
	StickyEscape                StickyEscapeConfig
}

type StickyEscapeConfig struct {
	Enabled   bool
	TtftMs    float64
	ErrorRate float64
}

// FeedbackConfig 保存一次请求回写运行时反馈时使用的 EWMA 系数。
// 统计仍按提供商共享，但系数由请求最终命中的分组决定。
type FeedbackConfig struct {
	ErrorRateAlpha float64
	TtftAlpha      float64
}

// ValidateGroupOverrides 校验分组覆盖字段的取值范围。
// 基础权重允许全部设为零，此时 Top-K 使用稳定并列规则并等权抽样。
func ValidateGroupOverrides(overrides GroupAdvancedSchedulerOverrides) error {
	if overrides.LBTopK != nil && *overrides.LBTopK <= 0 {
		return fmt.Errorf("lb_top_k must be a positive integer")
	}
	for _, item := range []struct {
		name  string
		value *float64
	}{{"ewma_error_rate_alpha", overrides.EWMAErrorRateAlpha}, {"ewma_ttft_alpha", overrides.EWMATTFTAlpha}} {
		if item.value == nil {
			continue
		}
		if *item.value <= 0 || *item.value > 1 || math.IsNaN(*item.value) || math.IsInf(*item.value, 0) {
			return fmt.Errorf("%s must be between 0 and 1", item.name)
		}
	}
	if overrides.StickyEscapeTTFTMs != nil && *overrides.StickyEscapeTTFTMs <= 0 {
		return fmt.Errorf("sticky_escape_ttft_ms must be positive")
	}
	if overrides.StickyEscapeErrorRate != nil &&
		(*overrides.StickyEscapeErrorRate < 0 || *overrides.StickyEscapeErrorRate > 1 ||
			math.IsNaN(*overrides.StickyEscapeErrorRate) || math.IsInf(*overrides.StickyEscapeErrorRate, 0)) {
		return fmt.Errorf("sticky_escape_error_rate must be between 0 and 1")
	}

	Weights := []struct {
		name  string
		value *float64
	}{
		{"weight_priority", overrides.WeightPriority},
		{"weight_load", overrides.WeightLoad},
		{"weight_queue", overrides.WeightQueue},
		{"weight_error_rate", overrides.WeightErrorRate},
		{"weight_ttft", overrides.WeightTTFT},
		{"weight_reset", overrides.WeightReset},
		{"weight_quota_headroom", overrides.WeightQuotaHeadroom},
		{"weight_previous_response", overrides.WeightPreviousResponse},
		{"weight_session_sticky", overrides.WeightSessionSticky},
	}
	for _, item := range Weights {
		if item.value == nil {
			continue
		}
		if *item.value < 0 || math.IsNaN(*item.value) || math.IsInf(*item.value, 0) {
			return fmt.Errorf("%s must be a non-negative finite number", item.name)
		}
	}

	return nil
}

// ValidateEffectiveWeights 校验合并后的完整权重。
// 基础权重总和可以为零，基础权重和全部权重的总和都需要是有限值。
func ValidateEffectiveWeights(weights ScoreWeights) error {
	values := []struct {
		name  string
		value float64
	}{
		{"weight_priority", weights.Priority},
		{"weight_load", weights.Load},
		{"weight_queue", weights.Queue},
		{"weight_error_rate", weights.ErrorRate},
		{"weight_ttft", weights.TTFT},
		{"weight_reset", weights.Reset},
		{"weight_quota_headroom", weights.QuotaHeadroom},
		{"weight_previous_response", weights.Previous},
		{"weight_session_sticky", weights.SessionSticky},
	}
	for _, item := range values {
		if item.value < 0 || math.IsNaN(item.value) || math.IsInf(item.value, 0) {
			return fmt.Errorf("%s must be a non-negative finite number", item.name)
		}
	}

	resolved := weights
	if baseSum := resolved.BaseWeightSum(); math.IsNaN(baseSum) || math.IsInf(baseSum, 0) {
		return fmt.Errorf("base-weight sum must be finite")
	}
	if totalSum := resolved.TotalWeightSum(); math.IsNaN(totalSum) || math.IsInf(totalSum, 0) {
		return fmt.Errorf("total-weight sum must be finite")
	}
	return nil
}

// HasWeightOverrides 判断是否需要读取全局权重完成合并校验。
func HasWeightOverrides(overrides GroupAdvancedSchedulerOverrides) bool {
	return overrides.WeightPriority != nil ||
		overrides.WeightLoad != nil ||
		overrides.WeightQueue != nil ||
		overrides.WeightErrorRate != nil ||
		overrides.WeightTTFT != nil ||
		overrides.WeightReset != nil ||
		overrides.WeightQuotaHeadroom != nil ||
		overrides.WeightPreviousResponse != nil ||
		overrides.WeightSessionSticky != nil
}

// ApplyGroupWeightOverrides 替换分组提供的权重字段。
func ApplyGroupWeightOverrides(
	weights ScoreWeights,
	overrides GroupAdvancedSchedulerOverrides,
) ScoreWeights {
	if overrides.WeightPriority != nil {
		weights.Priority = *overrides.WeightPriority
	}
	if overrides.WeightLoad != nil {
		weights.Load = *overrides.WeightLoad
	}
	if overrides.WeightQueue != nil {
		weights.Queue = *overrides.WeightQueue
	}
	if overrides.WeightErrorRate != nil {
		weights.ErrorRate = *overrides.WeightErrorRate
	}
	if overrides.WeightTTFT != nil {
		weights.TTFT = *overrides.WeightTTFT
	}
	if overrides.WeightReset != nil {
		weights.Reset = *overrides.WeightReset
	}
	if overrides.WeightQuotaHeadroom != nil {
		weights.QuotaHeadroom = *overrides.WeightQuotaHeadroom
	}
	if overrides.WeightPreviousResponse != nil {
		weights.Previous = *overrides.WeightPreviousResponse
	}
	if overrides.WeightSessionSticky != nil {
		weights.SessionSticky = *overrides.WeightSessionSticky
	}
	return weights
}

// ResolveEffective 以全局生效配置为基线合并分组覆盖。
// 分组设置的零值也参与覆盖，基础权重全部为零时仍使用分组权重。
func ResolveEffective(
	baseTopK int,
	baseWeights ScoreWeights,
	global RuntimeSettings,
	overrides GroupAdvancedSchedulerOverrides,
) EffectiveSettings {
	if baseTopK <= 0 {
		baseTopK = 7
	}
	globalTopK := baseTopK
	if global.LbTopKOverride > 0 {
		globalTopK = global.LbTopKOverride
	}
	globalWeights := ApplyGlobalWeightOverrides(baseWeights, global.WeightOverrides)
	if !globalWeights.ValidGlobal() {
		globalWeights = baseWeights
	}

	effective := EffectiveSettings{
		StickyWeightedEnabled:       global.StickyWeightedEnabled,
		SubscriptionPriorityEnabled: global.SubscriptionPriorityEnabled,
		TopK:                        globalTopK,
		Weights:                     globalWeights,
		Feedback:                    NormalizeFeedback(FeedbackConfig{ErrorRateAlpha: global.EwmaErrorRateAlpha, TtftAlpha: global.EwmaTTFTAlpha}),
		StickyEscape:                NormalizeStickyEscape(StickyEscapeConfig{Enabled: global.StickyEscapeEnabled, TtftMs: global.StickyEscapeTTFTMs, ErrorRate: global.StickyEscapeErrorRate}),
	}
	if overrides.StickyWeightedEnabled != nil {
		effective.StickyWeightedEnabled = *overrides.StickyWeightedEnabled
	}
	if overrides.SubscriptionPriorityEnabled != nil {
		effective.SubscriptionPriorityEnabled = *overrides.SubscriptionPriorityEnabled
	}
	if overrides.LBTopK != nil && *overrides.LBTopK > 0 {
		effective.TopK = *overrides.LBTopK
	}
	if overrides.EWMAErrorRateAlpha != nil {
		effective.Feedback.ErrorRateAlpha = *overrides.EWMAErrorRateAlpha
	}
	if overrides.EWMATTFTAlpha != nil {
		effective.Feedback.TtftAlpha = *overrides.EWMATTFTAlpha
	}
	if overrides.StickyEscapeEnabled != nil {
		effective.StickyEscape.Enabled = *overrides.StickyEscapeEnabled
	}
	if overrides.StickyEscapeTTFTMs != nil {
		effective.StickyEscape.TtftMs = float64(*overrides.StickyEscapeTTFTMs)
	}
	if overrides.StickyEscapeErrorRate != nil {
		effective.StickyEscape.ErrorRate = *overrides.StickyEscapeErrorRate
	}
	effective.Feedback = NormalizeFeedback(effective.Feedback)
	effective.StickyEscape = NormalizeStickyEscape(effective.StickyEscape)
	effective.Weights = ApplyGroupWeightOverrides(effective.Weights, overrides)
	if validateErr := ValidateEffectiveWeights(effective.Weights); validateErr != nil {
		// 权重异常时回退到默认权重，分组的其他有效覆盖继续生效。
		effective.Weights = globalWeights
	}
	return effective
}

// NormalizeStickyEscape 将健康逃逸配置限制在允许的取值范围内。
func NormalizeStickyEscape(value StickyEscapeConfig) StickyEscapeConfig {
	thresholdsUnset := value.TtftMs == 0 && value.ErrorRate == 0
	if !value.Enabled && value.TtftMs == 0 && value.ErrorRate == 0 {
		// 未注册配置结构体时使用零值，默认开启健康逃逸。
		value.Enabled = true
	}
	if value.TtftMs <= 0 || math.IsNaN(value.TtftMs) || math.IsInf(value.TtftMs, 0) {
		value.TtftMs = 15000
	}
	if value.ErrorRate < 0 || value.ErrorRate > 1 || math.IsNaN(value.ErrorRate) || math.IsInf(value.ErrorRate, 0) {
		value.ErrorRate = 0.5
	}
	if thresholdsUnset {
		value.ErrorRate = 0.5
	}
	return value
}

func ApplyGlobalWeightOverrides(
	weights ScoreWeights,
	overrides map[string]float64,
) ScoreWeights {
	for key, value := range overrides {
		switch key {
		case "priority":
			weights.Priority = value
		case "load":
			weights.Load = value
		case "queue":
			weights.Queue = value
		case "error_rate":
			weights.ErrorRate = value
		case "ttft":
			weights.TTFT = value
		case "reset":
			weights.Reset = value
		case "quota_headroom":
			weights.QuotaHeadroom = value
		case "previous_response":
			weights.Previous = value
		case "session_sticky":
			weights.SessionSticky = value
		}
	}
	return weights
}

func NormalizeFeedback(value FeedbackConfig) FeedbackConfig {
	if value.ErrorRateAlpha <= 0 || value.ErrorRateAlpha > 1 || math.IsNaN(value.ErrorRateAlpha) || math.IsInf(value.ErrorRateAlpha, 0) {
		value.ErrorRateAlpha = defaultAdvancedSchedulerErrorRateAlpha
	}
	if value.TtftAlpha <= 0 || value.TtftAlpha > 1 || math.IsNaN(value.TtftAlpha) || math.IsInf(value.TtftAlpha, 0) {
		value.TtftAlpha = defaultAdvancedSchedulerTTFTAlpha
	}
	return value
}

// BaseWeightSum 按配置字段顺序累加基础权重，浮点加法的顺序会影响结果。
func (w ScoreWeights) BaseWeightSum() float64 {
	return w.Priority + w.Load + w.Queue + w.ErrorRate + w.TTFT + w.Reset + w.QuotaHeadroom
}

func (w ScoreWeights) TotalWeightSum() float64 {
	return w.BaseWeightSum() + w.Previous + w.SessionSticky
}

// ValidGlobal 检查全局基础权重之和为正，分组权重另行校验并允许总和为零。
func (w ScoreWeights) ValidGlobal() bool {
	return ValidateEffectiveWeights(w) == nil && w.BaseWeightSum() > 0
}
