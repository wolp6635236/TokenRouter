package selection

import (
	"context"
	"errors"
	"testing"
	"time"

	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"

	routingtestkit "github.com/TokenFlux/TokenRouter/internal/routing/testkit"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGatewayProviderLayerUsesGroupMappedModelForSupportAndRateLimit(t *testing.T) {
	groupID := int64(4201)
	pricingConfig := routingtestkit.Configuration{
		ID:           71,
		Status:       billing.StatusActive,
		ModelMapping: map[string]string{"client-alias": "group-model"},
	}
	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{},
		Shared: Shared{GroupPolicies: routingtestkit.PricingConfig(groupID, capability.PlatformAnthropic,

			pricingConfig)},
	}, nil)

	ctx := svc.withGroupContext(context.Background(), &routing.Group{
		ID: groupID,

		Status:   billing.StatusActive,
		Hydrated: true,
	})
	future := time.Now().Add(10 * time.Minute).Format(time.RFC3339)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic,
			Status:      billing.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"group-model": "upstream-model"},
				"model_whitelist": []any{"upstream-model"},
			},
			Extra: map[string]any{
				"model_rate_limits": map[string]any{
					"upstream-model": map[string]any{"rate_limit_reset_at": future},
				},
			},
		},
	}

	require.True(t, svc.isModelSupportedByProviderWithContext(ctx, provider, "client-alias"))
	require.False(t, svc.isProviderSchedulableForModelSelection(ctx, provider, "client-alias"))
	require.True(t, svc.shouldClearStickySessionForProviderLayer(ctx, provider, "client-alias"))
}

func TestGatewayAnthropicProviderSupportMapsBeforePlatformNormalization(t *testing.T) {
	tests := []struct {
		name           string
		providerType   string
		finalModel     string
		whitelistModel string
	}{
		{
			name:           "OAuth",
			providerType:   capability.ProviderTypeOAuth,
			finalModel:     "claude-sonnet-4-5-20250929",
			whitelistModel: "claude-sonnet-4-5-20250929",
		},
		{
			name:           "ServiceAccount",
			providerType:   capability.ProviderTypeServiceAccount,
			finalModel:     "claude-sonnet-4-5@20250929",
			whitelistModel: "claude-sonnet-4-5@20250929",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic,
					Type: tt.providerType,
					Credentials: map[string]any{
						"model_mapping":   map[string]any{"group-model": "claude-sonnet-4-5-20250929"},
						"model_whitelist": []any{tt.whitelistModel},
					},
				},
			}

			require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "group-model"))
			require.Equal(t, tt.finalModel, resolveProviderUpstreamModel(context.Background(), provider, "group-model"))
		})
	}
}

func TestAdvancedSchedulerUsesRoutingModelAndKeepsRequestedModel(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 72, Status: billing.StatusActive, Schedulable: true,
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"group-model": "upstream-model"},
				"model_whitelist": []any{"upstream-model"},
			},
		},
	}
	scheduler := &compatiblePicker{
		service: newCompatibleSelectionForTest(CompatibleDependencies{
			Reads: Reads{},

			Shared: Shared{},
		}, nil),
	}
	req := schedulercore.PlatformSelectionInput{
		Platform:       capability.PlatformOpenAI,
		RequestedModel: "client-alias",
		RoutingModel:   "group-model",
	}

	require.Equal(t, "client-alias", req.RequestedModel)
	require.Equal(t, "group-model", requestRoutingModel(req))
	require.True(t, scheduler.isProviderRequestCompatible(context.Background(), provider, req))
}

// TestOpenAIHTTPPassthroughKeepsExplicitModelScope 检查透传提供商的最终模型白名单。
func TestOpenAIHTTPPassthroughKeepsExplicitModelScope(t *testing.T) {
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 76,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Extra:       map[string]any{"openai_passthrough": true},
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"client-model": "mapped-model"},
				"model_whitelist": []any{"other-model"},
			},
		},
	}
	plainCtx := context.Background()

	require.False(t, gatewayprovider.ExecutionModelPolicy(&provider).SupportsCompatibleRouting(plainCtx, "client-model"))
	require.False(t, gatewayprovider.CompatibleProviderEligible(plainCtx, &provider, capability.PlatformOpenAI, "client-model", false, ""))

	scheduler := &compatiblePicker{service: newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{},

		Shared: Shared{},
	}, nil), stats: schedulercore.NewRuntimeStats(time.Now)}
	req := schedulercore.PlatformSelectionInput{Platform: capability.PlatformOpenAI, RequestedModel: "client-model", RoutingModel: "client-model"}
	require.False(t, scheduler.isProviderRequestCompatible(plainCtx, &provider, req))

	plainErr := noAvailableOpenAISelectionErrorForRoutingWithDetails(plainCtx, "client-model", "client-model", false, "", []gatewayprovider.ExecutionProvider{provider})
	var modelErr *routing.GroupModelUnsupportedError
	require.True(t, errors.As(plainErr, &modelErr))

	repo := schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{provider}}
	svc := newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{Providers: repo}}, nil)

	require.False(t, gatewayprovider.NewModelAvailability(gatewaytestkit.AvailabilityStore{Source: repo}, svc.groupPolicies, true).DiagnoseCompatibleRouting(plainCtx, nil, "client-model", capability.PlatformOpenAI).HasModelSupport)
}

// TestResolveOpenAIWSRoutingModelForProviderStrictlyFollowsBillingBasis 验证长连接每轮都严格按所选依据检查 R、C 或 U。

// TestResolveOpenAIWSRoutingModelForProviderRejectsUnsupportedMappedModel 验证后续 turn 不能绕过固定提供商的最终白名单。
