package provider_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	billingcore "github.com/TokenFlux/TokenRouter/internal/billing"
	billingpricing "github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	catalogprovider "github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeAPIKeyFastModeWireEncoding(t *testing.T) {
	resolver := fastModeTestResolver()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}

	forceOnCtx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "claude-opus-4-8")
	body, headers, err := gatewayprovider.
		ApplyAnthropicFastMode(forceOnCtx, resolver, provider, "claude-opus-4-8", []byte(`{"model":"claude-opus-4-8"}`), http.Header{})
	require.NoError(t, err)
	require.Equal(t, "fast", gjson.GetBytes(body, "speed").String())
	require.True(t, claude.ContainsBetaToken(claude.GetHeaderRaw(headers, "anthropic-beta"), claude.BetaFastMode))

	forceOffCtx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "claude-opus-4-8")
	claude.SetHeaderRaw(headers, "anthropic-beta", claude.BetaFastMode+",context-management-2025-06-27")
	body, headers, err = gatewayprovider.
		ApplyAnthropicFastMode(forceOffCtx, resolver, provider, "claude-opus-4-8", body, headers)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "speed").Exists())
	require.False(t, claude.ContainsBetaToken(claude.GetHeaderRaw(headers, "anthropic-beta"), claude.BetaFastMode))
	require.True(t, claude.ContainsBetaToken(claude.GetHeaderRaw(headers, "anthropic-beta"), "context-management-2025-06-27"))
}

// TestClaudeAPIKeyFastModeForceOffIgnoresCapabilityAndCredentialType 检查各凭据类型在缺少定价解析器时是否仍执行强制关闭。
func TestClaudeAPIKeyFastModeForceOffIgnoresCapabilityAndCredentialType(t *testing.T) {
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "claude-opus-4-8")

	for _, providerType := range []string{capability.ProviderTypeAPIKey, capability.ProviderTypeOAuth, capability.ProviderTypeSetupToken} {
		t.Run(providerType, func(t *testing.T) {
			headers := http.Header{}
			claude.SetHeaderRaw(headers, "anthropic-beta", claude.BetaFastMode+",context-management-2025-06-27")
			body, updatedHeaders, err := gatewayprovider.
				ApplyAnthropicFastMode(
					ctx, nil,

					&gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: providerType}},
					"claude-opus-4-8",
					[]byte(`{"model":"claude-opus-4-8","speed":"fast"}`),
					headers,
				)
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(body, "speed").Exists())
			require.False(t, claude.ContainsBetaToken(claude.GetHeaderRaw(updatedHeaders, "anthropic-beta"), claude.BetaFastMode))
			require.True(t, claude.ContainsBetaToken(claude.GetHeaderRaw(updatedHeaders, "anthropic-beta"), "context-management-2025-06-27"))
		})
	}
}

func fastModeTestContext(policy, model string) context.Context {
	ctx := apikey.WithFastModePolicy(context.Background(), policy)
	ctx = requeststate.WithGroup(ctx, &routing.Group{ID: 11})
	return context.WithValue(ctx, telemetry.Model, model)
}

func fastModeTestResolver() *billingcore.PriceResolver {
	pricing := catalogprovider.NewServiceFromSnapshot(catalogprovider.Options{ModelLookupCandidates: modelidentity.CandidatesFactory}, nil, catalogprovider.Snapshot{Data: map[string]*billingpricing.CatalogModelPricing{
		"gpt-5.5": {
			InputCostPerToken:     5e-6,
			OutputCostPerToken:    30e-6,
			SupportsServiceTier:   true,
			SupportsPromptCaching: true,
		},
		"claude-opus-4-8": {
			InputCostPerToken:     5e-6,
			OutputCostPerToken:    25e-6,
			SupportsServiceTier:   true,
			SupportsPromptCaching: true,
		},
	}})
	billing := billingtestkit.Calculator(pricing, nil)
	return billingtestkit.PriceResolver(nil, billing)
}
