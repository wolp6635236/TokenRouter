package provider_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/protocol"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestDiagnoseModelAvailabilityForPlatform_NoModel_AlwaysAvailable(t *testing.T) {
	repo := &availabilityProviderStore{providers: nil, providersByID: map[int64]*gatewayprovider.ExecutionProvider{}}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "", capability.PlatformOpenAI)

	require.True(t, diag.HasProvidersInPool, "空模型必须保守返回 HasProvidersInPool=true，让调用方继续走 503")
	require.True(t, diag.HasModelSupport, "空模型必须保守返回 HasModelSupport=true，让调用方继续走 503")
}

func TestDiagnoseModelAvailabilityForPlatform_EmptyPlatformKeepsEmptyGroupEmpty(t *testing.T) {
	repo := &availabilityProviderStore{providers: nil, providersByID: map[int64]*gatewayprovider.ExecutionProvider{}}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5", "")

	require.False(t, diag.HasProvidersInPool)
	require.False(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_NilReceiver(t *testing.T) {
	var svc *routing.ModelAvailability

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5", capability.PlatformOpenAI)

	require.True(t, diag.HasProvidersInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_NoProvidersInPool(t *testing.T) {
	repo := &availabilityProviderStore{providers: nil, providersByID: map[int64]*gatewayprovider.ExecutionProvider{}}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5", capability.PlatformOpenAI)

	require.False(t, diag.HasProvidersInPool)
	require.False(t, diag.HasModelSupport, "没有提供商表示没有模型支持；调用方会走空池 503 分支")
}

func TestDiagnoseModelAvailabilityForPlatform_ExplicitMappingMatches(t *testing.T) {
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"gpt-5.1-codex-mini": "gpt-5.1-codex-mini"},
					},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5.1-codex-mini", capability.PlatformOpenAI)

	require.True(t, diag.HasProvidersInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityUsesDefaultCatalogForEmptyScope(t *testing.T) {
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Status: billing.StatusActive, Schedulable: true} /* 未配置范围时使用提供商默认目录 */},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5.1-codex-mini", capability.PlatformOpenAI)

	require.False(t, diag.HasModelSupport, "默认目录外的模型不能隐式放行")
	diag = svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5.6-sol", capability.PlatformOpenAI)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_WildcardMappingMatches(t *testing.T) {
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"*": "gpt-5"},
					},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5.1-codex-mini", capability.PlatformOpenAI)

	require.True(t, diag.HasModelSupport, "通配符映射必须把请求模型视为可服务")
}

func TestDiagnoseModelAvailabilityForPlatform_NoMatchingModel_ReturnsNotFoundSignal(t *testing.T) {
	groupID := int64(42)
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					ProviderGroups: []providercore.GroupMembership{
						{GroupID: groupID},
					},
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
				},
			},
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 2,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					ProviderGroups: []providercore.GroupMembership{
						{GroupID: groupID},
					},
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5-mini": "gpt-5-mini"}},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), &groupID, "gpt-5.1-codex-mini", capability.PlatformOpenAI)

	require.True(t, diag.HasProvidersInPool, "分组内存在 OpenAI 提供商")
	require.False(t, diag.HasModelSupport, "没有提供商映射允许该模型时 handler 应返回 404")
}

func TestDiagnoseModelAvailabilityForPlatform_RateLimitedSupportingProviderRemainsConfigured(t *testing.T) {
	groupID := int64(42)
	cooldownUntil := time.Now().Add(time.Hour)
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:               capability.PlatformAnthropic,
					Status:                 billing.StatusActive,
					Schedulable:            true,
					RateLimitResetAt:       &cooldownUntil,
					OverloadUntil:          &cooldownUntil,
					TempUnschedulableUntil: &cooldownUntil,
					ProviderGroups:         []providercore.GroupMembership{{GroupID: groupID}},
					Credentials: map[string]any{
						"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
					},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	require.False(t, repo.providers[0].View().IsSchedulable(), "test provider must be excluded from normal scheduling while cooling down")
	svc := newAvailabilityForTest(repo, nil, false)

	// 诊断必须绕过只反映瞬时状态的快照。

	diag := svc.DiagnoseGeneral(context.Background(), &groupID, "claude-opus-4-8", capability.PlatformAnthropic)

	require.True(t, diag.HasProvidersInPool)
	require.True(t, diag.HasModelSupport, "a configured model remains supported while every matching provider is temporarily cooling down")
}

