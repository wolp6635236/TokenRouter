package selection

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
	schedulerredis "github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/stretchr/testify/require"
)

type openAISnapshotCacheStub struct {
	schedulercore.SnapshotCache
	snapshotProviders []*gatewayprovider.ExecutionProvider
	providersByID     map[int64]*gatewayprovider.ExecutionProvider
}

type schedulerTestOpenAIProviderRepo struct {
	gatewayprovider.ExecutionProviderStore

	providers []gatewayprovider.

		// withAdvancedSchedulerTestGroup 为高级调度测试明确注入最终目标分组。
		// 生产代码不再读取全局开关，测试也必须声明该分组使用 advanced。
		ExecutionProvider
}

func withAdvancedSchedulerTestGroup(ctx context.Context, groupID int64) context.Context {
	return requeststate.WithGroup(ctx, &routing.Group{
		ID: groupID,

		SchedulerType: routing.GroupSchedulerTypeAdvanced,
		Status:        billing.StatusActive,
		Hydrated:      true,
	})
}

func (r schedulerTestOpenAIProviderRepo) GetByID(ctx context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	for i := range r.providers {
		if r.providers[i].Record.ID == id {
			prepareSelectionFixtureProvider(ctx, &r.providers[i], nil)
			return &r.providers[i], nil
		}
	}
	return nil, errors.New("provider not found")
}

func (r schedulerTestOpenAIProviderRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, []string{platform})
}

func (r schedulerTestOpenAIProviderRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range r.providers {
		if acc.Record.Platform == platform {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r schedulerTestOpenAIProviderRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

// ListModelAvailabilityCandidates 模拟只按持久配置筛选模型诊断候选提供商。
func (r schedulerTestOpenAIProviderRepo) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]gatewayprovider.ExecutionProvider, error) {
	platformSet := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		platformSet[platform] = struct{}{}
	}
	result := make([]gatewayprovider.ExecutionProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		if _, ok := platformSet[provider.Record.Platform]; !ok || provider.Record.Status != billing.StatusActive || !provider.Record.Schedulable {
			continue
		}
		if groupID != nil && !openAIStickyProviderMatchesGroup(&provider, groupID) {
			continue
		}
		if groupID == nil && !includeGrouped && !openAIStickyProviderMatchesGroup(&provider, nil) {
			continue
		}
		result = append(result, provider)
	}
	return result, nil
}

type schedulerGroupAwareOpenAIProviderRepo struct {
	schedulerTestOpenAIProviderRepo
}

func (r schedulerGroupAwareOpenAIProviderRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range r.providers {
		if acc.Record.Platform == platform && openAIStickyProviderMatchesGroup(&acc, &groupID) {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r schedulerGroupAwareOpenAIProviderRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range r.providers {
		if acc.Record.Platform == platform && openAIStickyProviderMatchesGroup(&acc, nil) {
			result = append(result, acc)
		}
	}
	return result, nil
}

type schedulerTestConcurrencyCache struct {
	schedulercore.ConcurrencyCache
	loadBatchErr    error
	loadMap         map[int64]*schedulercore.ProviderLoadInfo
	acquireResults  map[int64]bool
	waitCounts      map[int64]int
	skipDefaultLoad bool
	acquiredIDs     *[]int64
	releasedIDs     *[]int64
}

// noSlotSchedulerTestConcurrencyCache 在辅助选择操作并发槽时使测试失败。
type noSlotSchedulerTestConcurrencyCache struct {
	schedulerTestConcurrencyCache
	acquireCalls int
}

func (c *noSlotSchedulerTestConcurrencyCache) AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error) {
	c.acquireCalls++
	return false, errors.New("辅助选择不应申请并发槽")
}

func (c schedulerTestConcurrencyCache) AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error) {
	if c.acquiredIDs != nil {
		*c.acquiredIDs = append(*c.acquiredIDs, providerID)
	}
	if c.acquireResults != nil {
		if result, ok := c.acquireResults[providerID]; ok {
			return result, nil
		}
	}
	return true, nil
}

func (c schedulerTestConcurrencyCache) ReleaseProviderSlot(ctx context.Context, providerID int64, requestID string) error {
	if c.releasedIDs != nil {
		*c.releasedIDs = append(*c.releasedIDs, providerID)
	}
	return nil
}

func (c schedulerTestConcurrencyCache) GetProvidersLoadBatch(ctx context.Context, providers []schedulercore.ProviderWithConcurrency) (map[int64]*schedulercore.ProviderLoadInfo, error) {
	if c.loadBatchErr != nil {
		return nil, c.loadBatchErr
	}
	out := make(map[int64]*schedulercore.ProviderLoadInfo, len(providers))
	if c.skipDefaultLoad && c.loadMap != nil {
		for _, acc := range providers {
			if load, ok := c.loadMap[acc.ID]; ok {
				out[acc.ID] = load
			}
		}
		return out, nil
	}
	for _, acc := range providers {
		if c.loadMap != nil {
			if load, ok := c.loadMap[acc.ID]; ok {
				out[acc.ID] = load
				continue
			}
		}
		out[acc.ID] = &schedulercore.ProviderLoadInfo{ProviderID: acc.ID, LoadRate: 0}
	}
	return out, nil
}

func (c schedulerTestConcurrencyCache) GetProviderWaitingCount(ctx context.Context, providerID int64) (int, error) {
	if c.waitCounts != nil {
		if count, ok := c.waitCounts[providerID]; ok {
			return count, nil
		}
	}
	return 0, nil
}

type schedulerTestGatewayCache struct {
	sessionBindings map[string]int64
	deletedSessions map[string]int
}

func (c *schedulerTestGatewayCache) GetSessionProviderID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if id, ok := c.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func (c *schedulerTestGatewayCache) SetSessionProviderID(ctx context.Context, groupID int64, sessionHash string, providerID int64, ttl time.Duration) error {
	if c.sessionBindings == nil {
		c.sessionBindings = make(map[string]int64)
	}
	c.sessionBindings[sessionHash] = providerID
	return nil
}

func (c *schedulerTestGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	return nil
}

func (c *schedulerTestGatewayCache) DeleteSessionProviderID(ctx context.Context, groupID int64, sessionHash string) error {
	if c.sessionBindings == nil {
		return nil
	}
	if c.deletedSessions == nil {
		c.deletedSessions = make(map[string]int)
	}
	c.deletedSessions[sessionHash]++
	delete(c.sessionBindings, sessionHash)
	return nil
}

func (c *schedulerTestGatewayCache) SetSessionOwnerGroupID(ctx context.Context, userID int64, source, sessionHash string, groupID int64, ttl time.Duration) (bool, error) {
	return true, nil
}

func (c *schedulerTestGatewayCache) GetSessionOwnerGroupID(ctx context.Context, userID int64, source, sessionHash string) (int64, error) {
	return 0, errors.New("not found")
}

func (c *schedulerTestGatewayCache) RefreshSessionOwnerTTL(ctx context.Context, userID int64, source, sessionHash string, ttl time.Duration) error {
	return nil
}

func newSchedulerTestOpenAIWSV2Config() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	return cfg
}

func newSchedulerTestSubscriptionPriorityConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0
	return cfg
}

type advancedSchedulerSettingRepoStub struct {
	values map[string]string
}

func (s *advancedSchedulerSettingRepoStub) Get(ctx context.Context, key string) (*settings.Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &settings.Setting{Key: key, Value: value}, nil
}

func (s *advancedSchedulerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s == nil || s.values == nil {
		return "", settings.ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return "", settings.ErrSettingNotFound
	}
	return value, nil
}

func (s *advancedSchedulerSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected call to Set")
}

func (s *advancedSchedulerSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, err := s.GetValue(context.Background(), key); err == nil {
			result[key] = value
		}
	}
	return result, nil
}

func (s *advancedSchedulerSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected call to SetMultiple")
}

func (s *advancedSchedulerSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected call to GetAll")
}

func (s *advancedSchedulerSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected call to Delete")
}

func newAdvancedSchedulerParametersForTest(cfg *config.Config, _ string, values ...string) *schedulercore.Parameters {
	repo := &advancedSchedulerSettingRepoStub{
		values: map[string]string{},
	}
	if len(values) > 0 && values[0] != "" {
		repo.values[schedulercore.SettingKeyAdvancedSchedulerStickyWeightedEnabled] = values[0]
	}
	if len(values) > 1 && values[1] != "" {
		repo.values[schedulercore.SettingKeyAdvancedSchedulerSubscriptionPriorityEnabled] = values[1]
	}
	return schedulercore.NewParameters(schedulercore.NewSettingsRuntime(schedulercore.Diagnostics{}), repo, diagnosticParameterDefaults(cfg))
}

func (s *openAISnapshotCacheStub) GetSnapshot(ctx context.Context, bucket schedulercore.SchedulerBucket) ([]schedulercore.SnapshotProvider, bool, error) {
	if len(s.snapshotProviders) == 0 {
		return nil, false, nil
	}
	out := make([]schedulercore.SnapshotProvider, 0, len(s.snapshotProviders))
	for _, provider := range s.snapshotProviders {
		if provider == nil {
			continue
		}
		cloned := *provider
		prepareSelectionFixtureProvider(ctx, &cloned, &bucket.GroupID)
		out = append(out, codec.WrapRecord(&cloned.Record))
	}
	return out, true, nil
}

