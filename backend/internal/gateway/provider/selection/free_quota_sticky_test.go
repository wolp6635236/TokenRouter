package selection

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/TokenFlux/TokenRouter/internal/usage"
	"github.com/stretchr/testify/require"
)

func TestGetSchedulableProvider_AppliesGrokFreeSoftGate(t *testing.T) {
	// 缓存预热后，粘性或非列表路径不得返回超过门禁的免费 OAuth 提供商。
	// 首次粘性命中失败开放并安排异步刷新，后续命中使用缓存。
	cfg := &config.Config{}
	cfg.Gateway.Grok.FreeQuotaSoftGateEnabled = true
	cfg.Gateway.Grok.FreeQuotaTokenLimit = 500_000
	cfg.Gateway.Grok.FreeQuotaSoftGatePercent = 95
	cfg.Gateway.Grok.FreeQuotaWindowHours = 24
	cfg.Gateway.Grok.FreeQuotaStatsCacheSeconds = 60

	provider := healthyGrokOAuthGatewayTestProvider(8801, "tok")
	provider.Record.Credentials["subscription_tier"] = "free"
	provider.Record.Status = billing.StatusActive
	provider.Record.Schedulable = true

	repo := &mockProviderRepoForPlatform{
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{provider.Record.ID: provider},
	}
	usageRepo := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usage.ProviderStats{
		provider.Record.ID: {Tokens: 480_000}, // above 95% of 500k
	}}
	// 清理共享网关免费层门禁缓存，保证测试结果稳定。
	var tasks sync.WaitGroup
	t.Cleanup(tasks.Wait)
	gate := newGrokFreeQuotaTestGate(cfg, usageRepo, func(_ string, work func()) bool {
		tasks.Add(1)
		go func() { defer tasks.Done(); work() }()
		return true
	})
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, FreeQuota: gate}, cfg)

	got, err := svc.getSchedulableProvider(context.Background(), provider.Record.ID)
	require.NoError(t, err)
	require.NotNil(t, got, "first sticky hit fail-opens while free-gate stats refresh")

	require.Eventually(t, func() bool {
		got, err := svc.getSchedulableProvider(context.Background(), provider.Record.ID)
		return err == nil && got == nil
	}, 2*time.Second, 10*time.Millisecond, "over free soft-gate sticky hit must miss after cache warm")
}

type grokFreeQuotaUsageRepoStub struct {
	usage.UsageLogRepository

	mu      sync.Mutex
	stats   map[int64]*usage.ProviderStats
	err     error
	calls   int
	lastIDs []int64
	start   time.Time
}

func (r *grokFreeQuotaUsageRepoStub) GetProviderWindowStatsBatch(_ context.Context, providerIDs []int64, start time.Time) (map[int64]*usage.ProviderStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lastIDs = append([]int64(nil), providerIDs...)
	r.start = start
	if r.err != nil {
		return nil, r.err
	}
	result := make(map[int64]*usage.ProviderStats, len(providerIDs))
	for _, providerID := range providerIDs {
		if stats := r.stats[providerID]; stats != nil {
			copyStats := *stats
			result[providerID] = &copyStats
		}
	}
	return result, nil
}

func newGrokFreeQuotaTestGate(cfg *config.Config, reader usage.UsageLogRepository, background func(string, func()) bool) *provider.FreeQuotaGate {
	return provider.NewFreeQuotaGate(func() provider.FreeQuotaOptions {
		v := cfg.Gateway.Grok
		return provider.FreeQuotaOptions{Enabled: v.FreeQuotaSoftGateEnabled, TokenLimit: v.FreeQuotaTokenLimit, Percent: v.FreeQuotaSoftGatePercent, WindowHours: v.FreeQuotaWindowHours, CacheSeconds: v.FreeQuotaStatsCacheSeconds}
	}, func(ctx context.Context, ids []int64, start time.Time) (map[int64]int64, error) {
		return usage.ReadProviderTokenWindow(ctx, reader, ids, start)
	}, background, time.Now, nil, nil)
}

func healthyGrokOAuthGatewayTestProvider(id int64, token string) *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: provider.Record{
			LoadLocation: time.LoadLocation, ID: id,
			Name:        "grok",
			Platform:    capability.PlatformGrok,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Credentials: map[string]any{
				"access_token":  token,
				"refresh_token": "refresh-token",
				"expires_at":    time.Now().Add(2 * provider.GrokTokenRefreshSkew).UTC().Format(time.RFC3339),
				"base_url":      xai.DefaultCLIBaseURL,
			},
		},
	}
}

