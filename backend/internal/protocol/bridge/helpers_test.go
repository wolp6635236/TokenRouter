package bridge

import (
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// collectStreamEvents 收集 Chat Completions 分片转换得到的 Responses 事件及收尾事件。
func collectStreamEvents(t *testing.T, chunks []string) []ResponsesStreamEvent {
	t.Helper()
	state := NewChatCompletionsToResponsesStreamState(testRuntime(), "deepseek-v4-pro")
	var events []ResponsesStreamEvent
	for _, payload := range chunks {
		var chunk ChatCompletionsChunk
		require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
		events = append(events, ChatCompletionsChunkToResponsesEvents(testRuntime(), &chunk, state)...)
	}
	events = append(events, FinalizeChatCompletionsResponsesStream(testRuntime(), state)...)
	return events
}

// responseObjectOf 从事件序列化后的 JSON 中取出 response 子对象。
func responseObjectOf(t *testing.T, evt ResponsesStreamEvent) map[string]any {
	t.Helper()
	m := marshalEvent(t, evt)
	resp, ok := m["response"].(map[string]any)
	require.True(t, ok, "event must carry a response object: %v", m)
	return resp
}

// requireCreatedAt 返回响应中大于零的 Unix 创建时间。
func requireCreatedAt(t *testing.T, resp map[string]any) int64 {
	t.Helper()
	raw, ok := resp["created_at"]
	require.True(t, ok, "response 对象必须带 created_at，否则严格客户端直接反序列化失败")
	value, ok := raw.(float64)
	require.True(t, ok, "created_at 必须是数字，得到 %T", raw)
	require.Greater(t, int64(value), int64(0), "created_at 必须是有效的 unix 时间戳")
	return int64(value)
}

// marshalEvent 通过自定义 MarshalJSON 序列化事件，并返回解码后的顶层对象。
func marshalEvent(t *testing.T, e ResponsesStreamEvent) map[string]any {
	t.Helper()
	b, err := json.Marshal(e)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func strPtr(s string) *string { return &s }

// testRuntime 为协议转换测试提供当前时间和随机字节。
func testRuntime() Runtime { return Runtime{Now: time.Now, ReadRandom: rand.Read} }