func (s *openAISnapshotCacheStub) GetProvider(ctx context.Context, providerID int64) (schedulercore.SnapshotProvider, error) {
	if s.providersByID == nil {
		return nil, nil
	}
	provider := s.providersByID[providerID]
	if provider == nil {
		return nil, nil
	}
	cloned := *provider
	prepareSelectionFixtureProvider(ctx, &cloned, nil)
	return codec.WrapRecord(&cloned.Record), nil
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_AdvancedGroupSkipsConcurrencySlot(t *testing.T) {
	groupID := int64(10105)
	ctx := requeststate.WithGroup(context.Background(), &routing.Group{
		ID: groupID,

		SchedulerType: routing.GroupSchedulerTypeAdvanced,
		Status:        billing.StatusActive,
		Hydrated:      true,
	})
	concurrencyCache := &noSlotSchedulerTestConcurrencyCache{}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36000, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 1,
		}}}}},
		Shared: Shared{
			Cache: &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{
				Logf:  logging.LegacyPrintf,
				Event: logging.Event,
			}),
		},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)

	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(36000), provider.Record.ID)
	require.Zero(t, concurrencyCache.acquireCalls, "仅选提供商入口不得占用真实并发槽")
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabledUsesLegacyLoadAwareness(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10106)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36002,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cache := &schedulerTestGatewayCache{}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache: cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{
				Logf:  logging.LegacyPrintf,
				Event: logging.Event,
			}),
		},
	}, cfg)

	store := svc.ResponseStateStore()
	require.NoError(t, store.BindResponseProvider(ctx, groupID, "resp_disabled_001", 36001, time.Hour))

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"resp_disabled_001",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(36002), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickyPreviousHit)
}

// TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabled_LoadBatchReportsFilterReasons 验证回归：legacy 负载批处理路径有两个直接返回 ErrNoAvailableProviders 的出口，
// 绕过了高级调度器和非批处理 legacy 选择器的诊断。启用负载批处理时默认会走这里，
// 配额自动暂停不应再只表现为无法定位原因的 503。
func TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabled_LoadBatchReportsFilterReasons(t *testing.T) {
	ctx := gatewayprovider.WithQuotaAutoPauseSettings(context.Background(), ops.OpsOpenAIProviderQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
	groupID := int64(10107)
	quotaPaused := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36003,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Extra: map[string]any{
				"codex_7d_used_percent":  95.0,
				"codex_7d_reset_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
		},
	}
	mappingMiss := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 36004,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-4o": "gpt-4o"},
			},
		},
	}
	excluded := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36005,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{quotaPaused, mappingMiss, excluded}}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, cfg)

	require.False(t, svc.groupUsesAdvancedScheduler(ctx, &groupID))
	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.4-mini",
		map[int64]struct{}{excluded.Record.ID: {}}, egress.OpenAIUpstreamTransportAny, false,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, schedulercore.ErrNoAvailableProviders)
	require.Nil(t, selection)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.EqualError(t, err, "no available OpenAI providers supporting model: gpt-5.4-mini (pool=3, filtered: excluded=1 model_not_supported=1 quota_auto_pause_7d=1)")
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabled_RequiredWSV2_SkipsHTTPOnlyProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10108)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36011,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36012,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportResponsesWebsocketV2, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(36012), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabled_RequiredWSV2_NoAvailableProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10109)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36021,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportResponsesWebsocketV2, false,
	)
	require.ErrorContains(t, err, "no available OpenAI providers")
	require.Nil(t, selection)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabled_EmbeddingsSkipsChatOnlyProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10110)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 36031,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				Credentials: map[string]any{
					"model_whitelist":              []string{"*"},
					"openai_workload_capabilities": []any{"text_generation"},
				},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 36032,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				Credentials: map[string]any{
					"model_whitelist":              []string{"*"},
					"openai_workload_capabilities": []any{"text_generation", "embeddings"},
				},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"text-embedding-3-small",
		nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityEmbeddings,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(36032), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectProviderForTokenCount_DoesNotAcquireGenerationSlot(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10115)
	acquiredIDs := make([]int64, 0)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 36501, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "openai_capabilities": []any{"chat_completions"}},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 36502, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "openai_capabilities": []any{"embeddings"}},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 36503, Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "openai_capabilities": []any{"chat_completions"}},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 36504, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 15,
				Credentials: map[string]any{
					"openai_capabilities": []any{"chat_completions"},
					"model_mapping":       map[string]any{"gpt-4o": "gpt-4o"},
				},
			},
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{36501: false}, acquiredIDs: &acquiredIDs}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, &config.Config{})

	provider, err := svc.SelectProviderForTokenCount(
		ctx,
		&groupID,
		"",
		"gpt-5.1",
		providercore.OpenAIEndpointCapabilityTextGeneration,
		capability.PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(36501), provider.Record.ID)
	require.Empty(t, acquiredIDs, "token counting must not acquire a generation slot")
}

// TestOpenAIGatewayService_SelectProviderWithScheduler_ResponsesCapabilityExcludesUnsupportedAPIKey 验证生图意图的 /v1/responses 请求要求 OpenAIEndpointCapabilityResponses：管理员声明
// 不支持 Responses API 的 APIKey 提供商必须被排除，避免 forward 阶段降级为无法生图
// 的 Chat Completions 直转（#4417）。
func TestOpenAIGatewayService_SelectProviderWithScheduler_ResponsesCapabilityExcludesUnsupportedAPIKey(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10120)

	newSvc := func(providers []gatewayprovider.ExecutionProvider) *Compatible {
		cfg := &config.Config{}
		cfg.Gateway.Scheduling.LoadBatchEnabled = false
		return newCompatibleSelectionForTest(CompatibleDependencies{
			Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
			Shared: Shared{
				Cache:       &schedulerTestGatewayCache{},
				Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)
	}

	supported := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
		},
	}
	// 此提供商优先级更高且仅允许 Chat，能力检查失效时会被优先选中。
	unsupported := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
			Extra: map[string]any{"openai_text_route_mode": "force_chat_completions"},
		},
	}

	t.Run("生图意图仅选中支持 responses 的提供商", func(t *testing.T) {
		svc := newSvc([]gatewayprovider.ExecutionProvider{supported, unsupported})
		selection, _, err := svc.SelectProviderWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-image-2", nil, egress.OpenAIUpstreamTransportAny, providercore.OpenAIEndpointCapabilityResponses,
			false, false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Provider)
		require.Equal(t, int64(37001), selection.Provider.Record.ID)
	})

	t.Run("仅有不支持 responses 的提供商时生图意图无可用提供商", func(t *testing.T) {
		svc := newSvc([]gatewayprovider.ExecutionProvider{unsupported})
		selection, _, err := svc.SelectProviderWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-image-2", nil, egress.OpenAIUpstreamTransportAny, providercore.OpenAIEndpointCapabilityResponses,
			false, false,
		)
		require.Error(t, err)
		require.Nil(t, selection)
	})

	t.Run("非生图路径仍可选中不支持 responses 的提供商", func(t *testing.T) {
		svc := newSvc([]gatewayprovider.ExecutionProvider{unsupported})
		selection, _, err := svc.SelectProviderWithSchedulerForCapability(
			ctx, &groupID, "", "", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, providercore.OpenAIEndpointCapabilityTextGeneration,
			false, false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Provider)
		require.Equal(t, int64(37002), selection.Provider.Record.ID)
	})
}

