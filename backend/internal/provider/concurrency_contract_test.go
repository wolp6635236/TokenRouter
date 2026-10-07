package provider

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProviderConcurrencyDefaultsInvalidGrokOAuthToOne(t *testing.T) {
	require.Equal(t, 1, NormalizeProviderConcurrency(capability.PlatformGrok, capability.ProviderTypeOAuth, 0))
	require.Equal(t, 1, NormalizeProviderConcurrency(capability.PlatformGrok, capability.ProviderTypeOAuth, -5))
}

func TestNormalizeProviderConcurrencyPreservesExplicitValues(t *testing.T) {
	require.Equal(t, 50, NormalizeProviderConcurrency(capability.PlatformGrok, capability.ProviderTypeOAuth, 50))
	require.Equal(t, 2, NormalizeProviderConcurrency(capability.PlatformOpenAI, capability.ProviderTypeOAuth, 2))
	require.Equal(t, 2, NormalizeProviderConcurrency(capability.PlatformGrok, capability.ProviderTypeAPIKey, 2))
}