func TestOpenAIDiagnoseModelAvailabilityForPlatform_RateLimitedSupportingProviderRemainsConfigured(t *testing.T) {
	groupID := int64(43)
	cooldownUntil := time.Now().Add(time.Hour)
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 2,
					Platform:               capability.PlatformOpenAI,
					Status:                 billing.StatusActive,
					Schedulable:            true,
					RateLimitResetAt:       &cooldownUntil,
					OverloadUntil:          &cooldownUntil,
					TempUnschedulableUntil: &cooldownUntil,
					ProviderGroups:         []providercore.GroupMembership{{GroupID: groupID}},
					Credentials: map[string]any{
						"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
					},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	require.False(t, repo.providers[0].View().IsSchedulable(), "test provider must be excluded from normal scheduling while cooling down")
	svc := newAvailabilityForTest(repo, nil, true)

	// 诊断必须绕过只反映瞬时状态的快照。

	diag := svc.DiagnoseCompatible(context.Background(), &groupID, "claude-opus-4-8", capability.PlatformOpenAI)

	require.True(t, diag.HasProvidersInPool)
	require.True(t, diag.HasModelSupport, "OpenAI-compatible diagnosis must keep transiently limited supporting providers in the configured pool")
}

func TestDiagnoseModelAvailabilityForPlatform_WrongPlatformFiltersOut(t *testing.T) {
	// 分组里只有 Anthropic 提供商，但用户路由到 OpenAI 网关。
	// 诊断必须按平台过滤掉 Anthropic 提供商，因此 HasProvidersInPool=false，调用方保留 503。
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformAnthropic,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"}},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}
	svc := newAvailabilityForTest(repo, nil, false)

	diag := svc.DiagnoseGeneral(context.Background(), availabilityFixtureGroupID(), "gpt-5", capability.PlatformOpenAI)

	require.False(t, diag.HasProvidersInPool, "OpenAI 路由不能把 Anthropic 提供商算进提供商池")
	require.False(t, diag.HasModelSupport)
}

func TestOpenAIGatewayDiagnoseModelAvailabilityForPlatform_GrokPlatformFiltersOpenAIProviders(t *testing.T) {
	repo := &availabilityProviderStore{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}
	svc := newAvailabilityForTest(repo, nil, true)

	diag := svc.DiagnoseCompatible(context.Background(), availabilityFixtureGroupID(), "grok-4.3", capability.PlatformGrok)

	require.False(t, diag.HasProvidersInPool, "Grok 诊断不能把 OpenAI 提供商算进提供商池")
	require.False(t, diag.HasModelSupport)
}

// availabilityProviderStore 按测试提供商的持久配置筛选候选。
type availabilityProviderStore struct {
	providers      []gatewayprovider.ExecutionProvider
	providersByID  map[int64]*gatewayprovider.ExecutionProvider
	calls          int
	includeGrouped bool
	platforms      []string
}

func (m *availabilityProviderStore) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]providercore.Record, error) {
	m.calls++
	m.includeGrouped = includeGrouped
	m.platforms = append([]string(nil), platforms...)
	platformSet := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		platformSet[platform] = struct{}{}
	}
	result := make([]providercore.Record, 0, len(m.providers))
	for _, value := range m.providers {
		if _, ok := platformSet[value.Record.Platform]; !ok || value.Record.Status != billing.StatusActive || !value.Record.Schedulable {
			continue
		}
		if groupID != nil {
			inGroup := slices.Contains(value.Record.GroupIDs, *groupID)
			for _, group := range value.Record.ProviderGroups {
				if group.GroupID == *groupID {
					inGroup = true
					break
				}
			}
			if !inGroup {
				continue
			}
		} else if !includeGrouped && (len(value.Record.ProviderGroups) > 0 || len(value.Record.GroupIDs) > 0) {
			continue
		}
		result = append(result, *gatewayprovider.ExecutionRecord(&value))
	}
	return result, nil
}

func availabilityFixtureGroupID() *int64 { id := int64(71); return &id }

// newAvailabilityForTest 为模型诊断用例配置测试分组和模型范围。
func newAvailabilityForTest(repo *availabilityProviderStore, policies *routing.PricingConfigService, compatible bool) *routing.ModelAvailability {
	for i := range repo.providers {
		value := &repo.providers[i].Record
		if len(value.GroupIDs) == 0 && len(value.ProviderGroups) == 0 {
			value.GroupIDs = []int64{*availabilityFixtureGroupID()}
		}
		if value.Type == "" {
			value.Type = capability.ProviderTypeAPIKey
		}
	}
	return gatewayprovider.NewModelAvailability(repo, policies, compatible)
}

