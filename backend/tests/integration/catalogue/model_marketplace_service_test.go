package catalogue_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	routingtestkit "github.com/TokenFlux/TokenRouter/internal/routing/testkit"
)

type marketplaceGroupRepoStub struct {
	routing.GroupRepository

	groups []routing.Group
}

func TestModelMarketplaceQoderProviderMappedCustomModelUsesRouteKeyManualPricing(t *testing.T) {
	groupID := int64(903)
	inputPrice := 0.01
	outputPrice := 0.02
	pricingConfigService := routingtestkit.PricingConfig(groupID, capability.PlatformQoder, routingtestkit.Configuration{ID: groupID, Status: billing.StatusActive, BillingModelSource: routing.BillingModelSourceUpstream, ModelPricing: []routing.ModelPricingEntry{{Models: []string{"qmodel"}, BillingMode: routing.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}}})

	billingService := billingtestkit.Calculator(nil, nil)
	svc := newCatalogueMarketplace(nil, newCatalogueFixture(&modelsListProviderRepoStub{byGroup: map[int64][]providercore.Record{
		groupID: {
			{
				ID:       1,
				Platform: capability.PlatformQoder,
				Type:     capability.ProviderTypeCosy,
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"custom-qoder-model": "qmodel",
					},
				},
			},
		},
	}}, pricingConfigService, cataloguePriceResolver(pricingConfigService, billingService)), billingService)
	group := &routing.Group{ID: groupID, RateMultiplier: 1}

	models := svc.ModelsForGroup(context.Background(), group)

	var customModel *routing.ModelMarketplaceModel
	for i := range models {
		if models[i].ID == "custom-qoder-model" {
			customModel = &models[i]
			break
		}
	}
	if customModel == nil {
		t.Fatalf("Qoder provider-mapped marketplace models = %#v, want custom-qoder-model", models)
	}
	pricing := customModel.Pricing
	if pricing.InputPricePerToken != inputPrice || pricing.OutputPricePerToken != outputPrice {
		t.Fatalf("Qoder provider-mapped route key display price = (%g, %g), want (%g, %g)", pricing.InputPricePerToken, pricing.OutputPricePerToken, inputPrice, outputPrice)
	}
}

func TestModelMarketplaceListPublicPrefetchesProvidersOnce(t *testing.T) {
	groups := []routing.Group{
		{ID: 4101, Name: "OpenAI A", Status: billing.StatusActive, RateMultiplier: 1, ActiveProviderCount: 1},
		{ID: 4102, Name: "OpenAI B", Status: billing.StatusActive, RateMultiplier: 1, ActiveProviderCount: 1},
	}
	providers := []providercore.Record{
		{
			ID:       5101,
			Platform: capability.PlatformOpenAI,
			GroupIDs: []int64{4101},
			ProviderGroups: []providercore.GroupMembership{{
				ProviderID: 5101,
				GroupID:    4101,
			}},
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"client-a": "upstream-a"},
				"model_whitelist": []any{"upstream-a"},
			},
		},
		{
			ID:       5102,
			Platform: capability.PlatformOpenAI,
			GroupIDs: []int64{4102},
			ProviderGroups: []providercore.GroupMembership{{
				ProviderID: 5102,
				GroupID:    4102,
			}},
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"client-b": "upstream-b"},
				"model_whitelist": []any{"upstream-b"},
			},
		},
	}
	providerRepo := &modelsListProviderRepoStub{
		all: providers,
		byGroup: map[int64][]providercore.Record{
			4101: {providers[0]},
			4102: {providers[1]},
		},
	}
	gatewayService := newCatalogueFixture(providerRepo, nil, nil)
	service := newCatalogueMarketplace(&marketplaceGroupRepoStub{groups: groups}, gatewayService, billingtestkit.Calculator(nil, nil))

	result, err := service.ListPublic(context.Background(), routing.MarketplaceListOptions{IncludeCapacity: true})
	if err != nil {
		t.Fatalf("ListPublic returned error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListPublic returned %d groups, want 2", len(result))
	}
	if providerRepo.listAllCalls.Load() != 1 || providerRepo.listByGroupCalls.Load() != 0 {
		t.Fatalf("provider queries = all:%d by_group:%d, want all:1 by_group:0", providerRepo.listAllCalls.Load(), providerRepo.listByGroupCalls.Load())
	}
	groupAModels := make(map[string]struct{}, len(result[0].Models))
	for _, model := range result[0].Models {
		groupAModels[model.ID] = struct{}{}
	}
	groupBModels := make(map[string]struct{}, len(result[1].Models))
	for _, model := range result[1].Models {
		groupBModels[model.ID] = struct{}{}
	}
	if _, ok := groupAModels["client-a"]; !ok {
		t.Fatalf("group A models = %#v, want client-a", result[0].Models)
	}
	if _, leaked := groupAModels["client-b"]; leaked {
		t.Fatalf("group A models = %#v, must not contain client-b", result[0].Models)
	}
	if _, ok := groupBModels["client-b"]; !ok {
		t.Fatalf("group B models = %#v, want client-b", result[1].Models)
	}
	if _, leaked := groupBModels["client-a"]; leaked {
		t.Fatalf("group B models = %#v, must not contain client-a", result[1].Models)
	}
}

func TestModelMarketplacePrefetchSortsByGlobalProviderPriority(t *testing.T) {
	providerRepo := &modelsListProviderRepoStub{all: []providercore.Record{
		{ID: 5103, Priority: 10, GroupIDs: []int64{4101}},
		{ID: 5102, Priority: 5, GroupIDs: []int64{4101}},
		{ID: 5101, Priority: 5, GroupIDs: []int64{4101}},
	}}
	svc := newCatalogueMarketplace(nil, newCatalogueFixture(providerRepo, nil, nil), nil)

	providersByGroup, ok := svc.PrefetchProviders(context.Background())

	if !ok {
		t.Fatal("prefetchPublicGroupProviders should succeed")
	}
	providers := providersByGroup[4101]
	if len(providers) != 3 {
		t.Fatalf("prefetched providers = %d, want 3", len(providers))
	}
	if providers[0].ID != 5101 || providers[1].ID != 5102 || providers[2].ID != 5103 {
		t.Fatalf("prefetched provider order = [%d %d %d], want [5101 5102 5103]", providers[0].ID, providers[1].ID, providers[2].ID)
	}
}

func (s *marketplaceGroupRepoStub) ListActive(context.Context) ([]routing.Group, error) {
	return s.groups, nil
}