func TestOpenAIProviderSchedulerLoadBalanceAppliesGrokFreeQuotaGate(t *testing.T) {
	cfg := grokFreeQuotaTestConfig()

	providers := []gatewayprovider.ExecutionProvider{
		{Record: provider.Record{LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"model_whitelist": []string{"*"}, "subscription_tier": "free"}}},
		{Record: provider.Record{LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"model_whitelist": []string{"*"}, "subscription_tier": "pro"}}},
	}
	reader := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usage.ProviderStats{1: {Tokens: 480_000}}}
	factory := freeQuotaFactoryForTest(t, cfg, reader)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:                Reads{Providers: selectionProviderFixture{providers: providers}},
		FreeQuota:            factory(),
		NewAdvancedFreeQuota: factory,
	}, cfg)
	picker := &compatiblePicker{service: svc, stats: scheduler.NewRuntimeStats(time.Now)}

	// 通过后台刷新预热缓存，使负载均衡路径能够看到软性门禁结果。
	_ = picker.filterGrokFreeQuotaProviders(context.Background(), providers)
	require.Eventually(t, func() bool {
		filtered := picker.filterGrokFreeQuotaProviders(context.Background(), providers)
		return len(providerIDs(filtered)) == 1 && providerIDs(filtered)[0] == 2
	}, 2*time.Second, 10*time.Millisecond)

	core, scope := picker.platformSelector()
	result, _, _, _, err := core.SelectByLoadBalance(context.Background(), scheduler.PlatformSelectionInput{GroupID: selectionFixtureGroupID(context.Background()), Platform: capability.PlatformGrok})
	selection := scope.restore(result)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(2), selection.Provider.Record.ID)
}

func grokFreeQuotaTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.Grok.FreeQuotaSoftGateEnabled = true
	cfg.Gateway.Grok.FreeQuotaTokenLimit = 500_000
	cfg.Gateway.Grok.FreeQuotaSoftGatePercent = 95
	cfg.Gateway.Grok.FreeQuotaWindowHours = 24
	cfg.Gateway.Grok.FreeQuotaStatsCacheSeconds = 60
	return cfg
}

func providerIDs(providers []gatewayprovider.ExecutionProvider) []int64 {
	ids := make([]int64, 0, len(providers))
	for i := range providers {
		ids = append(ids, providers[i].Record.ID)
	}
	return ids
}

// TestOpenAIGetSchedulableProvider_AppliesGrokFreeSoftGate 检查 OpenAI 兼容选择入口是否执行 Grok 免费层门禁。
func TestOpenAIGetSchedulableProvider_AppliesGrokFreeSoftGate(t *testing.T) {
	// 基础调度的 OpenAI 兼容粘性路径也执行 Grok 免费层门禁。
	cfg := &config.Config{}
	cfg.Gateway.Grok.FreeQuotaSoftGateEnabled = true
	cfg.Gateway.Grok.FreeQuotaTokenLimit = 500_000
	cfg.Gateway.Grok.FreeQuotaSoftGatePercent = 95
	cfg.Gateway.Grok.FreeQuotaWindowHours = 24
	cfg.Gateway.Grok.FreeQuotaStatsCacheSeconds = 60

	provider := healthyGrokOAuthGatewayTestProvider(8802, "tok")
	provider.Record.Credentials["subscription_tier"] = "free"
	provider.Record.Status = billing.StatusActive
	provider.Record.Schedulable = true

	repo := &mockProviderRepoForPlatform{
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{provider.Record.ID: provider},
	}
	usageRepo := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usage.ProviderStats{
		provider.Record.ID: {Tokens: 480_000},
	}}
	factory := freeQuotaFactoryForTest(t, cfg, usageRepo)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:                Reads{Providers: repo},
		FreeQuota:            factory(),
		NewAdvancedFreeQuota: factory,
	}, cfg)

	got, err := svc.getSchedulableProvider(context.Background(), provider.Record.ID)
	require.NoError(t, err)
	require.NotNil(t, got, "first sticky hit fail-opens while free-gate stats refresh")

	require.Eventually(t, func() bool {
		got, err := svc.getSchedulableProvider(context.Background(), provider.Record.ID)
		return err == nil && got == nil
	}, 2*time.Second, 10*time.Millisecond, "OpenAI legacy sticky must apply free soft-gate after cache warm")
}

// freeQuotaFactoryForTest 保留各池独立缓存，测试结束等待已接受的刷新。
func freeQuotaFactoryForTest(t *testing.T, cfg *config.Config, source usage.UsageLogRepository) func() *provider.FreeQuotaGate {
	var tasks sync.WaitGroup
	t.Cleanup(tasks.Wait)
	background := func(_ string, work func()) bool {
		tasks.Add(1)
		go func() { defer tasks.Done(); work() }()
		return true
	}
	return func() *provider.FreeQuotaGate { return newGrokFreeQuotaTestGate(cfg, source, background) }
}
