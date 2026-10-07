package selection

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	routing "github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	schedulerredis "github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"

	"github.com/TokenFlux/TokenRouter/internal/config"
)

// snapshotHydrationCache 为 SnapshotService 提供轻量和完整提供商快照。
type snapshotHydrationCache struct {
	scheduler.SnapshotCache
	snapshot  []*gatewayprovider.ExecutionProvider
	providers map[int64]*gatewayprovider.ExecutionProvider
}

func (c *snapshotHydrationCache) GetSnapshot(ctx context.Context, bucket scheduler.SchedulerBucket) ([]scheduler.SnapshotProvider, bool, error) {
	out := make([]scheduler.SnapshotProvider, 0, len(c.snapshot))
	for _, v := range c.snapshot {
		prepareSelectionFixtureProvider(ctx, v, &bucket.GroupID)
		out = append(out, codec.WrapRecord(gatewayprovider.ExecutionRecord(v)))
	}
	return out, true, nil
}

func (c *snapshotHydrationCache) GetProvider(ctx context.Context, id int64) (scheduler.SnapshotProvider, error) {
	prepareSelectionFixtureProvider(ctx, c.providers[id], nil)
	return codec.WrapRecord(gatewayprovider.ExecutionRecord(c.providers[id])), nil
}

func TestOpenAISelectProviderWithLoadAwareness_HydratesSelectedProviderFromSchedulerSnapshot(t *testing.T) {
	cache := &snapshotHydrationCache{
		snapshot: []*gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformOpenAI,
					Type:        capability.ProviderTypeAPIKey,
					Status:      billing.StatusActive,
					Schedulable: true,
					Concurrency: 1,
					Priority:    1,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"gpt-4": "gpt-4",
						},
					},
				},
			},
		},
		providers: map[int64]*gatewayprovider.ExecutionProvider{
			1: {
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformOpenAI,
					Type:        capability.ProviderTypeAPIKey,
					Status:      billing.StatusActive,
					Schedulable: true,
					Concurrency: 1,
					Priority:    1,
					Credentials: map[string]any{
						"api_key":       "sk-live",
						"model_mapping": map[string]any{"gpt-4": "gpt-4"},
					},
				},
			},
		},
	}

	schedulerSnapshot := newHydrationSnapshotForTest(cache, nil)
	groupID := int64(2)
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Snapshot: schedulerredis.NewSnapshotReader(schedulerSnapshot),
		},
		Shared: Shared{Cache: &responseCacheFixture{}},
	}, nil)

	selection, err := svc.SelectProviderWithLoadAwareness(context.Background(), &groupID, "", "gpt-4", nil)
	if err != nil {
		t.Fatalf("SelectProviderWithLoadAwareness error: %v", err)
	}
	if selection == nil || selection.Provider == nil {
		t.Fatalf("expected selected provider")
	}
	if got := selection.Provider.View().GetOpenAIApiKey(); got != "sk-live" {
		t.Fatalf("expected hydrated api key, got %q", got)
	}
}

func TestOpenAINewAcquiredSelectionResult_ReleasesSlotWhenHydrationFails(t *testing.T) {
	cache := &snapshotHydrationCache{
		providers: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	schedulerSnapshot := newHydrationSnapshotForTest(cache, selectionProviderFixture{})
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Snapshot: schedulerredis.NewSnapshotReader(schedulerSnapshot),
		},
		Shared: Shared{},
	}, nil)

	releaseCalls := 0

	selection, err := svc.newAcquiredSelectionResult(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1001}}, func() {
		releaseCalls++
	})

	if err == nil {
		t.Fatalf("expected hydration error")
	}
	if selection != nil {
		t.Fatalf("expected nil selection on hydration error")
	}
	if releaseCalls != 1 {
		t.Fatalf("expected release to be called once, got %d", releaseCalls)
	}
}

func TestGatewaySelectProviderWithLoadAwareness_HydratesSelectedProviderFromSchedulerSnapshot(t *testing.T) {
	cache := &snapshotHydrationCache{
		snapshot: []*gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 9,
					Platform:    capability.PlatformAnthropic,
					Type:        capability.ProviderTypeAPIKey,
					Status:      billing.StatusActive,
					Schedulable: true,
					Concurrency: 1,
					Priority:    1,
				},
			},
		},
		providers: map[int64]*gatewayprovider.ExecutionProvider{
			9: {
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 9,
					Platform:    capability.PlatformAnthropic,
					Type:        capability.ProviderTypeAPIKey,
					Status:      billing.StatusActive,
					Schedulable: true,
					Concurrency: 1,
					Priority:    1,
					Credentials: map[string]any{
						"model_whitelist": []string{"*"},
						"api_key":         "anthropic-live-key",
					},
				},
			},
		},
	}

	schedulerSnapshot := newHydrationSnapshotForTest(cache, nil)
	svc := newGenericSelectionForTest(GenericDependencies{
		Reads:  Reads{Snapshot: schedulerredis.NewSnapshotReader(schedulerSnapshot)},
		Shared: Shared{Cache: &mockGatewayCacheForPlatform{}},
	}, testConfig())

	result, err := svc.SelectProviderWithLoadAwareness(context.Background(), selectionFixtureGroupID(context.Background()), "", "claude-3-5-sonnet-20241022", nil, "", 0)
	if err != nil {
		t.Fatalf("SelectProviderWithLoadAwareness error: %v", err)
	}
	if result == nil || result.Provider == nil {
		t.Fatalf("expected selected provider")
	}
	if got := result.Provider.View().GetCredential("api_key"); got != "anthropic-live-key" {
		t.Fatalf("expected hydrated api key, got %q", got)
	}
}

