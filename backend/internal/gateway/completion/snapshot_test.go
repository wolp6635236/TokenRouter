package completion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResponseModelCompletionSnapshot 检查排队时复制的响应模型保持稳定。
func TestResponseModelCompletionSnapshot(t *testing.T) {
	source := &Input{Result: &Result{UpstreamModel: "sent", UpstreamResponseModel: "first"}}
	captured := Snapshot(source)
	source.Result.UpstreamResponseModel = "next-turn"
	require.Equal(t, "first", captured.Result.UpstreamResponseModel)
}
