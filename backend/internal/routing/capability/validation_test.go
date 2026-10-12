package capability

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
)

func TestMixedGroupFallbackSnapshotOwnsLists(t *testing.T) {
	input := map[ProtocolID][]ProtocolID{ProtocolOpenAIChatCompletions: {ProtocolAnthropicMessages}, ProtocolOpenAIResponses: {}}
	copy := protocol.CloneFallbacks(input)
	copy[ProtocolOpenAIChatCompletions][0] = ProtocolGeminiGenerateContent
	require.Equal(t, ProtocolAnthropicMessages, input[ProtocolOpenAIChatCompletions][0])
	_, exists := copy[ProtocolOpenAIResponses]
	require.True(t, exists)
	require.Empty(t, copy[ProtocolOpenAIResponses])
	require.NoError(t, ValidateProtocolFallbacks("", input))
	require.Error(t, ValidateProtocolFallbacks("", map[ProtocolID][]ProtocolID{"unknown": {}}))
	require.Error(t, ValidateProtocolFallbacks("", map[ProtocolID][]ProtocolID{ProtocolOpenAIChatCompletions: {ProtocolAnthropicMessages, ProtocolAnthropicMessages}}))
}