// TestOpenAIGatewayService_SelectProviderWithScheduler_AlphaSearchAllowsAPIKeyProvider 验证alpha/search 调度必须同时放行 OAuth 与 APIKey 提供商：v0.1.157 曾因 OAuth-only
// 门控把 APIKey 提供商从候选池剔除，纯 APIKey 分组的独立搜索请求在选号阶段就
// 报无可用提供商，Codex 网页搜索整体失效（转发层其实一直支持 APIKey 路径）。
func TestOpenAIGatewayService_SelectProviderWithScheduler_AlphaSearchAllowsAPIKeyProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10125)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Cache:       &schedulerTestGatewayCache{},
		},
	}, cfg)

	selection, _, err := svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.6-sol",
		nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityAlphaSearch,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(38001), selection.Provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_DefaultDisabled_AllowsGrokChatProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10113)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36041,
				Platform:    capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"grok-4.3",
		nil, egress.OpenAIUpstreamTransportAny, providercore.OpenAIEndpointCapabilityTextGeneration,
		false,
		false,
		capability.PlatformGrok,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(36041), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_GrokMediaCapabilityFiltersIneligibleProviders(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10114)
	ineligible := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36051, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
			Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: false},
		},
	}
	eligible := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 36052, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
			Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: true},
		},
	}
	newService := func(providers []gatewayprovider.ExecutionProvider) *Compatible {
		cfg := &config.Config{}
		cfg.Gateway.Scheduling.LoadBatchEnabled = false
		return newCompatibleSelectionForTest(CompatibleDependencies{
			Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
			Shared: Shared{
				Cache:       &schedulerTestGatewayCache{},
				Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)
	}

	t.Run("media generation skips higher priority ineligible provider", func(t *testing.T) {
		selection, _, err := newService([]gatewayprovider.ExecutionProvider{ineligible, eligible}).SelectProviderWithSchedulerForCapability(
			ctx, &groupID, "", "", "grok-imagine-video", nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityGrokMediaGeneration,
			false, false, capability.PlatformGrok,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Provider)
		require.Equal(t, eligible.Record.ID, selection.Provider.Record.ID)
	})

	t.Run("media generation fails closed when all providers are ineligible", func(t *testing.T) {
		selection, _, err := newService([]gatewayprovider.ExecutionProvider{ineligible}).SelectProviderWithSchedulerForCapability(
			ctx, &groupID, "", "", "grok-imagine-video", nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityGrokMediaGeneration,
			false, false, capability.PlatformGrok,
		)

		require.Error(t, err)
		require.ErrorIs(t, err, schedulercore.ErrNoAvailableProviders)
		require.Nil(t, selection)
	})

	t.Run("chat remains routable on media-ineligible provider", func(t *testing.T) {
		selection, _, err := newService([]gatewayprovider.ExecutionProvider{ineligible}).SelectProviderWithSchedulerForCapability(
			ctx, &groupID, "", "", "grok-4.3", nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityTextGeneration,
			false, false, capability.PlatformGrok,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Provider)
		require.Equal(t, ineligible.Record.ID, selection.Provider.Record.ID)
	})
}

// TestOpenAIGatewayService_SelectProviderWithScheduler_NoAvailableErrorReportsQuotaAutoPauseExclusion 验证回归 #4599：高级调度初筛排除全部候选时，错误必须携带逐原因统计。
func TestOpenAIGatewayService_SelectProviderWithScheduler_NoAvailableErrorReportsQuotaAutoPauseExclusion(t *testing.T) {
	ctx := gatewayprovider.WithQuotaAutoPauseSettings(context.Background(), ops.OpsOpenAIProviderQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
	groupID := int64(101201)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38101,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Extra: map[string]any{
					"codex_7d_used_percent":  95.0,
					"codex_7d_reset_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
					"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
				},
			},
		},
	}
	cfg := &config.Config{}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),

			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx, &groupID, "", "", "gpt-5.4-mini", nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, schedulercore.ErrNoAvailableProviders)
	require.Nil(t, selection)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.EqualError(t, err, "no available OpenAI providers supporting model: gpt-5.4-mini (pool=1, filtered: quota_auto_pause_7d=1)")
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_NoAvailableErrorPreservesModelBusinessError(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101202)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38111,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
			},
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Cache:       &schedulerTestGatewayCache{},
		},
	}, &config.Config{})

	selection, _, err := svc.SelectProviderWithScheduler(
		ctx, &groupID, "", "", "grok-4.5", nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.Nil(t, selection)
	var modelErr *routing.GroupModelUnsupportedError
	require.ErrorAs(t, err, &modelErr)

	scheduler := &compatiblePicker{service: svc}
	compatible, reason := scheduler.isProviderRequestCompatibleReason(ctx, &providers[0], schedulercore.PlatformSelectionInput{RequestedModel: "grok-4.5"})
	require.False(t, compatible)
	require.Equal(t, "model_not_supported", reason)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_NoAvailableErrorAggregatesReasonsDeterministically(t *testing.T) {
	ctx := gatewayprovider.WithQuotaAutoPauseSettings(context.Background(), ops.OpsOpenAIProviderQuotaAutoPauseSettings{DefaultThreshold7d: 0.9})
	groupID := int64(101203)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	quotaPaused := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38121,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Extra: map[string]any{
				"codex_7d_used_percent":  95.0,
				"codex_7d_reset_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
		},
	}
	mappingMiss := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 38122,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-4o": "gpt-4o"},
			},
		},
	}
	excluded := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38123,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{quotaPaused, mappingMiss, excluded}}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
		},
	}, &config.Config{})

	selection, _, err := svc.SelectProviderWithScheduler(
		ctx, &groupID, "", "", "gpt-5.4-mini", map[int64]struct{}{38123: {}}, egress.OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, schedulercore.ErrNoAvailableProviders)
	require.Nil(t, selection)
	require.EqualError(t, err, "no available OpenAI providers supporting model: gpt-5.4-mini (pool=3, filtered: excluded=1 model_not_supported=1 quota_auto_pause_7d=1)")
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_NoAvailableErrorReportsEmptyPool(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101204)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{}},
		Shared: Shared{
			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Cache:      &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{
				Logf:  logging.LegacyPrintf,
				Event: logging.Event,
			}),
		},
	}, &config.Config{})

	selection, _, err := svc.SelectProviderWithScheduler(
		ctx, &groupID, "", "", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, schedulercore.ErrNoAvailableProviders)
	require.Nil(t, selection)
	require.EqualError(t, err, "no available OpenAI providers supporting model: gpt-5.1 (pool=0)")
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_EnabledUsesAdvancedPreviousResponseRouting(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10107)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37002,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),

			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	store := svc.ResponseStateStore()
	require.NoError(t, store.BindResponseProvider(ctx, groupID, "resp_enabled_001", 37001, time.Hour))

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"resp_enabled_001",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37001), selection.Provider.Record.ID)
	require.Equal(t, "previous_response_id", decision.Layer)
	require.True(t, decision.StickyPreviousHit)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_StickyWeightedSessionUsesTopKSampling(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101071)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37101,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    100,
				GroupIDs:    []int64{groupID},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37102,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 2
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 0.7
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0.8
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0.5
	cfg.Gateway.AdvancedScheduler.ScoreWeights.SessionSticky = 3
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
	for index := 0; index < 128; index++ {
		cache.sessionBindings["openai:"+fmt.Sprintf("session_hash_weighted_topk_%d", index)] = 37101
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true", "true"),
			Cache:       cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, cfg)

	var observedSticky, observedNonSticky bool
	for index := 0; index < 128; index++ {
		selection, decision, err := svc.SelectProviderWithScheduler(
			ctx,
			&groupID,
			"",
			fmt.Sprintf("session_hash_weighted_topk_%d", index),
			"gpt-5.1",
			nil, egress.OpenAIUpstreamTransportAny, false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Provider)
		require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
		require.Equal(t, 2, decision.TopK)
		observedSticky = observedSticky || selection.Provider.Record.ID == 37101
		observedNonSticky = observedNonSticky || selection.Provider.Record.ID == 37102
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}
	require.True(t, observedSticky, "粘性加分提供商仍应参与抽样")
	require.True(t, observedNonSticky, "OpenAI 选择器不能把加权粘性强制置首")
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_StickyWeightedPreviousRequiresMovableContext(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101072)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37111,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    100,
				GroupIDs:    []int64{groupID},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37112,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.AdvancedScheduler.LBTopK = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 0.7
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0.8
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0.5
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),

			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true", "true"),
		},
	}, cfg)

	store := svc.ResponseStateStore()
	require.NoError(t, store.BindResponseProvider(ctx, groupID, "resp_weighted_unmovable", 37111, time.Hour))

	selection, decision, err := svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"resp_weighted_unmovable",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, providercore.OpenAIEndpointCapabilityTextGeneration,
		false,
		false,
		capability.PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37111), selection.Provider.Record.ID)
	require.Equal(t, "previous_response_id", decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	selection, decision, err = svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"resp_weighted_unmovable",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, providercore.OpenAIEndpointCapabilityTextGeneration,
		false,
		true,
		capability.PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37112), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickyPreviousHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_PreviousResponseCompactUnsupportedDeletesBinding(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101073)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37121,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
					"openai_compact_mode":                           providercore.OpenAICompactModeForceOff,
				},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37122,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    10,
				GroupIDs:    []int64{groupID},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
					"openai_compact_mode":                           providercore.OpenAICompactModeForceOn,
				},
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.AdvancedScheduler.LBTopK = 2
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 0.7
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0.8
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0.5
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:      &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{
				Logf:  logging.LegacyPrintf,
				Event: logging.Event,
			}),
		},
	}, cfg)

	store := svc.ResponseStateStore()
	require.NoError(t, store.BindResponseProvider(ctx, groupID, "resp_compact_unsupported", 37121, time.Hour))

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"resp_compact_unsupported",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37122), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickyPreviousHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	providerID, err := store.GetResponseProvider(ctx, groupID, "resp_compact_unsupported")
	require.NoError(t, err)
	require.Zero(t, providerID)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_Enabled_EmbeddingsSkipsChatOnlyProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10111)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 37011,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				Credentials: map[string]any{
					"model_whitelist":              []string{"*"},
					"openai_workload_capabilities": []any{"text_generation"},
				},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 37012,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				Credentials: map[string]any{
					"model_whitelist":              []string{"*"},
					"openai_workload_capabilities": []any{"text_generation", "embeddings"},
				},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),

			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"",
		"",
		"text-embedding-3-small",
		nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityEmbeddings,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37012), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 1, decision.CandidateCount)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_Enabled_EmbeddingsSkipsChatOnlyStickyBindings(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10112)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 37021,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				Credentials: map[string]any{
					"model_whitelist":              []string{"*"},
					"openai_workload_capabilities": []any{"text_generation"},
				},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 37022,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				Credentials: map[string]any{
					"model_whitelist":              []string{"*"},
					"openai_workload_capabilities": []any{"text_generation", "embeddings"},
				},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_embeddings": 37021,
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache: cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{
				Logf:  logging.LegacyPrintf,
				Event: logging.Event,
			}),
			Health: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg,
				"true"),
		},
	}, cfg)

	store := svc.ResponseStateStore()
	require.NoError(t, store.BindResponseProvider(ctx, groupID, "resp_embeddings_chat_only", 37021, time.Hour))

	selection, decision, err := svc.SelectProviderWithSchedulerForCapability(
		ctx,
		&groupID,
		"resp_embeddings_chat_only",
		"session_hash_embeddings",
		"text-embedding-3-small",
		nil, egress.OpenAIUpstreamTransportHTTPSSE, providercore.OpenAIEndpointCapabilityEmbeddings,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37022), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickyPreviousHit)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(37022), cache.sessionBindings["openai:session_hash_embeddings"])
}

