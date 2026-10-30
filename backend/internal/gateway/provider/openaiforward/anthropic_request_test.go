package openaiforward

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNativeAnthropicTargetURLKeepsProtocolVersion covers root, relay prefix,
// versioned and complete Messages endpoints. The resulting request path must
// contain exactly one protocol version while retaining the configured prefix.
func TestNativeAnthropicTargetURLKeepsProtocolVersion(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.anthropic.com":                  "https://api.anthropic.com/v1/messages",
		"https://relay.example/prefix/anthropic":     "https://relay.example/prefix/anthropic/v1/messages",
		"https://opencode.ai/inference/anthropic/v1": "https://opencode.ai/inference/anthropic/v1/messages",
		"https://relay.example/prefix/v1/":           "https://relay.example/prefix/v1/messages",
		"https://relay.example/prefix/v1/messages":   "https://relay.example/prefix/v1/messages",
	} {
		got, err := NativeAnthropicTargetURL(1, base, func(value string) (string, error) { return value, nil })
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}
