package provider_test

import (
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// newUpstreamHealthForTest 根据测试配置构造上游健康观测器。
func newUpstreamHealthForTest(store gatewayprovider.ExecutionProviderStore, cfg *config.Config, cache provider.TempUnschedCache, options provider.HealthOptions, readers *gatewayprovider.RuntimeReaders) *provideradapter.UpstreamHealth {
	if cfg != nil {
		options.UnauthorizedCooldownMinutes = cfg.RateLimit.OAuth401CooldownMinutes
		options.OverloadMinutes = cfg.RateLimit.OverloadCooldownMinutes
		options.CNIntervalMinutes = cfg.Gateway.CNProviders.IntervalMinutes
	}
	return gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: store, Cache: cache, Options: options, Readers: readers})
}

// newExecutionReadersFixture 保持设置替身的原缓存包裹与读取时点。
func newExecutionReadersFixture(repo settings.Repository, _ *config.Config) *gatewayprovider.RuntimeReaders {
	if repo != nil {
		repo = settings.New(repo)
	}
	return gatewaytestkit.RuntimeReaders(repo)
}
