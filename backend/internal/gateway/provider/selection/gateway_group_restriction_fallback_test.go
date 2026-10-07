package selection

import (
	"context"
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	routingtestkit "github.com/TokenFlux/TokenRouter/internal/routing/testkit"
	"github.com/stretchr/testify/require"
)

func TestSelectProviderForModelWithExclusions_UsesAdmittedFallbackGroupForGroupRestriction(t *testing.T) {
	t.Parallel()

	groupID := int64(10)
	fallbackID := int64(11)
	ch := routingtestkit.Configuration{
		ID:             1,
		Status:         billing.StatusActive,
		GroupIDs:       []int64{fallbackID},
		RestrictModels: true,
		ModelPricing: []routing.ModelPricingEntry{
			{Models: []string{"claude-sonnet-4-6"}},
		},
	}
	pricingConfigSvc := routingtestkit.NewConfigServiceFixture(routingtestkit.StandardPricingConfigRepository(ch, map[int64]string{
		fallbackID: capability.PlatformAnthropic,
	}))
	providerRepo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range providerRepo.providers {
		providerRepo.providersByID[providerRepo.providers[i].Record.ID] = &providerRepo.providers[i]
	}
	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{
			groupID: {
				ID: groupID,

				Status:          billing.StatusActive,
				ClaudeCodeOnly:  true,
				FallbackGroupID: &fallbackID,
				Hydrated:        true,
			},
			fallbackID: {
				ID: fallbackID,

				Status:   billing.StatusActive,
				Hydrated: true,
			},
		},
	}

	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{
			Providers: providerRepo,

			Groups: groupRepo,
		},
		Shared: Shared{GroupPolicies: pricingConfigSvc},
	}, testConfig())

	// 入口已完成回退授权，选择器只使用最终分组及其模型限制。
	ctx := requeststate.WithGroup(context.Background(), groupRepo.groups[fallbackID])
	ctx = requeststate.WithRoutePlan(ctx, routing.Plan(routing.PlanInput{Group: groupRepo.groups[fallbackID]}))
	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &fallbackID, "", "claude-sonnet-4-6", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(1), provider.Record.ID)
}

func TestSelectProviderWithLoadAwareness_UsesAdmittedFallbackGroupForGroupRestriction(t *testing.T) {
	t.Parallel()

	groupID := int64(10)
	fallbackID := int64(11)
	ch := routingtestkit.Configuration{
		ID:             1,
		Status:         billing.StatusActive,
		GroupIDs:       []int64{fallbackID},
		RestrictModels: true,
		ModelPricing: []routing.ModelPricingEntry{
			{Models: []string{"claude-sonnet-4-6"}},
		},
	}
	pricingConfigSvc := routingtestkit.NewConfigServiceFixture(routingtestkit.StandardPricingConfigRepository(ch, map[int64]string{
		fallbackID: capability.PlatformAnthropic,
	}))
	providerRepo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range providerRepo.providers {
		providerRepo.providersByID[providerRepo.providers[i].Record.ID] = &providerRepo.providers[i]
	}
	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{
			groupID: {
				ID: groupID,

				Status:          billing.StatusActive,
				ClaudeCodeOnly:  true,
				FallbackGroupID: &fallbackID,
				Hydrated:        true,
			},
			fallbackID: {
				ID: fallbackID,

				Status:   billing.StatusActive,
				Hydrated: true,
			},
		},
	}

	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{
			Providers: providerRepo,

			Groups: groupRepo,
		},
		Shared: Shared{GroupPolicies: pricingConfigSvc},
	}, testConfig())

	// 入口已完成回退授权，选择器只使用最终分组及其模型限制。
	ctx := requeststate.WithGroup(context.Background(), groupRepo.groups[fallbackID])
	ctx = requeststate.WithRoutePlan(ctx, routing.Plan(routing.PlanInput{Group: groupRepo.groups[fallbackID]}))
	result, err := svc.SelectProviderWithLoadAwareness(ctx, &fallbackID, "", "claude-sonnet-4-6", nil, "", 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Provider)
	require.Equal(t, int64(1), result.Provider.Record.ID)
}
