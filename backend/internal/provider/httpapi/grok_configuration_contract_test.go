package httpapi_test

import (
	"net/http"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGrokMediaEligibilityExtra(t *testing.T) {
	t.Run("boolean override is accepted", func(t *testing.T) {
		extra, err := providercore.NormalizeGrokMediaEligibilityExtra(capability.PlatformGrok, map[string]any{providercore.GrokMediaEligibleExtraKey: false})

		require.NoError(t, err)
		require.Equal(t, false, extra[providercore.GrokMediaEligibleExtraKey])
	})

	t.Run("null clears override", func(t *testing.T) {
		extra, err := providercore.NormalizeGrokMediaEligibilityExtra(capability.PlatformGrok, map[string]any{providercore.GrokMediaEligibleExtraKey: nil})

		require.NoError(t, err)
		require.NotContains(t, extra, providercore.GrokMediaEligibleExtraKey)
	})

	t.Run("malformed override is rejected", func(t *testing.T) {
		_, err := providercore.NormalizeGrokMediaEligibilityExtra(capability.PlatformGrok, map[string]any{providercore.GrokMediaEligibleExtraKey: "false"})

		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
	})

	t.Run("other platforms ignore provider owned value", func(t *testing.T) {
		extra := map[string]any{providercore.GrokMediaEligibleExtraKey: "provider-owned"}
		normalized, err := providercore.NormalizeGrokMediaEligibilityExtra(capability.PlatformOpenAI, extra)

		require.NoError(t, err)
		require.Equal(t, extra, normalized)
	})
}

func TestNormalizeGrokMediaEligibilityUpdateExtra(t *testing.T) {
	provider := &providercore.Record{Platform: capability.PlatformGrok, Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: false}}

	t.Run("omitted override preserves current value", func(t *testing.T) {
		input := &providercore.UpdateProviderInput{Extra: map[string]any{"quota_used": float64(1)}}
		normalized, err := providercore.NormalizeGrokMediaEligibilityUpdateExtra(provider, input, map[string]any{"quota_used": float64(1)})

		require.NoError(t, err)
		require.Equal(t, false, normalized[providercore.GrokMediaEligibleExtraKey])
	})

	t.Run("null removes current override", func(t *testing.T) {
		input := &providercore.UpdateProviderInput{Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: nil}}
		normalized, err := providercore.NormalizeGrokMediaEligibilityUpdateExtra(provider, input, map[string]any{providercore.GrokMediaEligibleExtraKey: nil})

		require.NoError(t, err)
		require.NotContains(t, normalized, providercore.GrokMediaEligibleExtraKey)
		require.Contains(t, input.Extra, providercore.GrokMediaEligibleExtraKey)
	})

	t.Run("provided boolean replaces current override", func(t *testing.T) {
		input := &providercore.UpdateProviderInput{Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: true}}
		normalized, err := providercore.NormalizeGrokMediaEligibilityUpdateExtra(provider, input, map[string]any{providercore.GrokMediaEligibleExtraKey: true})

		require.NoError(t, err)
		require.Equal(t, true, normalized[providercore.GrokMediaEligibleExtraKey])
	})

	t.Run("malformed override is rejected on update", func(t *testing.T) {
		input := &providercore.UpdateProviderInput{Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: "false"}}
		_, err := providercore.NormalizeGrokMediaEligibilityUpdateExtra(provider, input, nil)

		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
	})

	t.Run("non grok update is unchanged", func(t *testing.T) {
		input := &providercore.UpdateProviderInput{Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: "provider-owned"}}
		normalized := map[string]any{providercore.GrokMediaEligibleExtraKey: "provider-owned"}
		got, err := providercore.NormalizeGrokMediaEligibilityUpdateExtra(&providercore.Record{Platform: capability.PlatformOpenAI}, input, normalized)

		require.NoError(t, err)
		require.Equal(t, normalized, got)
	})
}