// TestModelAvailabilityUsesExplicitGroupAcrossPlatforms 验证模型可用性只诊断明确分组的提供商，空平台聚合各提供商平台。
func TestModelAvailabilityUsesExplicitGroupAcrossPlatforms(t *testing.T) {
	groupID := int64(71)
	otherGroup := int64(72)

	for _, compatible := range []bool{false, true} {
		repo := &availabilityProviderStore{providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{ID: 1, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, GroupIDs: []int64{groupID}, Credentials: map[string]any{"model_whitelist": []string{"claude-test"}}}},
			{Record: providercore.Record{ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, GroupIDs: []int64{groupID}, Credentials: map[string]any{"model_whitelist": []string{"gpt-test"}}}},
			{Record: providercore.Record{ID: 3, Platform: capability.PlatformGemini, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, GroupIDs: []int64{otherGroup}, Credentials: map[string]any{"model_whitelist": []string{"gemini-test"}}}},
		}}
		service := gatewayprovider.NewModelAvailability(repo, nil, compatible)
		for _, model := range []string{"claude-test", "gpt-test"} {
			result := service.DiagnoseGeneral(context.Background(), &groupID, model, "")
			require.True(t, result.HasModelSupport)
			require.ElementsMatch(t, capability.ProviderPlatforms(), repo.platforms)
			require.False(t, repo.includeGrouped)
		}
		require.False(t, service.DiagnoseGeneral(context.Background(), &groupID, "gemini-test", "").HasModelSupport)
		forced := service.DiagnoseGeneral(context.Background(), &groupID, "claude-test", capability.PlatformOpenAI)
		require.True(t, forced.HasProvidersInPool)
		require.False(t, forced.HasModelSupport)
		calls := repo.calls
		require.Equal(t, routing.ModelAvailabilityDiagnosis{}, service.DiagnoseGeneral(context.Background(), nil, "gpt-test", ""))
		require.Equal(t, routing.ModelAvailabilityDiagnosis{}, service.DiagnoseCompatible(context.Background(), nil, "gpt-test", ""))
		require.Equal(t, routing.ModelAvailabilityDiagnosis{}, service.DiagnoseCompatibleRouting(context.Background(), nil, "gpt-test", ""))
		require.Equal(t, calls, repo.calls, "缺少分组时不应读取候选池")
		ctx := apikey.WithForcePlatform(context.Background(), capability.PlatformGemini)
		require.False(t, service.DiagnoseCompatibleRouting(ctx, &groupID, "gpt-test", "").HasProvidersInPool)
		require.Equal(t, []string{capability.PlatformGemini}, repo.platforms)
	}
}

// TestModelAvailabilityChecksProtocolWithoutTreatingCooldownAsMissingModel 验证模型诊断复用协议资格，同时忽略提供商的临时冷却状态。
func TestModelAvailabilityChecksProtocolWithoutTreatingCooldownAsMissingModel(t *testing.T) {
	group := &routing.Group{ID: 71, Hydrated: true, Status: routing.StatusActive, ProtocolFallbacks: map[protocol.ProtocolID][]protocol.ProtocolID{protocol.ProtocolAnthropicMessages: {}}}
	until := time.Now().Add(time.Hour)
	repo := &availabilityProviderStore{providers: []gatewayprovider.ExecutionProvider{{Record: providercore.Record{ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive, Schedulable: true, GroupIDs: []int64{group.ID}, RateLimitResetAt: &until, Credentials: map[string]any{"model_whitelist": []string{"gpt-test"}}}}}}
	service := gatewayprovider.NewModelAvailability(repo, nil, true)
	ctx := requeststate.WithClientProtocol(requeststate.WithGroup(context.Background(), group), protocol.ProtocolAnthropicMessages)
	denied := service.DiagnoseCompatible(ctx, &group.ID, "gpt-test", "")
	require.True(t, denied.HasProvidersInPool)
	require.False(t, denied.HasModelSupport)
	group.ProtocolFallbacks = nil
	ctx = requeststate.WithClientProtocol(requeststate.WithGroup(context.Background(), group), protocol.ProtocolAnthropicMessages)
	require.True(t, service.DiagnoseCompatible(ctx, &group.ID, "gpt-test", "").HasModelSupport)
}