func TestOpenAIGatewayService_OpenAIProviderSchedulerMetrics_DisabledNoOp(t *testing.T) {
	svc := newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{}, Shared: Shared{}}, nil)

	ttft := 120
	svc.ReportOpenAIProviderScheduleResult(&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 10}}, "", true, &ttft)
	svc.RecordOpenAIProviderSwitch()

	snapshot := svc.SnapshotOpenAIProviderSchedulerMetrics()
	require.Equal(t, schedulercore.PlatformMetricsSnapshot{}, snapshot)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SkipsQuarantinedSharedProxy(t *testing.T) {
	proxyA := int64(4698)
	proxyB := int64(4699)
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 469801, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, ProxyID: &proxyA}},
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 469802, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, ProxyID: &proxyA}},
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 469803, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, ProxyID: &proxyB}},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event})},
		ProxyCircuit: egress.NewProxyStreamCircuit(egress.ProxyStreamCircuitSettings{FailureThreshold: 1, FailureWindow: time.Minute, QuarantineTTL: 10 *
			time.Minute, MaxEntries: 16}),
	}, cfg)

	svc.proxyCircuit.RecordFailure(proxyA, time.Now())

	selection, _, err := svc.SelectProviderWithScheduler(
		context.Background(), selectionFixtureGroupID(context.Background()), "", "", "gpt-5.6-sol", nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(469803), selection.Provider.Record.ID)
}

// TestOpenAIGatewayService_SelectProviderWithScheduler_FailsOpenWhenAllProxiesQuarantined 检查所有候选的代理均被隔离时，是否放宽隔离限制继续选择。
func TestOpenAIGatewayService_SelectProviderWithScheduler_FailsOpenWhenAllProxiesQuarantined(t *testing.T) {
	proxyID := int64(5056)
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 505601, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, ProxyID: &proxyID}},
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 505602, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, ProxyID: &proxyID}},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event})},
		ProxyCircuit: egress.NewProxyStreamCircuit(egress.ProxyStreamCircuitSettings{FailureThreshold: 1, FailureWindow: time.Minute, QuarantineTTL: 10 *
			time.Minute, MaxEntries: 16}),
	}, cfg)

	tripped, _ := svc.proxyCircuit.RecordFailure(proxyID, time.Now())
	require.True(t, tripped)

	selection, _, err := svc.SelectProviderWithScheduler(
		context.Background(), selectionFixtureGroupID(context.Background()), "", "", "gpt-5.6-sol", nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err, "代理隔离不能导致无可用提供商")
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.NotNil(t, selection.Provider.Record.ProxyID)
	require.Equal(t, proxyID, *selection.Provider.Record.ProxyID)
	require.True(t, svc.proxyCircuit.IsBlocked(proxyID, time.Now()),
		"fail-open 只影响本次调度，不应清除隔离状态")
}

// TestOpenAIGatewayService_SelectProviderWithSchedulerForRouting_FailsOpenWhenAllProxiesQuarantined 检查指定 routingModel 的入口是否执行 fail-open 二次调度。
func TestOpenAIGatewayService_SelectProviderWithSchedulerForRouting_FailsOpenWhenAllProxiesQuarantined(t *testing.T) {
	proxyID := int64(5057)
	provider := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 505701, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, ProxyID: &proxyID}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:        Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}},
		Shared:       Shared{Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event})},
		ProxyCircuit: egress.NewProxyStreamCircuit(egress.ProxyStreamCircuitSettings{FailureThreshold: 1, FailureWindow: time.Minute, QuarantineTTL: 10 * time.Minute, MaxEntries: 16}),
	}, cfg)

	svc.proxyCircuit.RecordFailure(proxyID, time.Now())

	selection, _, err := svc.SelectProviderWithSchedulerForCapabilityAndRoutingModel(
		context.Background(), selectionFixtureGroupID(context.Background()), "", "", "client-alias", "gpt-5.6-sol", nil, egress.OpenAIUpstreamTransportAny, "", false, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, provider.Record.ID, selection.Provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyRateLimitedProviderFallsBackToFreshCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10101)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleSticky := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 31001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}}
	staleBackup := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 31002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	freshSticky := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 31001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}}
	freshBackup := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 31002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_rate_limited": 31001}}
	snapshotCache := &openAISnapshotCacheStub{snapshotProviders: []*gatewayprovider.ExecutionProvider{staleSticky, staleBackup}, providersByID: map[int64]*gatewayprovider.ExecutionProvider{31001: freshSticky, 31002: freshBackup}}
	snapshotService := schedulercore.NewSnapshotService(snapshotCache, nil, nil, nil, nil)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{*freshSticky, *freshBackup}},
			Snapshot:  schedulerredis.NewSnapshotReader(snapshotService),
		},
		Shared: Shared{
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Cache:       cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, &config.Config{})

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_rate_limited", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(31002), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_AutoPauseBy5hThreshold(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35001,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   95.0,
				"auto_pause_5h_threshold": 0.95,
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35002), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_AllowsBelow5hThreshold(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35101,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   80.0,
				"auto_pause_5h_threshold": 0.95,
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35102, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35101), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_AutoPauseBy7dThreshold(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35201,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_7d_used_percent":   95.0,
				"auto_pause_7d_threshold": 0.95,
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35202, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35202), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_UnconfiguredThresholdKeepsLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35301, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, Extra: map[string]any{"codex_5h_used_percent": 99.0, "codex_7d_used_percent": 99.0}}}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35302, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35301), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_UsesGlobalDefaultThreshold(t *testing.T) {
	ctx := gatewayprovider.WithQuotaAutoPauseSettings(context.Background(), ops.OpsOpenAIProviderQuotaAutoPauseSettings{DefaultThreshold5h: 0.95})
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35401,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent": 95.0,
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35402, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35402), provider.Record.ID)
}

// TestOpenAIGatewayService_SelectProviderForModelWithExclusions_PerProviderDisableOverridesGlobalDefault 检查提供商禁用自动暂停时，是否覆盖全局阈值。
// 否则“阈值留空”会静默回退到全局默认值，管理员无法单独白名单某个提供商。
func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_PerProviderDisableOverridesGlobalDefault(t *testing.T) {
	ctx := gatewayprovider.WithQuotaAutoPauseSettings(context.Background(), ops.OpsOpenAIProviderQuotaAutoPauseSettings{DefaultThreshold5h: 0.95})
	// 提供商用量很高且没有提供商级阈值（通常会回退到全局默认并被暂停），
	// 此提供商配置了禁用自动暂停标记。
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35701,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":  99.0,
				"auto_pause_5h_disabled": true,
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35702, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35701), provider.Record.ID)
}

// TestOpenAIGatewayService_SelectProviderForModelWithExclusions_PerWindowDisableScoped 验证禁用标记按窗口生效：只禁用 5h 时，7d 自动暂停仍应触发。
func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_PerWindowDisableScoped(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35801,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   99.0,
				"codex_7d_used_percent":   99.0,
				"auto_pause_5h_disabled":  true,
				"auto_pause_7d_threshold": 0.95,
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35802, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35802), provider.Record.ID, "7d auto-pause must still fire even though 5h is disabled")
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_StaleUsageWindowResetSkipsPause(t *testing.T) {
	ctx := context.Background()
	// 用量超过阈值，但窗口重置时间已过，因此缓存百分比已经过期（真实窗口已滚动），
	// 提供商不能继续暂停；否则它可能因为没有流量刷新而被永久跳过。
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35501,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   99.0,
				"auto_pause_5h_threshold": 0.95,
				"codex_5h_reset_at":       time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35502, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35501), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_FreshUsageWindowStillPauses(t *testing.T) {
	ctx := context.Background()
	// 与上面相同，但窗口尚未重置，因此提供商仍应保持暂停。
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35601,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   99.0,
				"auto_pause_5h_threshold": 0.95,
				"codex_5h_reset_at":       time.Now().Add(time.Hour).Format(time.RFC3339),
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35602, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35602), provider.Record.ID)
}

// TestOpenAIGatewayService_SelectProviderForModelWithExclusions_StaleUsageSnapshotSkipsPause_Issue2994 检查过期用量快照是否允许探测请求。
// 提供商停调后缺少流量刷新快照，允许请求可用响应头更新用量。快照有效期按观测时间判断。
func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_StaleUsageSnapshotSkipsPause_Issue2994(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35701,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   99.0,
				"auto_pause_5h_threshold": 0.95,
				// 窗口尚未重置，因此 reset 保护不会生效。
				"codex_5h_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339),
				// 快照已经陈旧：早于 openAICodexAutoPauseStaleAfter（2h）。
				"codex_usage_updated_at": time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35702, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35701), provider.Record.ID)
}

