package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGroupRequestsDecodeOpenAIFast 验证管理请求能区分免费 Fast 的显式 true/false
// 与更新请求中的省略状态。
func TestGroupRequestsDecodeOpenAIFast(t *testing.T) {
	var createReq CreateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"fast","force_openai_fast":true}`), &createReq))
	require.True(t, createReq.ForceOpenAIFast)

	var updateReq UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"force_openai_fast":false}`), &updateReq))
	require.NotNil(t, updateReq.ForceOpenAIFast)
	require.False(t, *updateReq.ForceOpenAIFast)

	var omitted UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &omitted))
	require.Nil(t, omitted.ForceOpenAIFast)
}
