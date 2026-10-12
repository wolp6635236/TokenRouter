package bridge

// 本文件覆盖 chatcompletions_anthropic_bridge.go、chatcompletions_responses_bridge.go、
// responses_to_anthropic.go 和 responses_to_chatcompletions.go 的推理内容、用量和工具流转换。

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChatReasoningAliasNonStreaming 验证非流响应的 reasoning 别名进入两种桥接协议。
func TestChatReasoningAliasNonStreaming(t *testing.T) {
	var response ChatCompletionsResponse
	require.NoError(t, json.Unmarshal([]byte(`{
		"id":"chatcmpl-alias","model":"reasoning-model",
		"choices":[{"index":0,"message":{"role":"assistant","content":"final answer","reasoning":"fallback reasoning"},"finish_reason":"stop"}]
	}`), &response))

	anthropic := ChatCompletionsResponseToAnthropic(testRuntime(), &response, "claude-sonnet")
	require.Len(t, anthropic.Content, 2)
	require.Equal(t, "thinking", anthropic.Content[0].Type)
	require.Equal(t, "fallback reasoning", anthropic.Content[0].Thinking)

	responses := ChatCompletionsResponseToResponses(testRuntime(), &response, "reasoning-model", nil, nil, false, nil)
	require.Len(t, responses.Output, 2)
	require.Equal(t, "reasoning", responses.Output[0].Type)
	require.Equal(t, "fallback reasoning", responses.Output[0].Summary[0].Text)
}

// TestChatReasoningAliasStreaming 验证流式别名和正式字段的优先级。
func TestChatReasoningAliasStreaming(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "alias fallback",
			payload: `{"id":"chatcmpl-alias","model":"reasoning-model","choices":[{"index":0,"delta":{"reasoning":"streamed fallback"},"finish_reason":null}]}`,
			want:    "streamed fallback",
		},
		{
			name:    "reasoning content precedence",
			payload: `{"id":"chatcmpl-alias","model":"reasoning-model","choices":[{"index":0,"delta":{"reasoning_content":"preferred reasoning","reasoning":"fallback reasoning"},"finish_reason":null}]}`,
			want:    "preferred reasoning",
		},
		{
			name:    "explicit empty reasoning content precedence",
			payload: `{"id":"chatcmpl-alias","model":"reasoning-model","choices":[{"index":0,"delta":{"reasoning_content":"","reasoning":"must not leak"},"finish_reason":null}]}`,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var chunk ChatCompletionsChunk
			require.NoError(t, json.Unmarshal([]byte(tt.payload), &chunk))

			anthropicEvents := ChatCompletionsChunkToAnthropicEvents(testRuntime(), &chunk, NewChatCompletionsToAnthropicStreamState(testRuntime(), "reasoning-model"))
			var anthropicReasoning string
			for _, event := range anthropicEvents {
				if event.Delta != nil && event.Delta.Type == "thinking_delta" {
					anthropicReasoning += event.Delta.Thinking
				}
			}
			require.Equal(t, tt.want, anthropicReasoning)

			responseEvents := ChatCompletionsChunkToResponsesEvents(testRuntime(), &chunk, NewChatCompletionsToResponsesStreamState(testRuntime(), "reasoning-model"))
			var responsesReasoning string
			for _, event := range responseEvents {
				if event.Type == "response.reasoning_summary_text.delta" {
					responsesReasoning += event.Delta
				}
			}
			require.Equal(t, tt.want, responsesReasoning)
		})
	}
}

func TestUsageConversionsPreserveCacheWriteTokens(t *testing.T) {
	var responsesUsage ResponsesUsage
	require.NoError(t, json.Unmarshal([]byte(`{
		"input_tokens":1000,
		"output_tokens":50,
		"input_tokens_details":{"cached_tokens":100,"cache_write_tokens":200}
	}`), &responsesUsage))
	require.NotNil(t, responsesUsage.InputTokensDetails)
	require.Equal(t, 200, responsesUsage.InputTokensDetails.CacheWriteTokens)

	chatUsage := chatUsageFromResponsesUsage(&responsesUsage)
	require.NotNil(t, chatUsage.PromptTokensDetails)
	require.Equal(t, 100, chatUsage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 200, chatUsage.PromptTokensDetails.CacheWriteTokens)

	roundTrip := ChatUsageToResponsesUsage(chatUsage)
	require.NotNil(t, roundTrip.InputTokensDetails)
	require.Equal(t, 200, roundTrip.CacheCreationInputTokens)
	require.Equal(t, 200, roundTrip.InputTokensDetails.CacheWriteTokens)
}