// TestOpenAIGatewayService_SelectProviderForModelWithExclusions_FreshExhaustedSnapshotStillPauses_Issue2994 检查快照刚刷新且配额耗尽时是否自动暂停提供商。
// 快照仍在有效期内且已用 99% 时继续执行暂停。
func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_FreshExhaustedSnapshotStillPauses_Issue2994(t *testing.T) {
	ctx := context.Background()
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35801,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"codex_5h_used_percent":   99.0,
				"auto_pause_5h_threshold": 0.95,
				"codex_5h_reset_at":       time.Now().Add(time.Hour).Format(time.RFC3339),
				// 快照 1 分钟前刚刷新：未陈旧，因此提供商保持暂停。
				"codex_usage_updated_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 35802, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(35802), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_SkipsFreshlyRateLimitedSnapshotCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10102)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	stalePrimary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 32001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}}
	staleSecondary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 32002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	freshPrimary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 32001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}}
	freshSecondary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 32002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	snapshotCache := &openAISnapshotCacheStub{snapshotProviders: []*gatewayprovider.ExecutionProvider{stalePrimary, staleSecondary}, providersByID: map[int64]*gatewayprovider.ExecutionProvider{32001: freshPrimary, 32002: freshSecondary}}
	snapshotService := schedulercore.NewSnapshotService(snapshotCache, nil, nil, nil, nil)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{*freshPrimary, *freshSecondary}},
			Snapshot:  schedulerredis.NewSnapshotReader(snapshotService),
		},
		Shared: Shared{
			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
		},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(32002), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_ModelRateLimitOnlySkipsThatModel(t *testing.T) {
	ctx := context.Background()
	resetAt := time.Now().Add(30 * time.Minute).Format(time.RFC3339)
	primary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 32101,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"model_rate_limits": map[string]any{
					"gpt-5.4": map[string]any{
						"rate_limit_reset_at": resetAt,
					},
				},
			},
		},
	}
	secondary := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 32102,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{primary, secondary}}},
		Shared: Shared{},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.4", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(32102), provider.Record.ID)

	provider, err = svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "gpt-5.3", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(32101), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyDBRuntimeRecheckSkipsStaleCachedProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10103)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleSticky := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 33001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}}
	staleBackup := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 33002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	dbSticky := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 33001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}}
	dbBackup := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 33002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_db_runtime_recheck": 33001}}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotProviders: []*gatewayprovider.ExecutionProvider{staleSticky, staleBackup},
		providersByID:     map[int64]*gatewayprovider.ExecutionProvider{33001: staleSticky, 33002: staleBackup},
	}
	snapshotService := schedulercore.NewSnapshotService(snapshotCache, nil, nil, nil, nil)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{dbSticky, dbBackup}},
			Snapshot: schedulerredis.NewSnapshotReader(
				snapshotService,
			),
		},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Cache:       cache,
		},
	}, &config.Config{})

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_db_runtime_recheck", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(33002), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectProviderForModelWithExclusions_DBRuntimeRecheckSkipsStaleCachedCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10104)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	stalePrimary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}}
	staleSecondary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	dbPrimary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34001, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}, RateLimitResetAt: &rateLimitedUntil}}
	dbSecondary := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34002, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotProviders: []*gatewayprovider.ExecutionProvider{stalePrimary, staleSecondary},
		providersByID:     map[int64]*gatewayprovider.ExecutionProvider{34001: stalePrimary, 34002: staleSecondary},
	}
	snapshotService := schedulercore.NewSnapshotService(snapshotCache, nil, nil, nil, nil)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{dbPrimary, dbSecondary}},
			Snapshot:  schedulerredis.NewSnapshotReader(snapshotService),
		},
		Shared: Shared{
			Parameters: newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, &config.Config{})

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(34002), provider.Record.ID)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_DBFreshGroupRecheckReleasesMovedProvider(t *testing.T) {
	ctx := context.Background()
	groupID, otherGroupID := int64(10105), int64(10106)
	stalePrimary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34101, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}}
	staleBackup := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34102, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 10, GroupIDs: []int64{groupID}}}
	dbPrimary := *stalePrimary
	dbPrimary.Record.GroupIDs = []int64{otherGroupID}
	dbBackup := *staleBackup
	snapshotCache := &openAISnapshotCacheStub{
		snapshotProviders: []*gatewayprovider.ExecutionProvider{stalePrimary, staleBackup},
		providersByID:     map[int64]*gatewayprovider.ExecutionProvider{stalePrimary.Record.ID: stalePrimary, staleBackup.Record.ID: staleBackup},
	}
	acquiredIDs, releasedIDs := []int64{}, []int64{}
	cfg := &config.Config{}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{dbPrimary, dbBackup}},
			Snapshot: schedulerredis.NewSnapshotReader(
				schedulercore.NewSnapshotService(snapshotCache, nil, nil, nil, nil)),
		},

		Shared: Shared{Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquiredIDs, releasedIDs: &releasedIDs}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event})},
	}, cfg)

	scheduler := &compatiblePicker{service: svc}

	core, scope := scheduler.platformSelector()
	candidates := []schedulercore.PlatformCandidateScore{
		{Provider: scope.provider(stalePrimary), LoadInfo: &schedulercore.ProviderLoadInfo{ProviderID: stalePrimary.Record.ID}},
		{Provider: scope.provider(staleBackup), LoadInfo: &schedulercore.ProviderLoadInfo{ProviderID: staleBackup.Record.ID}},
	}
	result, _, err := core.TryOrderBounded(ctx, schedulercore.PlatformSelectionInput{GroupID: &groupID, Platform: capability.PlatformOpenAI, RequestedModel: "gpt-5.1"}, candidates)
	selection := scope.restore(result)

	require.NoError(t, err)
	require.Equal(t, staleBackup.Record.ID, selection.Provider.Record.ID)
	require.Equal(t, []int64{stalePrimary.Record.ID, staleBackup.Record.ID}, acquiredIDs)
	require.Equal(t, []int64{stalePrimary.Record.ID}, releasedIDs)
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_SelectProviderWithLoadAwareness_DBFreshGroupRecheckWaitsOnValidProvider(t *testing.T) {
	ctx := context.Background()
	groupID, otherGroupID := int64(10107), int64(10108)
	stalePrimary := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34201, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}}
	staleBackup := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34202, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 10, GroupIDs: []int64{groupID}}}
	dbPrimary := *stalePrimary
	dbPrimary.Record.GroupIDs = []int64{otherGroupID}
	dbBackup := *staleBackup
	snapshotCache := &openAISnapshotCacheStub{
		snapshotProviders: []*gatewayprovider.ExecutionProvider{stalePrimary, staleBackup},
		providersByID:     map[int64]*gatewayprovider.ExecutionProvider{stalePrimary.Record.ID: stalePrimary, staleBackup.Record.ID: staleBackup},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Snapshot: schedulerredis.NewSnapshotReader(schedulercore.NewSnapshotService(snapshotCache,

				nil, nil, nil, nil)),
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{dbPrimary, dbBackup}},
		},

		Shared: Shared{Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{staleBackup.Record.ID: false}}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event})},
	}, cfg)

	selection, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, staleBackup.Record.ID, selection.Provider.Record.ID)
	require.Equal(t, staleBackup.Record.ID, selection.WaitPlan.ProviderID)
}

