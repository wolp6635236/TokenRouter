package grok

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountGrokNativeSearchCallsFromJSONBytes(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, CountGrokNativeSearchCallsFromJSONBytes(nil))
	require.Equal(t, 0, CountGrokNativeSearchCallsFromJSONBytes([]byte(`{"output":[]}`)))
	body := []byte(`{"output":[
		{"type":"web_search_call","id":"ws1","status":"completed"},
		{"type":"x_search_call","id":"xs1"},
		{"type":"function_call","name":"tool_search","call_id":"ts1"},
		{"type":"function_call","name":"lookup","call_id":"other"}
	]}`)
	require.Equal(t, 3, CountGrokNativeSearchCallsFromJSONBytes(body))
}

func TestCountGrokNativeSearchCallsFromJSONBytes_PrefersNestedResponse(t *testing.T) {
	t.Parallel()
	body := []byte(`{"output":[{"type":"web_search_call","id":"duplicate"}],"response":{"output":[{"type":"web_search_call","id":"duplicate"},{"type":"x_search_call","id":"xs1"}]}}`)
	require.Equal(t, 2, CountGrokNativeSearchCallsFromJSONBytes(body))
}

func TestCountGrokNativeSearchCallsFromJSONBytes_FallsBackWhenNestedOutputNull(t *testing.T) {
	t.Parallel()
	body := []byte(`{"output":[{"type":"web_search_call","id":"ws1"}],"response":{"output":null}}`)
	require.Equal(t, 1, CountGrokNativeSearchCallsFromJSONBytes(body))
}

func TestCountGrokNativeSearchCallsFromSSEBodyDedups(t *testing.T) {
	t.Parallel()
	sse := stringsJoin(
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call","id":"ws1","call_id":"c1"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call","id":"ws1","call_id":"c1"}}`,
		`data: {"type":"response.completed","response":{"output":[{"type":"web_search_call","id":"ws1","call_id":"c1"},{"type":"x_search_call","id":"xs1","call_id":"c2"}]}}`,
	)
	require.Equal(t, 2, CountGrokNativeSearchCallsFromSSEBody(sse))
}

func TestCountGrokNativeSearchCallsInSSEDataDedup_LiveStreamPath(t *testing.T) {
	t.Parallel()
	// 模拟实时流式累加器，依次处理 item.done 与 response.completed。
	// 同一 call_id 只能计费一次，防止附加费接近翻倍的回归。
	seen := make(map[string]struct{})
	done := []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call","id":"ws1","call_id":"c1"}}`)
	completed := []byte(`{"type":"response.completed","response":{"output":[{"type":"web_search_call","id":"ws1","call_id":"c1"},{"type":"x_search_call","id":"xs1","call_id":"c2"}]}}`)
	require.Equal(t, 1, CountGrokNativeSearchCallsInSSEDataDedup(done, seen))
	require.Equal(t, 1, CountGrokNativeSearchCallsInSSEDataDedup(completed, seen))
	// 未去重的原始路径仍会重复统计同一对事件包。
	require.Equal(t, 1, CountGrokNativeSearchCallsInSSEData(done))
	require.Equal(t, 2, CountGrokNativeSearchCallsInSSEData(completed))
}

func TestCountGrokNativeSearchCallsInSSEDataDedup_NoIDStillDedups(t *testing.T) {
	t.Parallel()
	// 上游省略 call_id 或 id 时，计数器用合成键识别重复调用。
	seen := make(map[string]struct{})
	done := []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call"}}`)
	completed := []byte(`{"type":"response.completed","response":{"output":[{"type":"web_search_call"}]}}`)
	require.Equal(t, 1, CountGrokNativeSearchCallsInSSEDataDedup(done, seen))
	require.Equal(t, 0, CountGrokNativeSearchCallsInSSEDataDedup(completed, seen))
}

func TestCountGrokNativeSearchCallsInSSEDataDedup_MultipleNoIDCalls(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	firstDone := []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call"}}`)
	secondDone := []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call"}}`)
	completed := []byte(`{"type":"response.completed","response":{"output":[{"type":"web_search_call"},{"type":"web_search_call"}]}}`)
	require.Equal(t, 1, CountGrokNativeSearchCallsInSSEDataDedup(firstDone, seen))
	require.Equal(t, 1, CountGrokNativeSearchCallsInSSEDataDedup(secondDone, seen))
	require.Equal(t, 0, CountGrokNativeSearchCallsInSSEDataDedup(completed, seen))
}

func stringsJoin(lines ...string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n\n"
	}
	return out
}

func TestCountGrokNativeSearchCallsFromJSON_MessagesStyleBody(t *testing.T) {
	// 验证 Anthropic 缓冲的 Grok /v1/messages 路径使用同一计数器。
	body := []byte(`{"id":"r1","output":[{"type":"web_search_call","id":"ws1"},{"type":"message","role":"assistant"}]}`)
	require.Equal(t, 1, CountGrokNativeSearchCallsFromJSONBytes(body))
}
