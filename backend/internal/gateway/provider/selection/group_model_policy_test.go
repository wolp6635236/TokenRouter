package selection

import (
	"context"
	"testing"
	"time"

	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"

	routingtestkit "github.com/TokenFlux/TokenRouter/internal/routing/testkit"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestOpenAIUpstreamRestrictionAppliesPricingConfigThenProviderMapping(t *testing.T) {
	groupID := int64(4202)
	price := 0.01
	pricingConfig := routingtestkit.Configuration{
		ID:                 73,
		Status:             billing.StatusActive,
		RestrictModels:     true,
		BillingModelSource: routing.BillingModelSourceUpstream,
		ModelMapping:       map[string]string{"client-alias": "group-model"},
		ModelPricing: []routing.ModelPricingEntry{{
			Models:     []string{"upstream-model"},
			InputPrice: &price,
		}},
	}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{},
		Shared: Shared{GroupPolicies: routingtestkit.PricingConfig(groupID, capability.PlatformOpenAI,
			pricingConfig)},
	}, nil)

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"group-model": "upstream-model"},
			},
		},
	}

	require.False(t, upstreamRestrictedForTest(svc, context.Background(), groupID, provider, "client-alias", false))
}

// TestOpenAIUpstreamRestrictionUsesActuallyForwardedOAuthModel 检查 OAuth 归一化和自动透传是否按上游模型限制候选。
func TestOpenAIUpstreamRestrictionUsesActuallyForwardedOAuthModel(t *testing.T) {
	price := 0.01
	tests := []struct {
		name               string
		groupID            int64
		pricingConfigModel string
		pricingModel       string
		provider           *gatewayprovider.ExecutionProvider
		restricted         bool
	}{
		{
			name:               "OAuth 后缀型号不能借用基名价格",
			groupID:            4204,
			pricingConfigModel: "gpt-5.6-sol-high",
			pricingModel:       "gpt-5.6-sol",
			restricted:         true,
			provider:           &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}},
		},
		{
			name:               "OAuth 完整后缀型号命中独立价卡",
			groupID:            4205,
			pricingConfigModel: "gpt-5.6-sol-high",
			pricingModel:       "gpt-5.6-sol-high",
			provider:           &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}},
			restricted:         false,
		},
		{
			name:               "裸名称不能隐式使用 Sol 的上游定价",
			groupID:            4207,
			pricingConfigModel: "gpt-5.6",
			pricingModel:       "gpt-5.6-sol",
			provider:           &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}},
			restricted:         true,
		},
		{
			name:               "自动透传仍按提供商映射后的模型检查",
			groupID:            4206,
			pricingConfigModel: "passthrough-model",
			pricingModel:       "mapped-model",
			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI,
					Type:  capability.ProviderTypeOAuth,
					Extra: map[string]any{"openai_passthrough": true},
					Credentials: map[string]any{
						"model_mapping": map[string]any{"passthrough-model": "mapped-model"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pricingConfig := routingtestkit.Configuration{
				ID:                 tt.groupID,
				Status:             billing.StatusActive,
				RestrictModels:     true,
				BillingModelSource: routing.BillingModelSourceUpstream,
				ModelMapping:       map[string]string{"client-alias": tt.pricingConfigModel},
				ModelPricing: []routing.ModelPricingEntry{{
					Models:     []string{tt.pricingModel},
					InputPrice: &price,
				}},
			}
			svc := newCompatibleSelectionForTest(CompatibleDependencies{
				Reads:  Reads{},
				Shared: Shared{GroupPolicies: routingtestkit.PricingConfig(tt.groupID, capability.PlatformOpenAI, pricingConfig)},
			}, nil)

			ctx := context.Background()
			restricted := upstreamRestrictedForTest(svc, ctx, tt.groupID, tt.provider, "client-alias", false)
			require.Equal(t, tt.restricted, restricted)
		})
	}
}

func TestModelAvailabilityDiagnosisAcceptsPricingConfigAlias(t *testing.T) {
	groupID := int64(4203)
	pricingConfig := routingtestkit.Configuration{
		ID:           74,
		Status:       billing.StatusActive,
		ModelMapping: map[string]string{"client-alias": "group-model"},
	}
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 75,
			Platform:    capability.PlatformOpenAI,
			Status:      billing.StatusActive,
			Schedulable: true,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"group-model": "upstream-model"},
				"model_whitelist": []any{"upstream-model"},
			},
		},
	}
	repo := schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads:  Reads{Providers: repo},
		Shared: Shared{GroupPolicies: routingtestkit.PricingConfig(groupID, capability.PlatformOpenAI, pricingConfig)},
	}, nil)

	diagnosis := gatewayprovider.NewModelAvailability(gatewaytestkit.AvailabilityStore{Source: repo}, svc.groupPolicies, true).DiagnoseCompatible(context.Background(), &groupID, "client-alias", capability.PlatformOpenAI)
	require.True(t, diagnosis.HasProvidersInPool)
	require.True(t, diagnosis.HasModelSupport)
}

// TestResolveOpenAIWSRoutingModelForProviderStrictlyFollowsBillingBasis 验证长连接每轮都严格按所选依据检查 R、C 或 U。

// TestResolveOpenAIWSRoutingModelForProviderRejectsUnsupportedMappedModel 验证后续 turn 不能绕过固定提供商的最终白名单。

// upstreamRestrictedForTest 先做分组映射，再检查提供商层的模型限制。
func upstreamRestrictedForTest(s *Compatible, ctx context.Context, group int64, value *gatewayprovider.ExecutionProvider, model string, compact bool) bool {
	return s.UpstreamRoutingModelRestricted(ctx, group, value, s.resolveGroupRoutingModel(ctx, &group, model), compact)
}
