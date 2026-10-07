package provider_test

import (
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestProviderGrokNeedsReauth(t *testing.T) {
	require.False(t, providercore.GrokNeedsReauth(nil))
	require.True(t, providercore.GrokNeedsReauth(&providercore.Record{
		Extra: map[string]any{"grok_needs_reauth": true},
	}))
	require.True(t, providercore.GrokNeedsReauth(&providercore.Record{
		Status:       providercore.StatusError,
		ErrorMessage: "Grok spending limit reached; reauthorize or wait for billing reset",
	}))
}
