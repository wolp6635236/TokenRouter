package provider

import (
	"net/http"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

func TestGrokMediaCapabilityKeepsOnlyUnobservedOAuthAsProbeCandidate(t *testing.T) {
	unobserved := &providercore.Record{Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}
	eligible, reason := providercore.GrokMediaGenerationEligibility(unobserved, GrokTierRules())
	require.False(t, eligible)
	require.Equal(t, "billing_unobserved", reason)
	require.True(t, SupportsOpenAIEndpoint(unobserved, providercore.OpenAIEndpointCapabilityGrokMediaGeneration))

	inconclusive := &providercore.Record{
		Platform: capability.PlatformGrok,
		Type:     capability.ProviderTypeOAuth,
		Extra: map[string]any{providercore.GrokUsageBillingExtraKey: &xai.BillingSummary{
			StatusCode: http.StatusOK,
			Partial:    true,
		}},
	}
	require.False(t, SupportsOpenAIEndpoint(inconclusive, providercore.OpenAIEndpointCapabilityGrokMediaGeneration))
}
