package selection

import (
	"context"
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	routingtestkit "github.com/TokenFlux/TokenRouter/internal/routing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/stretchr/testify/require"
)

func TestOpenAISelectProviderForModelWithExclusions_GroupMappedRestrictionRejectsEarly(t *testing.T) {
	t.Parallel()

	pricingConfigSvc := routingtestkit.NewConfigServiceFixture(routingtestkit.StandardPricingConfigRepository(routingtestkit.Configuration{
		ID:                 1,
		Status:             billing.StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: routing.BillingModelSourceGroupMapped,
		ModelPricing: []routing.ModelPricingEntry{
			{Models: []string{"gpt-4o"}},
		},
		ModelMapping: map[string]string{"gpt-4.1": "o3-mini"},
	}, map[int64]string{10: capability.PlatformOpenAI}))

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: selectionProviderFixture{providers: []gatewayprovider.ExecutionProvider{{Record: providercore.Record{
				Credentials:  map[string]any{"model_whitelist": []string{"*"}},
				LoadLocation: time.LoadLocation,
				ID:           1, Platform: capability.PlatformOpenAI, Status: billing.StatusActive,
				Schedulable: true,
			}}}},
		},
		Shared: Shared{GroupPolicies: pricingConfigSvc},
	}, nil)

	groupID := int64(10)
	_, err := svc.SelectProviderForModelWithExclusions(context.Background(), &groupID, "", "gpt-4.1", nil)
	require.ErrorIs(t, err, scheduler.ErrNoAvailableProviders)
	require.Contains(t, err.Error(), "group model restriction")
}

func TestOpenAISelectProviderForModelWithExclusions_UpstreamRestrictionSkipsDisallowedProvider(t *testing.T) {
	t.Parallel()

	pricingConfigSvc := routingtestkit.NewConfigServiceFixture(routingtestkit.StandardPricingConfigRepository(routingtestkit.Configuration{
		ID:                 1,
		Status:             billing.StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: routing.BillingModelSourceUpstream,
		ModelPricing: []routing.ModelPricingEntry{
			{Models: []string{"o3-mini"}},
		},
	}, map[int64]string{10: capability.PlatformOpenAI}))

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: selectionProviderFixture{providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           1, Platform: capability.PlatformOpenAI, Status: billing.StatusActive,
					Schedulable: true, Priority: 10, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-4.1": "gpt-4o"}},
				}},
				{Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Status: billing.StatusActive,
					Schedulable: true, Priority: 20,
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-4.1": "o3-mini"}},
				}},
			}},
		},
		Shared: Shared{GroupPolicies: pricingConfigSvc},
	}, nil)

	groupID := int64(10)
	provider, err := svc.SelectProviderForModelWithExclusions(context.Background(), &groupID, "", "gpt-4.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(2), provider.Record.ID)
}

func TestOpenAISelectProviderForModelWithExclusions_StickyRestrictedUpstreamFallsBack(t *testing.T) {
	t.Parallel()

	pricingConfigSvc := routingtestkit.NewConfigServiceFixture(routingtestkit.StandardPricingConfigRepository(routingtestkit.Configuration{
		ID:                 1,
		Status:             billing.StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: routing.BillingModelSourceUpstream,
		ModelPricing: []routing.ModelPricingEntry{
			{Models: []string{"o3-mini"}},
		},
	}, map[int64]string{10: capability.PlatformOpenAI}))

	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{"openai:sticky-session": 1},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: selectionProviderFixture{providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           1, Platform: capability.PlatformOpenAI, Status: billing.StatusActive,
					Schedulable: true, Priority: 10, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-4.1": "gpt-4o"}},
				}},
				{Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Status: billing.StatusActive,
					Schedulable: true, Priority: 20,
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-4.1": "o3-mini"}},
				}},
			}},
		},
		Shared: Shared{
			GroupPolicies: pricingConfigSvc,

			Cache: cache,
		},
	}, nil)

	groupID := int64(10)
	provider, err := svc.SelectProviderForModelWithExclusions(context.Background(), &groupID, "sticky-session", "gpt-4.1", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, int64(2), provider.Record.ID)
	require.Equal(t, 1, cache.deletedSessions["openai:sticky-session"])
	require.Equal(t, int64(2), cache.sessionBindings["openai:sticky-session"])
}
