package anthropic

// 速度用量场景覆盖 usage_patch.go 的 SSE 解析和 usage_events.go 的响应体解析。

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
)

func TestClaudeUsageSpeedParsing(t *testing.T) {
	usage := &protocol.TokenUsage{}
	ParseSSEUsage(`{"type":"message_start","message":{"usage":{"input_tokens":10,"speed":"fast"}}}`, usage)
	require.Equal(t, "fast", usage.Speed)

	parsed := ParseClaudeUsageFromResponseBody([]byte(`{"usage":{"input_tokens":10,"output_tokens":2,"speed":"standard"}}`))
	require.Equal(t, "standard", parsed.Speed)
}
