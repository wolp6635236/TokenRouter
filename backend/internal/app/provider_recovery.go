package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/gateway/media"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

// providerHealthRuntime 保存同一次装配创建的健康状态组件。
type providerHealthRuntime struct {
	Health   *provider.HealthService
	Recovery *provider.RecoveryService
	Observer *provideradapter.UpstreamHealth
}

// provideUpstreamHealth 返回 app 已构造的共享健康状态组件。
func provideUpstreamHealth(runtime *providerHealthRuntime) *provideradapter.UpstreamHealth {
	return runtime.Observer
}

// provideProviderHealthRuntime 为提供商健康状态组件绑定配置、设置读取器和基础设施接口。
func provideProviderHealthRuntime(
	store *providerpostgres.ProviderStore,
	cache provider.TempUnschedCache,
	cfg *config.Config,
	settings *provider.RuntimeSettings,
	timeouts provider.TimeoutCounterCache,
	forbidden provider.OpenAI403CounterCache,
	tokens provider.TokenCacheInvalidator,
	state *provider.RuntimeBlockState,
) *providerHealthRuntime {
	options := provider.HealthOptions{
		Now: time.Now, Warn: slog.Warn, Info: slog.Info,
		SessionWindows: store, TimeoutCounter: timeouts, ForbiddenCounter: forbidden,
		UnauthorizedCooldownMinutes: cfg.RateLimit.OAuth401CooldownMinutes,
		CNIntervalMinutes:           cfg.Gateway.CNProviders.IntervalMinutes,
		OverloadMinutes:             cfg.RateLimit.OverloadCooldownMinutes,
		APIKeyHealthWarn:            provideradapter.LogAPIKeyHealthWarning,
	}
	if counter, ok := cache.(provider.OpenAIAPIKeyHealthCache); ok {
		options.APIKeyHealthCounter = counter
	}
	if settings != nil {
		options.APIKeyHealthSettings = settings.GetOpenAIAPIKeyHealthBreakerSettings
		options.RateLimit429Settings = settings.GetRateLimit429CooldownSettings
		options.ForbiddenSettings = settings.GetOpenAI403CooldownSettings
		options.OverloadSettings = settings.GetOverloadCooldownSettings
		options.HasThresholdSettings = func() bool { return true }
		options.Thresholds = settings.GetProviderSchedulingThresholds
		options.StreamSettings = func(ctx context.Context) (*provider.StreamTimeoutSettings, error, bool) {
			value, err := settings.GetStreamTimeoutSettings(ctx)
			return value, err, true
		}
	}
	if tokens != nil {
		options.InvalidateUnauthorizedToken = tokens.InvalidateToken
	}
	if state != nil {
		options.Block = state.BlockProviderScheduling
	}

	// 恢复与窗口观测会相互调用，完成函数绑定后再返回组件供调用方使用。
	var recovery *provider.RecoveryService
	options.ClearWindowRateLimit = func(ctx context.Context, id int64) error {
		return recovery.ClearRateLimit(ctx, id)
	}
	health := provider.NewHealthService(store, cache, options)
	recoveryOptions := provider.RecoveryOptions{Now: time.Now, Warn: slog.Warn, ResetCounter: health.ResetForbiddenCounter}
	if state != nil {
		recoveryOptions.ClearSchedulingBlock = state.ClearProviderSchedulingBlock
	}
	if tokens != nil {
		recoveryOptions.InvalidateToken = tokens.InvalidateToken
	}
	recovery = provider.NewRecoveryService(store, cache, recoveryOptions)

	limits := &provideradapter.RateLimitObserver{Health: health, Plans: store, NextGeminiDaily: func() *int64 {
		reset := provider.GeminiDailyResetTime(time.Now(), geminiQuotaLocation()).Unix()
		return &reset
	}}
	if state != nil {
		limits.RetryOpenAI = func(value *provider.Record, headers http.Header, body []byte) bool {
			return provideradapter.CanRetryOpenAI429(state, value, headers, body)
		}
	}

	team := provider.NewTeamLinkedHealth(store, provider.TeamLinkedOptions{Now: options.Now, Warn: options.Warn, Block: options.Block})
	models := &provideradapter.ModelHealth{Health: health, IsImageModel: media.IsGPTImageGenerationModel}

	return &providerHealthRuntime{
		Health: health, Recovery: recovery, Observer: &provideradapter.UpstreamHealth{Core: health, Team: team, Limits: limits, Models: models},
	}
}

// provideProviderRecovery 返回共享的提供商恢复用例。
func provideProviderRecovery(runtime *providerHealthRuntime) *provider.RecoveryService {
	return runtime.Recovery
}

// provideProviderRuntimeState 构造恢复、刷新和执行入口共享的运行状态。
func provideProviderRuntimeState() *provider.RuntimeBlockState {
	return provider.NewRuntimeBlockState(time.Now)
}
