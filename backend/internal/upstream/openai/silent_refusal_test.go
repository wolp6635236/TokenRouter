package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

// TestOpenAIChatReasoningAliasForkConsumers 检查静默拒绝检测识别 reasoning 别名字段。
func TestOpenAIChatReasoningAliasForkConsumers(t *testing.T) {
	payload := `{"id":"chatcmpl-alias","model":"reasoning-model","choices":[{"index":0,"delta":{"reasoning":"fork reasoning"},"finish_reason":"stop"}]}`
	var chunk protocolopenai.ChatCompletionsChunk
	require.NoError(t, json.Unmarshal([]byte(payload), &chunk))

	require.True(t, protocolopenai.ChatChunkStartsResponsesOutput(&chunk))

	detector := NewChatSilentRefusalDetector(SilentRefusalMinRequestBodyBytes)
	detector.ObserveChatChunk(chunk)
	require.False(t, detector.IsSilentRefusal())
	require.True(t, detector.ShouldReleaseClientOutput())
}
