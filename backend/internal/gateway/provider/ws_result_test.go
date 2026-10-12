package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
)

// 本地预热标记经过双向结果转换后仍阻止上游成功反馈。
func TestWSLocalWarmupResultRoundTrip(t *testing.T) {
	original := &forward.OpenAIResult{LocalWarmup: true, RequestID: "resp_warmup", OpenAIWSMode: true, UpstreamTerminalEvent: "response.completed"}
	result := ForwardResultFromWS(ProjectWSResult(original))
	require.True(t, result.LocalWarmup)
	require.Equal(t, original.RequestID, result.RequestID)
	require.False(t, result.SucceededForScheduling())
}
