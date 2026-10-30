package openaiforward

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
)

// TestResultConversionsRetainIndependentNativeUsage round trips separate native
// cache buckets alongside inclusive OpenAI input. Mutating either conversion
// must leave its source observation available unchanged for completion capture.
func TestResultConversionsRetainIndependentNativeUsage(t *testing.T) {
	usage := protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 30, CacheCreationInputTokens: 12, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7, Speed: "fast"}
	source := &Result{Usage: AnthropicUsageToOpenAI(&usage), NativeUsage: &usage}
	forward := ToForwardResult(source)
	require.Equal(t, 142, forward.Usage.InputTokens)
	require.Equal(t, usage, *forward.NativeUsage)
	forward.NativeUsage.CacheCreation1hTokens = 99
	require.Equal(t, 7, source.NativeUsage.CacheCreation1hTokens)
	restored := FromForwardResult(forward)
	require.Equal(t, *forward.NativeUsage, *restored.NativeUsage)
	restored.NativeUsage.InputTokens = 999
	require.Equal(t, 100, forward.NativeUsage.InputTokens)
	require.Nil(t, ToForwardResult(&Result{}).NativeUsage)
	require.Nil(t, FromForwardResult(ToForwardResult(&Result{})).NativeUsage)
}