func TestOpenAIGatewayService_RecheckSelectedOpenAIProviderFromDB_KeepsGroupBoundary(t *testing.T) {
	grouped := gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 34301, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{99}}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{grouped}},
			Snapshot:  schedulerredis.NewSnapshotReader(schedulercore.NewSnapshotService(&openAISnapshotCacheStub{}, nil, nil, nil, nil)),
		},

		Shared: Shared{},
	}, &config.Config{})

	requestedGroupID := int64(100)

	for _, groupID := range []*int64{nil, &requestedGroupID} {
		fresh := svc.recheckSelectedOpenAIProviderFromDB(context.Background(), &grouped, groupID, capability.PlatformOpenAI, "gpt-5.1", false, "")
		require.Nil(t, fresh)
	}

	ungrouped := grouped
	ungrouped.Record.ID++
	ungrouped.Record.GroupIDs = nil
	standardSvc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{grouped, ungrouped}},
			Snapshot: schedulerredis.NewSnapshotReader(
				schedulercore.NewSnapshotService(&openAISnapshotCacheStub{}, nil, nil,
					nil, nil)),
		},
		Shared: Shared{},
	}, &config.Config{})

	require.Nil(t, standardSvc.recheckSelectedOpenAIProviderFromDB(context.Background(), &grouped, nil, capability.PlatformOpenAI, "gpt-5.1", false, ""))
	require.Nil(t, standardSvc.recheckSelectedOpenAIProviderFromDB(context.Background(), &ungrouped, nil, capability.PlatformOpenAI, "gpt-5.1", false, ""))
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_PreviousResponseSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(9)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1001,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 2,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cache := &schedulerTestGatewayCache{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 1800
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}},
		Shared: Shared{
			Cache:       cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	store := svc.ResponseStateStore()
	require.NoError(t, store.BindResponseProvider(ctx, groupID, "resp_prev_001", provider.Record.ID, time.Hour))

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"resp_prev_001",
		"session_hash_001",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, provider.Record.ID, selection.Provider.Record.ID)
	require.Equal(t, "previous_response_id", decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	require.Equal(t, provider.Record.ID, cache.sessionBindings["openai:session_hash_001"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2001,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			GroupIDs:    []int64{groupID},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_abc": provider.Record.ID,
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}},
		Shared: Shared{
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Cache:       cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
		},
	}, &config.Config{})

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_abc",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, provider.Record.ID, selection.Provider.Record.ID)
	require.Equal(t, "session_hash", decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyBusyKeepsSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10100)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21002,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    9,
				GroupIDs:    []int64{groupID},
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_sticky_busy": 21001,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	cfg.Gateway.AdvancedScheduler.StickyEscapeEnabled = false
	cfg.Gateway.AdvancedScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.AdvancedScheduler.StickyEscapeErrorRate = 0.5
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true

	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{
			21001: false, // sticky 提供商已满
			21002: true,  // 若回退负载均衡会命中该提供商（本测试要求不能切换）
		},
		waitCounts: map[int64]int{
			21001: 999,
		},
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			21001: {ProviderID: 21001, LoadRate: 90, WaitingCount: 9},
			21002: {ProviderID: 21002, LoadRate: 1, WaitingCount: 0},
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:      cache,

			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache,
				schedulercore.Diagnostics{
					Logf:  logging.LegacyPrintf,
					Event: logging.Event,
				}),
			Health: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, cfg,
	)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_sticky_busy",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21001), selection.Provider.Record.ID, "busy sticky provider should remain selected")
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(21001), selection.WaitPlan.ProviderID)
	require.Equal(t, "session_hash", decision.Layer)
	require.True(t, decision.StickySessionHit)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyEscapeByTTFT(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10101)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21101,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21102,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				GroupIDs:    []int64{groupID},
			},
		},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_ttft": 21101}}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.StickyEscapeEnabled = true
	cfg.Gateway.AdvancedScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.AdvancedScheduler.StickyEscapeErrorRate = 0.5
	concurrencyCache := schedulerTestConcurrencyCache{acquireResults: map[int64]bool{21102: true}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:       cache,
		},
	}, cfg,
	)

	svc.openaiProviderStats = schedulercore.NewRuntimeStats(time.Now)
	fastTTFT := 14999
	svc.openaiProviderStats.Report(21101, true, &fastTTFT)
	stableTTFT := 14999
	svc.openaiProviderStats.Report(21101, true, &stableTTFT)

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_sticky_ttft", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21101), selection.Provider.Record.ID)
	require.Equal(t, "session_hash", decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	slowTTFT := 20000
	for i := 0; i < 3; i++ {
		svc.openaiProviderStats.Report(21101, true, &slowTTFT)
	}

	selection, decision, err = svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_sticky_ttft", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21102), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(21101), cache.sessionBindings["openai:session_hash_sticky_ttft"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyEscapeByErrorRate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10102)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21201, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}},
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21202, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_error_rate": 21201}}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.StickyEscapeEnabled = true
	cfg.Gateway.AdvancedScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.AdvancedScheduler.StickyEscapeErrorRate = 0.7
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:      cache,

			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{21202: true}}, schedulercore.Diagnostics{
				Logf:  logging.LegacyPrintf,
				Event: logging.Event,
			}),
			Health: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, cfg)

	svc.openaiProviderStats = schedulercore.NewRuntimeStats(time.Now)
	for i := 0; i < 5; i++ {
		svc.openaiProviderStats.Report(21201, false, nil)
	}
	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_sticky_error_rate", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21201), selection.Provider.Record.ID)
	require.Equal(t, "session_hash", decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	svc.openaiProviderStats.Report(21201, false, nil)

	selection, decision, err = svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_sticky_error_rate", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21202), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(21201), cache.sessionBindings["openai:session_hash_sticky_error_rate"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyBusyEscapes(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10103)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21301, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}},
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21302, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_busy_escape": 21301}}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.StickyEscapeEnabled = true
	cfg.Gateway.AdvancedScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.AdvancedScheduler.StickyEscapeErrorRate = 0.5
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21301: false, 21302: true},
		waitCounts:     map[int64]int{21301: 999},
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			21301: {ProviderID: 21301, LoadRate: 95, WaitingCount: 9},
			21302: {ProviderID: 21302, LoadRate: 1, WaitingCount: 0},
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache: cache,
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache,

				schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg,
	)

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_sticky_busy_escape", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21302), selection.Provider.Record.ID)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionStickyEscapeDisabledKeepsLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10104)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21401, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}},
		{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21402, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_sticky_disabled": 21401}}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.StickyEscapeEnabled = false
	cfg.Gateway.AdvancedScheduler.StickyEscapeTTFTMs = 15000
	cfg.Gateway.AdvancedScheduler.StickyEscapeErrorRate = 0.5
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21401: false, 21402: true},
		waitCounts:     map[int64]int{21401: 999},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:      cache,

			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache,
				schedulercore.Diagnostics{
					Logf:  logging.LegacyPrintf,
					Event: logging.Event,
				}),
			Health: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, cfg,
	)

	svc.openaiProviderStats = schedulercore.NewRuntimeStats(time.Now)
	slowTTFT := 20000
	svc.openaiProviderStats.Report(21401, true, &slowTTFT)
	for i := 0; i < 5; i++ {
		svc.openaiProviderStats.Report(21401, false, nil)
	}

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_sticky_disabled", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21401), selection.Provider.Record.ID)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(21401), selection.WaitPlan.ProviderID)
	require.Equal(t, "session_hash", decision.Layer)
	require.True(t, decision.StickySessionHit)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SubscriptionPriorityChoosesSubscriptionPoolFirst(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10120)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	apiKey := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21602, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
			Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
			GroupIDs: []int64{groupID},
		},
	}
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 21601,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    10,
				GroupIDs:    []int64{groupID},
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "plan_type": "plus"},
			},
		},
		*apiKey,
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21601: true, 21602: true},
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			21601: {ProviderID: 21601, LoadRate: 90, WaitingCount: 1},
			21602: {ProviderID: 21602, LoadRate: 0, WaitingCount: 0},
		},
	}
	cfg := newSchedulerTestSubscriptionPriorityConfig()
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true", "", "true"),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_subscription_first", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21601), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 1, decision.TopK)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SubscriptionPriorityFallsBackWhenSubscriptionFull(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10121)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 21611,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "plan_type": "team"},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21612,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    9,
				GroupIDs:    []int64{groupID},
			},
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21611: false, 21612: true},
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			21611: {ProviderID: 21611, LoadRate: 0, WaitingCount: 0},
			21612: {ProviderID: 21612, LoadRate: 90, WaitingCount: 1},
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(newSchedulerTestSubscriptionPriorityConfig(), "true", "", "true"),
		},
	}, newSchedulerTestSubscriptionPriorityConfig())

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_subscription_fallback", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21612), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.True(t, selection.Acquired)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SubscriptionPriorityDisabledUsesScore(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10122)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 21621,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    10,
				GroupIDs:    []int64{groupID},
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "plan_type": "pro"},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21622,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
			},
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{21621: true, 21622: true},
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			21621: {ProviderID: 21621, LoadRate: 90, WaitingCount: 1},
			21622: {ProviderID: 21622, LoadRate: 0, WaitingCount: 0},
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(newSchedulerTestSubscriptionPriorityConfig(), "true", "", "false"),
		},
	}, newSchedulerTestSubscriptionPriorityConfig())

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_subscription_disabled", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21622), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_UsesProviderPriorityWithinGroupPool(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10123)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21631,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				ProviderGroups: []providercore.GroupMembership{
					{ProviderID: 21631, GroupID: groupID},
				},
				GroupIDs: []int64{groupID},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21632,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    100000,
				ProviderGroups: []providercore.GroupMembership{
					{ProviderID: 21632, GroupID: groupID},
				},
				GroupIDs: []int64{groupID},
			},
		},
	}
	cfg := newSchedulerTestSubscriptionPriorityConfig()
	cfg.Gateway.AdvancedScheduler.LBTopK = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 0
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 0
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerGroupAwareOpenAIProviderRepo{schedulerTestOpenAIProviderRepo{providers: providers}}},
		Shared: Shared{
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_group_priority", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(21631), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIProviderScheduler_SkipsProviderBlockedForRequestedModel(t *testing.T) {
	now := time.Now()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21633, Status: billing.StatusActive, Schedulable: true, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:          Reads{},
		Shared:         Shared{},
		ModelTransient: providercore.NewModelTransientState(128),
	}, nil)

	svc.modelTransient.RecordFailure(provider.Record.ID, "gpt-5.5", now)
	svc.modelTransient.RecordFailure(provider.Record.ID, "gpt-5.5", now.Add(time.Millisecond))
	scheduler := &compatiblePicker{service: svc}

	require.False(t, scheduler.isProviderRequestCompatible(context.Background(), provider, schedulercore.PlatformSelectionInput{RequestedModel: "gpt-5.5"}))
	require.True(t, scheduler.isProviderRequestCompatible(context.Background(), provider, schedulercore.PlatformSelectionInput{RequestedModel: "gpt-5.6-sol"}))
}

func TestReportOpenAIProviderScheduleResult_SuccessClearsModelTransientState(t *testing.T) {
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:          Reads{},
		Shared:         Shared{},
		ModelTransient: providercore.NewModelTransientState(128),
	}, nil)

	now := time.Now()
	svc.modelTransient.RecordFailure(21636, "gpt-5.5", now)
	svc.modelTransient.RecordFailure(21636, "gpt-5.5", now.Add(time.Millisecond))
	require.True(t, svc.modelTransient.IsBlocked(21636, "gpt-5.5", now.Add(2*time.Millisecond)))

	svc.ReportOpenAIProviderScheduleResult(&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 21636}}, "gpt-5.5", true, nil)

	require.False(t, svc.modelTransient.IsBlocked(21636, "gpt-5.5", now.Add(2*time.Millisecond)))
}

