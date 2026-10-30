package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// TestCompletionKeySnapshotPreservesSourceGroup captures a Key twice while
// changing its group multiplier between captures. The source keeps its group
// pointer, and each snapshot retains its own observed multiplier plus billing
// mode and rate-limit presence.
func TestCompletionKeySnapshotPreservesSourceGroup(t *testing.T) {
	group := &routing.Group{ID: 100, RateMultiplier: 2}
	key := &apikey.APIKey{Group: group, BillingMode: apikey.APIKeyBillingModeBalance, RateLimit5h: 1}
	first := ProjectCompletionKey(key)
	require.Same(t, group, key.Group)
	require.Equal(t, apikey.APIKeyBillingModeBalance, first.BillingMode)
	require.True(t, first.HasRateLimits)
	group.RateMultiplier = 3
	second := ProjectCompletionKey(key)
	require.Equal(t, 2.0, first.Group.RateMultiplier)
	require.Equal(t, 3.0, second.Group.RateMultiplier)
}

// TestCompletionProviderCapturesIndependentPolicies projects providers with
// different pricing and forbidden-counter requirements into completion data.
// OpenAI keeps counter resets without inheriting explicit Claude-alias pricing,
// and OpenCode inherits neither policy from its compatible wire protocols.
func TestCompletionProviderCapturesIndependentPolicies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		platform       string // source provider platform identity
		explicitPrice  bool   // alias needs configured pricing
		resetForbidden bool   // successful usage resets counter
	}{
		{providercore.PlatformOpenAI, false, true},
		{providercore.PlatformKimi, true, true},
		{providercore.PlatformZhipu, true, true},
		{providercore.PlatformDeepseek, true, true},
		{providercore.PlatformOpenCode, false, false},
		{providercore.PlatformAnthropic, false, false},
	}
	for _, test := range cases {
		t.Run(test.platform, func(t *testing.T) {
			snapshot := ProjectCompletionProvider(&providercore.Record{Platform: test.platform, Type: providercore.ProviderTypeAPIKey})
			require.Equal(t, test.explicitPrice, snapshot.RequiresExplicitClaudePricing)
			require.Equal(t, test.resetForbidden, snapshot.ResetForbiddenCounterOnSuccess)
		})
	}
}
