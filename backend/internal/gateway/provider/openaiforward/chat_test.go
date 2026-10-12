package openaiforward

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestCursorMixedShapeDetection 检查 Responses 报文改写模型后的 input 和其他字段。
func TestCursorMixedShapeDetection(t *testing.T) {
	cursorBody := []byte(`{
		"user": "85df22e7463ab6c2",
		"model": "gpt-5.4",
		"stream": true,
		"input": [
			{"role":"system","content":"You are GPT-5.4 running as a coding agent."},
			{"role":"user","content":"hello"}
		],
		"service_tier": "auto",
		"reasoning": {"effort": "high"}
	}`)

	hasMessages := gjson.GetBytes(cursorBody, "messages").Exists()
	hasInput := gjson.GetBytes(cursorBody, "input").Exists()
	isResponsesShape := !hasMessages && hasInput

	require.True(t, isResponsesShape,
		"Cursor body must be detected as Responses-shape (has input, no messages)")

	const upstreamModel = "gpt-5.1-codex"
	rewritten, err := sjson.SetBytes(cursorBody, "model", upstreamModel)
	require.NoError(t, err)

	assert.Equal(t, upstreamModel, gjson.GetBytes(rewritten, "model").String())

	inputResult := gjson.GetBytes(rewritten, "input")
	require.True(t, inputResult.Exists(), "input field must still exist after rewrite")
	require.True(t, inputResult.IsArray(), "input must still be an array (not null, not object)")

	items := inputResult.Array()
	require.Len(t, items, 2, "both input items must be preserved")
	assert.Equal(t, "system", items[0].Get("role").String())
	assert.Equal(t, "You are GPT-5.4 running as a coding agent.",
		items[0].Get("content").String())
	assert.Equal(t, "user", items[1].Get("role").String())
	assert.Equal(t, "hello", items[1].Get("content").String())

	assert.Equal(t, "85df22e7463ab6c2", gjson.GetBytes(rewritten, "user").String())
	assert.Equal(t, true, gjson.GetBytes(rewritten, "stream").Bool())
	assert.Equal(t, "auto", gjson.GetBytes(rewritten, "service_tier").String())
	assert.Equal(t, "high", gjson.GetBytes(rewritten, "reasoning.effort").String())

	assert.NotContains(t, string(rewritten), `"input":null`,
		"rewritten body must not collapse input to null")
}

// TestCursorMixedShapeDetection_NormalChatCompletionsUnaffected 检查含 messages 的请求采用 Chat Completions 分支。
func TestCursorMixedShapeDetection_NormalChatCompletionsUnaffected(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hi"}],
		"stream": true
	}`)

	hasMessages := gjson.GetBytes(body, "messages").Exists()
	hasInput := gjson.GetBytes(body, "input").Exists()
	isResponsesShape := !hasMessages && hasInput

	assert.False(t, isResponsesShape,
		"standard Chat Completions body must NOT be detected as Responses-shape")
}

// TestCursorMixedShapeDetection_BothFieldsPrefersMessages 检查同时含 messages 和 input 的请求优先使用 messages。
func TestCursorMixedShapeDetection_BothFieldsPrefersMessages(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hi"}],
		"input": [{"role":"user","content":"other"}]
	}`)

	hasMessages := gjson.GetBytes(body, "messages").Exists()
	hasInput := gjson.GetBytes(body, "input").Exists()
	isResponsesShape := !hasMessages && hasInput

	assert.False(t, isResponsesShape,
		"when both messages and input are present, must not take the Cursor shortcut")
}

// TestCursorMixedShapeDetection_EmptyBody 检查空报文的请求形状判断。
func TestCursorMixedShapeDetection_EmptyBody(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","stream":true}`)

	hasMessages := gjson.GetBytes(body, "messages").Exists()
	hasInput := gjson.GetBytes(body, "input").Exists()
	isResponsesShape := !hasMessages && hasInput

	assert.False(t, isResponsesShape,
		"body with neither messages nor input must not be taken as Cursor shape")
}

// TestCursorMixedShape_JSONRoundtrip 检查 Responses 报文的 JSON 往返。
func TestCursorMixedShape_JSONRoundtrip(t *testing.T) {
	cursorBody := []byte(`{"model":"gpt-5.4","stream":true,"input":[{"role":"user","content":"hi"}]}`)

	rewritten, err := sjson.SetBytes(cursorBody, "model", "gpt-5.1-codex")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rewritten, &parsed))

	assert.Equal(t, "gpt-5.1-codex", parsed["model"])
	assert.Equal(t, true, parsed["stream"])

	inputArr, ok := parsed["input"].([]any)
	require.True(t, ok, "input must decode to a Go []any after round-trip")
	require.Len(t, inputArr, 1)
}

// TestCursorMixedShape_StripsUnsupportedFields 检查 Cursor 请求中不受支持字段的清理。
func TestCursorMixedShape_StripsUnsupportedFields(t *testing.T) {
	cursorBody := []byte(`{
		"model": "gpt-5.4",
		"stream": true,
		"prompt_cache_retention": "24h",
		"safety_identifier": "cursor-user-xyz",
		"metadata": {"trace_id":"abc","caller":"cursor"},
		"stream_options": {"include_usage": true},
		"input": [{"role":"user","content":"hi"}]
	}`)

	for _, field := range CursorResponsesUnsupportedFields {
		require.True(t, gjson.GetBytes(cursorBody, field).Exists(),
			"test fixture must contain %s", field)
	}

	result := cursorBody
	for _, field := range CursorResponsesUnsupportedFields {
		if stripped, err := sjson.DeleteBytes(result, field); err == nil {
			result = stripped
		}
	}

	for _, field := range CursorResponsesUnsupportedFields {
		assert.False(t, gjson.GetBytes(result, field).Exists(),
			"%s must be stripped", field)
	}

	assert.Equal(t, "gpt-5.4", gjson.GetBytes(result, "model").String())
	assert.Equal(t, true, gjson.GetBytes(result, "stream").Bool())
	assert.True(t, gjson.GetBytes(result, "input").IsArray())
	assert.Equal(t, "user", gjson.GetBytes(result, "input.0.role").String())
}