func TestDefaultOpenAIProviderScheduler_ShouldEscapeStickyProvider_ThresholdBoundary(t *testing.T) {
	stats := schedulercore.NewRuntimeStats(time.Now)
	providerID := int64(21501)
	ttft := 15000
	stats.Report(providerID, true, &ttft)
	stats.Report(providerID, false, nil)
	stats.Report(providerID, true, nil)
	reason, errorRate, observedTTFT, shouldEscape := schedulercore.ShouldEscapeSticky(stats, providerID, policy.StickyEscapeConfig{
		Enabled:   true,
		TtftMs:    15000,
		ErrorRate: 0.5,
	})
	require.False(t, shouldEscape)
	require.Empty(t, reason)
	require.InDelta(t, 0.16, errorRate, 1e-9)
	require.InDelta(t, 15000, observedTTFT, 1e-9)

	for i := 0; i < 4; i++ {
		stats.Report(providerID, false, nil)
	}
	reason, errorRate, _, shouldEscape = schedulercore.ShouldEscapeSticky(stats, providerID, policy.StickyEscapeConfig{
		Enabled:   true,
		TtftMs:    15000,
		ErrorRate: 1,
	})
	require.False(t, shouldEscape)
	require.Empty(t, reason)
	reason, errorRate, observedTTFT, shouldEscape = schedulercore.ShouldEscapeSticky(stats, providerID, policy.StickyEscapeConfig{
		Enabled:   true,
		TtftMs:    15000,
		ErrorRate: errorRate,
	})
	require.False(t, shouldEscape)
	require.Empty(t, reason)
	require.InDelta(t, 0.655936, errorRate, 1e-9)
	require.InDelta(t, 15000, observedTTFT, 1e-9)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SessionSticky_ForceHTTP(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1010)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2101,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_ws_force_http": true,
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_force_http": provider.Record.ID,
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),

			Health:     gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters: newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
			Cache:      cache,
		},
	}, &config.Config{})

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_force_http",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, provider.Record.ID, selection.Provider.Record.ID)
	require.Equal(t, "session_hash", decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_RequiredWSV2_SkipsStickyHTTPProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1011)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2201,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2202,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				GroupIDs:    []int64{groupID},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_ws_only": 2201,
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()

	// 构造“HTTP-only 提供商负载更低”的场景，验证 required transport 会强制过滤。
	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			2201: {ProviderID: 2201, LoadRate: 0, WaitingCount: 0},
			2202: {ProviderID: 2202, LoadRate: 90, WaitingCount: 5},
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Parameters: newAdvancedSchedulerParametersForTest(cfg, "true"),
			Cache:      cache,

			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache,
				schedulercore.Diagnostics{
					Logf:  logging.LegacyPrintf,
					Event: logging.Event,
				}),
			Health: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
		},
	}, cfg,
	)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_ws_only",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportResponsesWebsocketV2, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(2202), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, 1, decision.CandidateCount)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_ClearsStickyProviderOutsideGroup(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1013)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2401,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2402,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
				ProviderGroups: []providercore.GroupMembership{
					{ProviderID: 2402, GroupID: groupID},
				},
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_removed_group": 2401,
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerGroupAwareOpenAIProviderRepo{schedulerTestOpenAIProviderRepo{providers: providers}}},
		Shared: Shared{
			Cache:       cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
		},
	}, &config.Config{})

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_removed_group",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(2402), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, 1, cache.deletedSessions["openai:session_hash_removed_group"])
	require.Equal(t, int64(2402), cache.sessionBindings["openai:session_hash_removed_group"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_RequiredWSV2_NoAvailableProvider(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1012)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2301,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
			},
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(newSchedulerTestOpenAIWSV2Config(), "true"),
			Cache:       &schedulerTestGatewayCache{},
		},
	}, newSchedulerTestOpenAIWSV2Config())

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportResponsesWebsocketV2, false,
	)
	require.Error(t, err)
	require.Nil(t, selection)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 0, decision.CandidateCount)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_LoadBalanceTopKFallback(t *testing.T) {
	ctx := context.Background()
	groupID := int64(11)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3002,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3003,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
			},
		},
	}

	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 2
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 0.4
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1.0
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 1.0
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0.2
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0.1

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			3001: {ProviderID: 3001, LoadRate: 95, WaitingCount: 8},
			3002: {ProviderID: 3002, LoadRate: 20, WaitingCount: 1},
			3003: {ProviderID: 3003, LoadRate: 10, WaitingCount: 0},
		},
		acquireResults: map[int64]bool{
			3003: false, // top1 失败，必须回退到 top-K 的下一候选
			3002: true,
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(3002), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 3, decision.CandidateCount)
	require.Equal(t, 2, decision.TopK)
	require.Greater(t, decision.LoadSkew, 0.0)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

// TestOpenAIGatewayService_SelectProviderWithScheduler_LoadBalanceTopKExcludesQuotaPaused 验证回归保护：TopK 初始过滤必须剔除配额自动暂停提供商。否则候选池会被暂停提供商填满，
// 健康提供商落到 TopK 之外，调度器会在健康提供商存在时仍返回“无可用提供商”。
func TestOpenAIGatewayService_SelectProviderWithScheduler_LoadBalanceTopKExcludesQuotaPaused(t *testing.T) {
	ctx := context.Background()
	groupID := int64(110)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				Extra: map[string]any{
					"codex_5h_used_percent":   96.0,
					"auto_pause_5h_threshold": 0.95,
				},
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 37002,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    5,
			},
		},
	}

	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 1 // TopK=1 会让问题必现：暂停提供商会完全挤掉健康提供商。
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 0.4
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1.0
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 1.0

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			37001: {ProviderID: 37001, LoadRate: 5, WaitingCount: 0},
			37002: {ProviderID: 37002, LoadRate: 5, WaitingCount: 0},
		},
		acquireResults: map[int64]bool{
			37002: true,
		},
	}

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(37002), selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	// 只有健康提供商应该进入候选池；暂停提供商必须在初始过滤阶段被剔除。
	require.Equal(t, 1, decision.CandidateCount)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_OpenAIProviderSchedulerMetrics(t *testing.T) {
	ctx := context.Background()
	groupID := int64(12)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 4001,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			GroupIDs:    []int64{groupID},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_metrics": provider.Record.ID,
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}},
		Shared: Shared{
			Cache:       cache,
			Concurrency: schedulercore.NewConcurrencyService(schedulerTestConcurrencyCache{}, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(&config.Config{}, "true"),
		},
	}, &config.Config{})

	selection, _, err := svc.SelectProviderWithScheduler(ctx, &groupID, "", "session_hash_metrics", "gpt-5.1", nil, egress.OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	svc.ReportOpenAIProviderScheduleResult(&provider, "", true, intPtrForTest(120))
	svc.RecordOpenAIProviderSwitch()

	snapshot := svc.SnapshotOpenAIProviderSchedulerMetrics()
	require.GreaterOrEqual(t, snapshot.SelectTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.StickySessionHitTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.ProviderSwitchTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.SchedulerLatencyMsAvg, float64(0))
	require.GreaterOrEqual(t, snapshot.StickyHitRatio, 0.0)
	require.GreaterOrEqual(t, snapshot.RuntimeStatsProviderCount, 1)
}

func intPtrForTest(v int) *int {
	return &v
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_LoadBalanceDistributesAcrossSessions(t *testing.T) {
	ctx := context.Background()
	groupID := int64(15)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 5101,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 3,
				Priority:    0,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 5102,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 3,
				Priority:    0,
			},
		},
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 5103,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 3,
				Priority:    0,
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 3
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 1

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*schedulercore.ProviderLoadInfo{
			5101: {ProviderID: 5101, LoadRate: 20, WaitingCount: 1},
			5102: {ProviderID: 5102, LoadRate: 20, WaitingCount: 1},
			5103: {ProviderID: 5103, LoadRate: 20, WaitingCount: 1},
		},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{sessionBindings: map[string]int64{}},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	selected := make(map[int64]int, len(providers))
	for i := 0; i < 60; i++ {
		sessionHash := fmt.Sprintf("session_hash_lb_%d", i)
		selection, decision, err := svc.SelectProviderWithScheduler(
			ctx,
			&groupID,
			"",
			sessionHash,
			"gpt-5.1",
			nil, egress.OpenAIUpstreamTransportAny, false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Provider)
		require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
		selected[selection.Provider.Record.ID]++
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}

	// 多 session 应该能打散到多个提供商，避免“恒定单提供商命中”。
	require.GreaterOrEqual(t, len(selected), 2)
}

func TestClamp01_AllBranches(t *testing.T) {
	require.Equal(t, 0.0, schedulercore.Clamp01(-0.2))
	require.Equal(t, 1.0, schedulercore.Clamp01(1.3))
	require.Equal(t, 0.5, schedulercore.Clamp01(0.5))
}

func TestCalcLoadSkewByMoments_Branches(t *testing.T) {
	require.Equal(t, 0.0, schedulercore.LoadSkewByMoments(1, 1, 1))
	// variance < 0 分支：sumSquares/count - mean^2 为负值时应钳制为 0。
	require.Equal(t, 0.0, schedulercore.LoadSkewByMoments(1, 0, 2))
	require.GreaterOrEqual(t, schedulercore.LoadSkewByMoments(6, 20, 3), 0.0)
}

