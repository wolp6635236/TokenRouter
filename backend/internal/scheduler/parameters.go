package scheduler

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

// ParameterDefaults 是 app 提供的静态调度参数。
type ParameterDefaults struct {
	TopK    int
	Weights policy.ScoreWeights
	Runtime policy.RuntimeSettings
}

// Parameters 在构造时绑定参数来源，动态设置缓存在 SettingsRuntime 中。
type Parameters struct {
	defaults ParameterDefaults
	source   RuntimeSettingSource
	runtime  *SettingsRuntime
}

// DefaultParameters 返回未提供进程配置时使用的默认参数。
func DefaultParameters() ParameterDefaults {
	return ParameterDefaults{TopK: 7, Weights: policy.ScoreWeights{Priority: 1, Load: 1, Queue: .7, ErrorRate: .8, TTFT: .5, Previous: 5, SessionSticky: 3}, Runtime: policy.RuntimeSettings{EwmaErrorRateAlpha: DefaultErrorRateAlpha, EwmaTTFTAlpha: DefaultTTFTAlpha, StickyEscape: policy.NormalizeStickyEscape(policy.StickyEscapeConfig{Enabled: true, TtftMs: 15000, ErrorRate: .5})}}
}

func NewParameters(runtime *SettingsRuntime, source RuntimeSettingSource, defaults ParameterDefaults) *Parameters {
	return &Parameters{runtime: runtime, source: source, defaults: defaults}
}

// Defaults 返回静态参数的值副本，运行时覆盖作用于副本。
func (p *Parameters) Defaults() ParameterDefaults {
	if p == nil {
		return DefaultParameters()
	}
	return p.defaults
}

// Runtime 从共享 TTL 缓存读取动态设置，接收者为 nil 时使用默认参数。
func (p *Parameters) Runtime(ctx context.Context) policy.RuntimeSettings {
	if p == nil {
		return (&SettingsRuntime{}).Load(ctx, nil, DefaultParameters().Runtime)
	}
	return p.runtime.Load(ctx, p.source, p.defaults.Runtime)
}

// Effective 将传入的分组覆盖值应用到运行参数上。
func (p *Parameters) Effective(ctx context.Context, overrides policy.GroupAdvancedSchedulerOverrides) policy.EffectiveSettings {
	if ctx == nil {
		ctx = context.Background()
	}
	defaults := p.Defaults()
	return policy.ResolveEffective(defaults.TopK, defaults.Weights, p.Runtime(ctx), overrides)
}

// TopK 返回全局候选数量设置，供诊断使用。
func (p *Parameters) TopK(ctx context.Context) int {
	base := p.Defaults().TopK
	if base <= 0 {
		base = 7
	}
	if settings := p.Runtime(ctx); settings.LbTopKOverride > 0 {
		return settings.LbTopKOverride
	}
	return base
}

func (p *Parameters) Weights(ctx context.Context) policy.ScoreWeights {
	base := p.Defaults().Weights
	overridden := policy.ApplyGlobalWeightOverrides(base, p.Runtime(ctx).WeightOverrides)
	if !overridden.ValidGlobal() {
		return base
	}
	return overridden
}