// TestStreamingParallelToolUseNoGhostDelta 检查 Chat Completions 经 Responses 转为 Anthropic 的并行工具流。
// function_call_arguments.done 中的完整参数需要发到已启动的工具块索引，否则 Claude Code 会报告“Content block not found”。
func TestStreamingParallelToolUseNoGhostDelta(t *testing.T) {
	ccState := NewChatCompletionsToResponsesStreamState(testRuntime(), "glm-5.2")
	anthropicState := NewResponsesEventToAnthropicState(testRuntime())
	anthropicState.Model = "glm-5.2"

	// 第一个 chunk 携带首个 tool_call 的 ID、名称和打包参数。
	chatChunk1 := &ChatCompletionsChunk{
		ID:    "chatcmpl-1",
		Model: "glm-5.2",
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{
				ToolCalls: []ChatToolCall{{
					Index: intPtr(0),
					ID:    "call_weather",
					Type:  "function",
					Function: ChatFunctionCall{
						Name:      "get_weather",
						Arguments: `{"city":"Tokyo"}`,
					},
				}},
			},
		}},
	}

	// 第二个 chunk 携带第二个 tool_call 的 ID、名称和打包参数。
	chatChunk2 := &ChatCompletionsChunk{
		ID:    "chatcmpl-1",
		Model: "glm-5.2",
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{
				ToolCalls: []ChatToolCall{{
					Index: intPtr(1),
					ID:    "call_time",
					Type:  "function",
					Function: ChatFunctionCall{
						Name:      "get_time",
						Arguments: `{}`,
					},
				}},
			},
		}},
	}

	// 第三个 chunk 结束工具调用。
	chatChunk3 := &ChatCompletionsChunk{
		ID:    "chatcmpl-1",
		Model: "glm-5.2",
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{},
			FinishReason: strPtr("tool_calls"),
		}},
	}

	// 先将 chunk 送入 CC→Responses 桥接，再转换为 Anthropic 事件。
	var allAnthropicEvents []AnthropicStreamEvent
	for _, chunk := range []*ChatCompletionsChunk{chatChunk1, chatChunk2, chatChunk3} {
		responsesEvents := ChatCompletionsChunkToResponsesEvents(testRuntime(), chunk, ccState)
		for _, rEvent := range responsesEvents {
			allAnthropicEvents = append(allAnthropicEvents, ResponsesEventToAnthropicEvents(&rEvent, anthropicState)...)
		}
	}

	// 收尾时 closeChatToolItems 为每个工具发出 function_call_arguments.done。
	finalResponsesEvents := FinalizeChatCompletionsResponsesStream(testRuntime(), ccState)
	for _, rEvent := range finalResponsesEvents {
		allAnthropicEvents = append(allAnthropicEvents, ResponsesEventToAnthropicEvents(&rEvent, anthropicState)...)
	}

	// 收集已收到 content_block_start 的 block 索引。
	startedBlocks := make(map[int]string) // 索引到 block 类型的映射。
	for _, e := range allAnthropicEvents {
		if e.Type == "content_block_start" && e.ContentBlock != nil {
			startedBlocks[*e.Index] = e.ContentBlock.Type
		}
	}

	// 每个 content_block_delta 指向已启动的 block。
	for _, e := range allAnthropicEvents {
		if e.Type != "content_block_delta" || e.Index == nil {
			continue
		}
		idx := *e.Index
		_, ok := startedBlocks[idx]
		require.Truef(t, ok,
			"content_block_delta on index %d which was never content_block_start'ed (ghost delta bug #4193)", idx)
	}

	// 每个 content_block_stop 指向已启动的 block。
	for _, e := range allAnthropicEvents {
		if e.Type != "content_block_stop" || e.Index == nil {
			continue
		}
		idx := *e.Index
		_, ok := startedBlocks[idx]
		require.Truef(t, ok,
			"content_block_stop on index %d which was never content_block_start'ed", idx)
	}

	// 两个 tool_use block 都应已启动。
	var toolUseBlocks []int
	for idx, blockType := range startedBlocks {
		if blockType == "tool_use" {
			toolUseBlocks = append(toolUseBlocks, idx)
		}
	}
	assert.Len(t, toolUseBlocks, 2, "both parallel tool_use blocks should be opened")

	// 收尾原因应为 tool_use。
	var sawMessageDelta bool
	for _, e := range allAnthropicEvents {
		if e.Type == "message_delta" {
			sawMessageDelta = true
			assert.Equal(t, "tool_use", e.Delta.StopReason)
		}
	}
	assert.True(t, sawMessageDelta, "message_delta should be emitted")
}

func intPtr(v int) *int { return &v }
