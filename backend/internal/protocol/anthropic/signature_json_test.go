package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStripAnthropicThinkingSignaturesPreservesLargeIntegers(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"work","signature":"opaque"}]}],"metadata":{"large_id":9007199254740993}}`)

	patched, changed := StripThinkingSignaturesJSON(body)

	require.True(t, changed)
	require.False(t, gjson.GetBytes(patched, "messages.0.content.0.signature").Exists())
	require.Equal(t, "9007199254740993", gjson.GetBytes(patched, "metadata.large_id").Raw)
}