func TestDefaultOpenAIProviderScheduler_ReportSwitchAndSnapshot(t *testing.T) {
	schedulerAny := newDefaultOpenAIProviderScheduler(newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{}, Shared: Shared{}}, nil),

		nil)
	scheduler, ok := schedulerAny.(*compatiblePicker)
	require.True(t, ok)

	ttft := 100
	scheduler.ReportResult(1001, true, &ttft)
	scheduler.ReportSwitch()
	scheduler.metrics.RecordSelect(schedulercore.PlatformDecision{
		Layer:             openAIProviderScheduleLayerLoadBalance,
		LatencyMs:         8,
		LoadSkew:          0.5,
		StickyPreviousHit: true,
	})
	scheduler.metrics.RecordSelect(schedulercore.PlatformDecision{
		Layer:            "session_hash",
		LatencyMs:        6,
		LoadSkew:         0.2,
		StickySessionHit: true,
	})

	snapshot := scheduler.SnapshotMetrics()
	require.Equal(t, int64(2), snapshot.SelectTotal)
	require.Equal(t, int64(1), snapshot.StickyPreviousHitTotal)
	require.Equal(t, int64(1), snapshot.StickySessionHitTotal)
	require.Equal(t, int64(1), snapshot.LoadBalanceSelectTotal)
	require.Equal(t, int64(1), snapshot.ProviderSwitchTotal)
	require.Greater(t, snapshot.SchedulerLatencyMsAvg, 0.0)
	require.Greater(t, snapshot.StickyHitRatio, 0.0)
	require.Greater(t, snapshot.LoadSkewAvg, 0.0)
}

func TestOpenAIGatewayService_SchedulerWrappersAndDefaults(t *testing.T) {
	svc := newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{}, Shared: Shared{}}, nil)

	ttft := 120
	svc.ReportOpenAIProviderScheduleResult(&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 10}}, "", true, &ttft)
	svc.RecordOpenAIProviderSwitch()
	snapshot := svc.SnapshotOpenAIProviderSchedulerMetrics()
	require.Equal(t, schedulercore.PlatformMetricsSnapshot{}, snapshot)
	require.Equal(t, 7, svc.schedulerParameters.Defaults().TopK)
	require.Equal(t, openaiStickySessionTTL, svc.SessionStickyTTL())

	defaultWeights := svc.schedulerParameters.Defaults().Weights
	require.Equal(t, 1.0, defaultWeights.Priority)
	require.Equal(t, 1.0, defaultWeights.Load)
	require.Equal(t, 0.7, defaultWeights.Queue)
	require.Equal(t, 0.8, defaultWeights.ErrorRate)
	require.Equal(t, 0.5, defaultWeights.TTFT)
	require.Equal(t, 0.0, defaultWeights.Reset)

	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 9
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 180
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 0.2
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 0.3
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 0.4
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0.5
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0.6
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Reset = 0.7
	svcWithCfg := newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{}, Shared: Shared{}}, cfg)

	require.Equal(t, 9, svcWithCfg.schedulerParameters.Defaults().TopK)
	require.Equal(t, 180*time.Second, svcWithCfg.SessionStickyTTL())
	customWeights := svcWithCfg.schedulerParameters.Defaults().Weights
	require.Equal(t, 0.2, customWeights.Priority)
	require.Equal(t, 0.3, customWeights.Load)
	require.Equal(t, 0.4, customWeights.Queue)
	require.Equal(t, 0.5, customWeights.ErrorRate)
	require.Equal(t, 0.6, customWeights.TTFT)
	require.Equal(t, 0.7, customWeights.Reset)
}

func TestDefaultOpenAIProviderScheduler_IsProviderTransportCompatible_Branches(t *testing.T) {
	scheduler := &compatiblePicker{}
	require.True(t, scheduler.isProviderTransportCompatible(nil, egress.OpenAIUpstreamTransportAny))
	require.True(t, scheduler.isProviderTransportCompatible(nil, egress.OpenAIUpstreamTransportHTTPSSE))
	require.False(t, scheduler.isProviderTransportCompatible(nil, egress.OpenAIUpstreamTransportResponsesWebsocketV2))

	cfg := newSchedulerTestOpenAIWSV2Config()
	scheduler.service = newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{}, Shared: Shared{}}, cfg)

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 8801,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	require.True(t, scheduler.isProviderTransportCompatible(provider, egress.OpenAIUpstreamTransportResponsesWebsocketV2))
	require.True(t, scheduler.isProviderTransportCompatible(provider, egress.OpenAIUpstreamTransportResponsesWebsocketV2Ingress))

	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	// Options 按值传入，路由模式测试需要重新构造选择器。
	scheduler.service = newCompatibleSelectionForTest(CompatibleDependencies{}, cfg)
	provider.Record.Extra["openai_apikey_responses_websockets_v2_mode"] = providercore.OpenAIWSIngressModeHTTPBridge
	require.False(t, scheduler.isProviderTransportCompatible(provider, egress.OpenAIUpstreamTransportResponsesWebsocketV2))
	require.True(t, scheduler.isProviderTransportCompatible(provider, egress.OpenAIUpstreamTransportResponsesWebsocketV2Ingress))

	provider.Record.Extra["openai_apikey_responses_websockets_v2_mode"] = providercore.OpenAIWSIngressModeOff
	require.False(t, scheduler.isProviderTransportCompatible(provider, egress.OpenAIUpstreamTransportResponsesWebsocketV2Ingress))
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_StickyWeightedDoesNotFallbackOutsideTopK(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101081)
	ctx = withAdvancedSchedulerTestGroup(ctx, groupID)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38001,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				GroupIDs:    []int64{groupID},
			},
		},
		{
			Record:
			// 粘性提供商仍在分组内，但分数不足以进入 Top-K。
			providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38002,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    100,
				GroupIDs:    []int64{groupID},
			},
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.AdvancedScheduler.LBTopK = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Priority = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Load = 1
	cfg.Gateway.AdvancedScheduler.ScoreWeights.Queue = 0.7
	cfg.Gateway.AdvancedScheduler.ScoreWeights.ErrorRate = 0.8
	cfg.Gateway.AdvancedScheduler.ScoreWeights.TTFT = 0.5
	cfg.Gateway.AdvancedScheduler.ScoreWeights.SessionSticky = 0
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{
		"openai:session_weighted_out_of_group": 38002,
	}}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{38001: false, 38002: true},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerGroupAwareOpenAIProviderRepo{schedulerTestOpenAIProviderRepo{providers: providers}}},
		Shared: Shared{
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true", "true"),
			Cache:       cache,
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_weighted_out_of_group",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	// Top-K 候选 38001 满并发时必须返回其等待计划，不能再硬回退到 Top-K 外的粘性提供商。
	require.Equal(t, int64(38001), selection.Provider.Record.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(38001), selection.WaitPlan.ProviderID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
}

func TestOpenAIGatewayService_SelectProviderWithScheduler_SubscriptionPriorityWaitsOnBusySubscriptionWhenRegularUnusable(t *testing.T) {
	ctx := context.Background()
	groupID := int64(101091)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record:
			// 订阅提供商：支持 compact，但并发已满（busy-but-waitable）。
			providercore.Record{
				LoadLocation: time.LoadLocation, ID: 38011,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    0,
				GroupIDs:    []int64{groupID},
				Credentials: map[string]any{"model_whitelist": []string{"*"}, "plan_type": "team"},
				Extra:       map[string]any{"openai_compact_mode": "force_on"},
			},
		},
		{
			Record:
			// 常规提供商：明确不支持 compact，无法服务本次请求。
			providercore.Record{
				Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 38012,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    9,
				GroupIDs:    []int64{groupID},
				Extra:       map[string]any{"openai_compact_mode": "force_off"},
			},
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{38011: false, 38012: true},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{Providers: schedulerTestOpenAIProviderRepo{providers: providers}},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: schedulercore.NewConcurrencyService(concurrencyCache, schedulercore.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(newSchedulerTestSubscriptionPriorityConfig(), "true", "", "true"),
		},
	}, newSchedulerTestSubscriptionPriorityConfig())

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_subscription_wait",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportAny, true,
	)
	// 常规池无候选时，为忙碌的订阅提供商生成等待计划。
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, int64(38011), selection.Provider.Record.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(38011), selection.WaitPlan.ProviderID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}

func (r schedulerTestOpenAIProviderRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for i := range r.providers {
		prepareSelectionFixtureProvider(ctx, &r.providers[i], &groupID)
		if slices.Contains(platforms, r.providers[i].Record.Platform) && openAIStickyProviderMatchesGroup(&r.providers[i], &groupID) {
			result = append(result, r.providers[i])
		}
	}
	return result, nil
}

func (r schedulerGroupAwareOpenAIProviderRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for i := range r.providers {
		if slices.Contains(platforms, r.providers[i].Record.Platform) && openAIStickyProviderMatchesGroup(&r.providers[i], &groupID) {
			prepareSelectionFixtureProvider(ctx, &r.providers[i], &groupID)
			result = append(result, r.providers[i])
		}
	}
	return result, nil
}

func (r schedulerGroupAwareOpenAIProviderRepo) GetByID(ctx context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	for i := range r.providers {
		if r.providers[i].Record.ID == id {
			copy := r.providers[i]
			groups := copy.Record.GroupIDs
			prepareSelectionFixtureProvider(ctx, &copy, nil)
			copy.Record.GroupIDs = groups
			return &copy, nil
		}
	}
	return nil, errors.New("provider not found")
}
