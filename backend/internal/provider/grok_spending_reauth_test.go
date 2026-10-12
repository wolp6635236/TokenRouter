package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderGrokNeedsReauth(t *testing.T) {
	require.False(t, GrokNeedsReauth(nil))
	require.True(t, GrokNeedsReauth(&Record{
		Extra: map[string]any{"grok_needs_reauth": true},
	}))
	require.True(t, GrokNeedsReauth(&Record{
		Status:       StatusError,
		ErrorMessage: "Grok spending limit reached; reauthorize or wait for billing reset",
	}))
}
