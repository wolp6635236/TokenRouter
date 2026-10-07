package provider_test

import (
	"context"
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/stretchr/testify/require"
)

// TestGrokMediaCapabilityFiltersOnlyGeneration 验证 Grok 媒体能力只限制生成请求。
func TestGrokMediaCapabilityFiltersOnlyGeneration(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 1,
			Platform:    capability.PlatformGrok,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Extra:       map[string]any{providercore.GrokMediaEligibleExtraKey: false},
		},
	}

	require.True(t, provideradapter.SupportsOpenAIEndpoint(gatewayprovider.ExecutionProtocolRecord(provider), providercore.OpenAIEndpointCapabilityTextGeneration))
	require.False(t, provideradapter.SupportsOpenAIEndpoint(gatewayprovider.ExecutionProtocolRecord(provider), providercore.OpenAIEndpointCapabilityGrokMediaGeneration))
	require.False(t, gatewayprovider.
		CompatibleProviderEligible(
			context.Background(), provider, capability.PlatformGrok, "grok-imagine-video", false,
			providercore.OpenAIEndpointCapabilityGrokMediaGeneration,
		))
}
