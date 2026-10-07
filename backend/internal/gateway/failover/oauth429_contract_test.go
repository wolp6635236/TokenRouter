package failover

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldStopOpenAIOAuth429Failover_AfterBoundedFullWindows(t *testing.T) {
	provider := OAuth429Provider{OpenAI: true}
	apiKeyProvider := OAuth429Provider{}
	var state OAuth429State

	require.False(t, StopOAuth429(provider, 429, 1, &state))

	require.False(t, StopOAuth429(provider, 429, 1, &state))
	require.False(t, StopOAuth429(provider, 429, 2, &state))
	require.True(t, StopOAuth429(provider, 429, 3, &state))
	require.False(t, StopOAuth429(apiKeyProvider, 429, 1, &state))
	require.False(t, StopOAuth429(provider, 500, 1, &state))
	require.False(t, StopOAuth429(provider, 429, 0, &state))
}

func TestShouldStopOpenAIOAuth429Failover_TracksOneGrokFollowupAttempt(t *testing.T) {
	provider := OAuth429Provider{Grok: true}
	apiKeyProvider := OAuth429Provider{}

	t.Run("429 then 500 stops after one followup", func(t *testing.T) {
		var state OAuth429State
		require.False(t, StopOAuth429(provider, 429, 1, &state))
		require.True(t, StopOAuth429(provider, 500, 2, &state))
	})

	t.Run("500 then 429 still allows one followup", func(t *testing.T) {
		var state OAuth429State
		require.False(t, StopOAuth429(provider, 500, 1, &state))
		require.False(t, StopOAuth429(provider, 429, 2, &state))
		require.True(t, StopOAuth429(provider, 502, 3, &state))
	})

	t.Run("OAuth 429 then API-key failure consumes the same followup", func(t *testing.T) {
		var state OAuth429State
		require.False(t, StopOAuth429(provider, 429, 1, &state))
		require.True(t, StopOAuth429(apiKeyProvider, 500, 2, &state))
	})

	var state OAuth429State
	require.False(t, StopOAuth429(provider, 429, 0, &state))
	require.False(t, StopOAuth429(apiKeyProvider, 429, 2, &state))
}
