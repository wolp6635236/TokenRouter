package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractAnthropicSSEDataLine(t *testing.T) {
	t.Run("valid data line with spaces", func(t *testing.T) {
		data, ok := ExtractSSEDataLine("data:   {\"type\":\"message_start\"}")
		require.True(t, ok)
		require.Equal(t, `{"type":"message_start"}`, data)
	})

	t.Run("non data line", func(t *testing.T) {
		data, ok := ExtractSSEDataLine("event: message_start")
		require.False(t, ok)
		require.Empty(t, data)
	})
}