func TestGatewaySelectProviderWithLoadAwareness_SkipsAntigravityGeminiFamilyRateLimitedSnapshot(t *testing.T) {
	resetAt := time.Now().Add(10 * time.Minute).Format(time.RFC3339)
	cache := &snapshotHydrationCache{
		snapshot: []*gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformAntigravity,
					Type:        capability.ProviderTypeOAuth,
					Status:      billing.StatusActive,
					Schedulable: true,
					Concurrency: 1,
					Priority:    1,
					ProviderGroups: []providercore.GroupMembership{
						{ProviderID: 1, GroupID: 22},
					},
					GroupIDs: []int64{22},
					Extra: map[string]any{
						"mixed_scheduling": true, "model_rate_limits": map[string]any{
							"antigravity:gemini": map[string]any{
								"rate_limit_reset_at": resetAt,
							},
						},
					},
				},
			},
			{
				Record: providercore.Record{
					Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2,
					Platform:    capability.PlatformAntigravity,
					Type:        capability.ProviderTypeOAuth,
					Status:      billing.StatusActive,
					Schedulable: true,
					Concurrency: 1,
					Priority:    2,
					ProviderGroups: []providercore.GroupMembership{
						{ProviderID: 2, GroupID: 22},
					},
					GroupIDs: []int64{22},
					Extra: map[string]any{
						"mixed_scheduling": true,
					},
				},
			},
		},
		providers: map[int64]*gatewayprovider.ExecutionProvider{
			1: {Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAntigravity, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true}},
			2: {Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAntigravity, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true}},
		},
	}
	groupID := int64(22)
	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{
			Groups: &mockGroupRepoForGateway{groups: map[int64]*routing.Group{groupID: {
				ID:     groupID,
				Status: billing.StatusActive, Hydrated: true,
			}}},
			Snapshot: schedulerredis.NewSnapshotReader(newHydrationSnapshotForTest(cache, nil)),
		},
		Shared: Shared{Concurrency: scheduler.NewConcurrencyService(&mockConcurrencyCache{}, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event})},
	}, &config.Config{Gateway: config.GatewayConfig{Scheduling: config.GatewaySchedulingConfig{
		LoadBatchEnabled: true, StickySessionMaxWaiting: 3, StickySessionWaitTimeout: time.Second,
		FallbackWaitTimeout: time.Second, FallbackMaxWaiting: 10,
	}}})

	result, err := svc.SelectProviderWithLoadAwareness(context.Background(), &groupID, "", "gemini-3-flash-preview", nil, "", 0)
	if err != nil {
		t.Fatalf("SelectProviderWithLoadAwareness error: %v", err)
	}
	if result == nil || result.Provider == nil {
		t.Fatalf("expected selected provider")
	}
	if result.Provider.Record.ID != 2 {
		t.Fatalf("expected scheduler to skip Gemini-family limited antigravity provider 1, got %d", result.Provider.Record.ID)
	}
}

// TestGatewayNewSelectionResultReleasesSlotWhenHydrationFails 验证已取得提供商槽后读取完整提供商失败，错误返回前必须归还一次。
func TestGatewayNewSelectionResultReleasesSlotWhenHydrationFails(t *testing.T) {
	cache := &snapshotHydrationCache{providers: map[int64]*gatewayprovider.ExecutionProvider{}}
	snapshot := newHydrationSnapshotForTest(cache, selectionProviderFixture{})
	gateway := newGenericSelectionForTest(GenericDependencies{
		Reads:  Reads{Snapshot: schedulerredis.NewSnapshotReader(snapshot)},
		Shared: Shared{},
	}, nil)

	calls := 0
	result, err := gateway.newSelectionResult(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1001}}, true, func() { calls++ }, nil)
	if err == nil || result != nil {
		t.Fatal("补全失败必须返回原错误而非选择结果")
	}
	if calls != 1 {
		t.Fatalf("释放次数=%d，期望 1", calls)
	}
}

func newHydrationSnapshotForTest(cache *snapshotHydrationCache, source Providers) *scheduler.SnapshotService {
	var read scheduler.SnapshotProviderSource
	if source != nil {
		read = hydrationProviderSource{source: source}
	}
	return scheduler.NewSnapshotService(cache, nil, read, nil, nil, scheduler.SnapshotBindings{ProviderNotFound: providercore.ErrProviderNotFound, GroupNotFound: routing.ErrGroupNotFound, Diagnostics: scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}})
}
