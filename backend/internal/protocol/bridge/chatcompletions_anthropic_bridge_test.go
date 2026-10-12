package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// assembledToolUse 保存重建后的工具调用 ID、名称和参数。
type assembledToolUse struct {
	ID    string
	Name  string
	Input string
}

func TestChatCompletionsResponseToAnthropic_TextOnly(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-1",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Index:        0,
			Message:      ChatMessage{Role: "assistant", Content: json.RawMessage(`"hello world"`)},
			FinishReason: "stop",
		}},
		Usage: &ChatUsage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	require.Equal(t, "chatcmpl-1", out.ID)
	require.Equal(t, "claude-sonnet-4-20250514", out.Model)
	require.Len(t, out.Content, 1)
	require.Equal(t, "text", out.Content[0].Type)
	require.Equal(t, "hello world", out.Content[0].Text)
	require.Equal(t, "end_turn", AnthropicStopReasonString(out.StopReason))
	require.Equal(t, 5, out.Usage.InputTokens)
	require.Equal(t, 2, out.Usage.OutputTokens)
}

func TestChatCompletionsResponseToAnthropic_ToolUse(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-2",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Index: 0,
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: ChatFunctionCall{
						Name:      "get_weather",
						Arguments: `{"city":"SF"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	require.Len(t, out.Content, 1)
	require.Equal(t, "tool_use", out.Content[0].Type)
	require.Equal(t, "call_1", out.Content[0].ID)
	require.Equal(t, "get_weather", out.Content[0].Name)
	require.Equal(t, `{"city":"SF"}`, string(out.Content[0].Input))
	require.Equal(t, "tool_use", AnthropicStopReasonString(out.StopReason))
}

func TestChatCompletionsResponseToAnthropic_ReasoningOnlyFallback(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-3",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Index: 0,
			Message: ChatMessage{
				Role:             "assistant",
				ReasoningContent: "I should think about this",
			},
			FinishReason: "stop",
		}},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	// 同时生成 thinking 和 text，缺少正文时将 reasoning 用作可见文本。
	require.Len(t, out.Content, 2)
	require.Equal(t, "thinking", out.Content[0].Type)
	require.Equal(t, "I should think about this", out.Content[0].Thinking)
	require.Equal(t, "text", out.Content[1].Type)
	require.Equal(t, "I should think about this", out.Content[1].Text)
}

func TestChatCompletionsResponseToAnthropic_FinishReasonLength(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-4",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Index:        0,
			Message:      ChatMessage{Role: "assistant", Content: json.RawMessage(`"truncated"`)},
			FinishReason: "length",
		}},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	require.Equal(t, "max_tokens", AnthropicStopReasonString(out.StopReason))
}

func TestChatCompletionsResponseToAnthropic_EmptyChoices(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:      "chatcmpl-5",
		Model:   "deepseek-v4-pro",
		Choices: []ChatChoice{},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	require.Len(t, out.Content, 1)
	require.Equal(t, "text", out.Content[0].Type)
	require.Equal(t, "", out.Content[0].Text)
	require.Equal(t, "end_turn", AnthropicStopReasonString(out.StopReason), "empty choices must not produce an empty stop_reason")
}

func TestChatCompletionsResponseToAnthropic_CacheTokens(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-6",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Index:        0,
			Message:      ChatMessage{Role: "assistant", Content: json.RawMessage(`"hi"`)},
			FinishReason: "stop",
		}},
		Usage: &ChatUsage{
			PromptTokens:     100,
			CompletionTokens: 5,
			TotalTokens:      105,
			PromptTokensDetails: &ChatTokenDetails{
				CachedTokens:        30,
				CacheCreationTokens: 10,
			},
		},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	// input = prompt(100) - cached(30) - cacheCreation(10) = 60。
	require.Equal(t, 60, out.Usage.InputTokens)
	require.Equal(t, 5, out.Usage.OutputTokens)
	require.Equal(t, 30, out.Usage.CacheReadInputTokens)
	require.Equal(t, 10, out.Usage.CacheCreationInputTokens)
}

func TestChatCompletionsResponseToAnthropic_NilResponse(t *testing.T) {
	out := ChatCompletionsResponseToAnthropic(testRuntime(), nil, "claude-sonnet-4-20250514")
	require.Len(t, out.Content, 1)
	require.Equal(t, "text", out.Content[0].Type)
	require.Equal(t, "end_turn", AnthropicStopReasonString(out.StopReason), "nil response must not produce an empty stop_reason")
	require.NotEmpty(t, out.ID)
}

func TestChatCompletionsChunkToAnthropicEvents_TextOnly(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":" world"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
	})

	types := anthropicEventTypes(events)
	// 验证完整文本块与消息生命周期。
	require.Equal(t, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}, types)

	// 验证文本增量。
	var texts []string
	for _, e := range events {
		if e.Type == "content_block_delta" && e.Delta != nil {
			texts = append(texts, e.Delta.Text)
		}
	}
	require.Equal(t, []string{"hello", " world"}, texts)

	// 验证停止原因。
	for _, e := range events {
		if e.Type == "message_delta" {
			require.Equal(t, "end_turn", e.Delta.StopReason)
			require.Equal(t, 5, e.Usage.InputTokens)
			require.Equal(t, 2, e.Usage.OutputTokens)
		}
	}
}

func TestChatCompletionsChunkToAnthropicEvents_ReasoningThenContent(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"thinking..."}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
	})

	types := anthropicEventTypes(events)
	// thinking block 关闭后再开始 text block。
	require.Equal(t, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}, types)

	// 第一个内容块是 thinking，第二个是 text。
	var blockTypes []string
	for _, e := range events {
		if e.Type == "content_block_start" && e.ContentBlock != nil {
			blockTypes = append(blockTypes, e.ContentBlock.Type)
		}
	}
	require.Equal(t, []string{"thinking", "text"}, blockTypes)
}

func TestChatCompletionsChunkToAnthropicEvents_ToolCallAggregation(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"SF\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
	})

	types := anthropicEventTypes(events)
	// 参数分片应在流收尾时合并为一个完整 JSON delta。
	require.Equal(t, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}, types)

	// 验证 tool_use block。
	for _, e := range events {
		if e.Type == "content_block_start" && e.ContentBlock != nil {
			require.Equal(t, "tool_use", e.ContentBlock.Type)
			require.Equal(t, "call_1", e.ContentBlock.ID)
			require.Equal(t, "get_weather", e.ContentBlock.Name)
		}
		if e.Type == "message_delta" {
			require.Equal(t, "tool_use", e.Delta.StopReason)
		}
	}

	// 验证跳过空首段后组装出的完整参数。
	var partials []string
	for _, e := range events {
		if e.Type == "content_block_delta" && e.Delta != nil {
			partials = append(partials, e.Delta.PartialJSON)
		}
	}
	require.Equal(t, []string{`{"city":"SF"}`}, partials)
}

func TestChatCompletionsChunkToAnthropicEvents_LengthMapsToMaxTokens(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
	})

	for _, e := range events {
		if e.Type == "message_delta" {
			require.Equal(t, "max_tokens", e.Delta.StopReason)
		}
	}
}

func TestChatCompletionsChunkToAnthropicEvents_EmptyStream(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`,
	})

	types := anthropicEventTypes(events)
	// 空内容也发出消息开始、增量和停止事件。
	require.Contains(t, types, "message_start")
	require.Contains(t, types, "message_stop")
}

func TestChatCompletionsChunkToAnthropicEvents_MessageStartEmittedOnce(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"content":"a"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"b"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
	})

	count := 0
	for _, e := range events {
		if e.Type == "message_start" {
			count++
		}
	}
	require.Equal(t, 1, count, "message_start should only be emitted once")
}

func TestChatCompletionsChunkToAnthropicEvents_ParallelToolCalls(t *testing.T) {
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"tool_b","arguments":"{\"b\":"}},{"index":0,"id":"call_1","type":"function","function":{"name":"tool_a","arguments":"{\"a\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1"}},{"index":1,"function":{"arguments":"2"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"}"}},{"index":0,"function":{"arguments":"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
	})

	// 即使声明顺序与参数分片交错，最终仍按工具 index 输出完整调用。
	tools := assembleToolUseBlocks(events)
	require.Len(t, tools, 2)
	require.Equal(t, assembledToolUse{ID: "call_1", Name: "tool_a", Input: `{"a":1}`}, tools[0])
	require.Equal(t, assembledToolUse{ID: "call_2", Name: "tool_b", Input: `{"b":2}`}, tools[1])

	// 每个 block 按 start、delta、stop 顺序输出，stop 结束该块。
	blockState := make(map[int]string)
	for _, e := range events {
		if e.Index == nil {
			continue
		}
		idx := *e.Index
		switch e.Type {
		case "content_block_start":
			require.Empty(t, blockState[idx], "block %d must start once", idx)
			blockState[idx] = "open"
		case "content_block_delta":
			require.Equal(t, "open", blockState[idx], "block %d delta must occur while open", idx)
		case "content_block_stop":
			require.Equal(t, "open", blockState[idx], "block %d stop must follow start", idx)
			blockState[idx] = "closed"
		}
	}
	require.Equal(t, map[int]string{0: "closed", 1: "closed"}, blockState)

	for _, e := range events {
		if e.Type == "message_delta" {
			require.Equal(t, "tool_use", e.Delta.StopReason)
		}
	}
}

func TestChatCompletionsChunkToAnthropicEvents_BuffersToolArgumentFragments(t *testing.T) {
	state := NewChatCompletionsToAnthropicStreamState(testRuntime(), "test")
	toolIndex := 0

	// 用大量小分片模拟 Write/Edit 一类大参数，状态中不得反复累加完整字符串。
	ChatCompletionsChunkToAnthropicEvents(testRuntime(), &ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index: &toolIndex,
				ID:    "call_many_fragments",
				Function: ChatFunctionCall{
					Name:      "Write",
					Arguments: `{"content":"`,
				},
			}}},
		}},
	}, state)
	const fragmentCount = 1024
	for range fragmentCount {
		ChatCompletionsChunkToAnthropicEvents(testRuntime(), &ChatCompletionsChunk{
			Choices: []ChatChunkChoice{{
				Delta: ChatDelta{ToolCalls: []ChatToolCall{{
					Index:    &toolIndex,
					Function: ChatFunctionCall{Arguments: "x"},
				}}},
			}},
		}, state)
	}
	ChatCompletionsChunkToAnthropicEvents(testRuntime(), &ChatCompletionsChunk{
		Choices: []ChatChunkChoice{{
			Delta: ChatDelta{ToolCalls: []ChatToolCall{{
				Index:    &toolIndex,
				Function: ChatFunctionCall{Arguments: `"}`},
			}}},
		}},
	}, state)

	require.Empty(t, state.toolCalls[0].Function.Arguments)
	require.Len(t, state.toolArgumentFragments[0], fragmentCount+2)

	events := FinalizeChatCompletionsAnthropicStream(testRuntime(), state)
	require.Len(t, events, 5)
	require.Equal(t, `{"content":"`+strings.Repeat("x", fragmentCount)+`"}`, events[1].Delta.PartialJSON)
}

func TestFinalizeChatCompletionsAnthropicStream_NoOpAfterStop(t *testing.T) {
	state := NewChatCompletionsToAnthropicStreamState(testRuntime(), "test")
	state.MessageStopSent = true

	events := FinalizeChatCompletionsAnthropicStream(testRuntime(), state)
	require.Nil(t, events, "finalize should be a no-op after message_stop")
}

func TestFinalizeChatCompletionsAnthropicStream_EmitsMessageStartIfMissing(t *testing.T) {
	state := NewChatCompletionsToAnthropicStreamState(testRuntime(), "test")
	// 未输入任何 chunk，此时尚未发送 message_start。

	events := FinalizeChatCompletionsAnthropicStream(testRuntime(), state)
	types := anthropicEventTypes(events)
	require.Contains(t, types, "message_start")
	require.Contains(t, types, "message_stop")
}

// TestDirectBridge_NonStreamingMatchesDoubleConversion 检查直接转换与经过 Responses 的两步转换生成相同的非流响应。
func TestDirectBridge_NonStreamingMatchesDoubleConversion(t *testing.T) {
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-eq",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Index: 0,
			Message: ChatMessage{
				Role:             "assistant",
				Content:          json.RawMessage(`"hello"`),
				ReasoningContent: "reasoning text",
				ToolCalls: []ChatToolCall{{
					ID:   "call_eq",
					Type: "function",
					Function: ChatFunctionCall{
						Name:      "search",
						Arguments: `{"q":"test"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: &ChatUsage{
			PromptTokens:        50,
			CompletionTokens:    10,
			TotalTokens:         60,
			PromptTokensDetails: &ChatTokenDetails{CachedTokens: 5},
		},
	}

	// 直接转换结果。
	direct := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")

	// 经过 Responses 的两步转换结果。
	responsesResp := ChatCompletionsResponseToResponses(testRuntime(), resp, "claude-sonnet-4-20250514", nil, nil, false, nil)
	double := ResponsesToAnthropic(responsesResp, "claude-sonnet-4-20250514")

	// 比较转换后的模型、内容和用量字段。
	require.Equal(t, AnthropicStopReasonString(direct.StopReason), AnthropicStopReasonString(double.StopReason))
	require.Equal(t, direct.Model, double.Model)
	require.Len(t, direct.Content, len(double.Content))
	for i := range direct.Content {
		require.Equal(t, double.Content[i].Type, direct.Content[i].Type, "block %d type mismatch", i)
		require.Equal(t, double.Content[i].Text, direct.Content[i].Text, "block %d text mismatch", i)
		require.Equal(t, double.Content[i].Thinking, direct.Content[i].Thinking, "block %d thinking mismatch", i)
		require.Equal(t, double.Content[i].Name, direct.Content[i].Name, "block %d name mismatch", i)
		require.Equal(t, double.Content[i].ID, direct.Content[i].ID, "block %d id mismatch", i)
		require.Equal(t, string(double.Content[i].Input), string(direct.Content[i].Input), "block %d input mismatch", i)
	}
	require.Equal(t, double.Usage.InputTokens, direct.Usage.InputTokens)
	require.Equal(t, double.Usage.OutputTokens, direct.Usage.OutputTokens)
	require.Equal(t, double.Usage.CacheReadInputTokens, direct.Usage.CacheReadInputTokens)
	require.Equal(t, double.Usage.CacheCreationInputTokens, direct.Usage.CacheCreationInputTokens)
}

func TestChatCompletionsToAnthropicStreamState_ToolCallNameArrivesLate(t *testing.T) {
	// 部分上游会先发送 tool_call index 与参数，再发送工具名。
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_late"}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"late_tool"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	})

	// 延迟发布的 tool_use block 使用上游返回的名称。
	var toolName string
	for _, e := range events {
		if e.Type == "content_block_start" && e.ContentBlock != nil && e.ContentBlock.Type == "tool_use" {
			toolName = e.ContentBlock.Name
		}
	}
	require.Equal(t, "late_tool", toolName)
}

func TestChatCompletionsToAnthropicStreamState_ToolCallIDAndNameArriveLate(t *testing.T) {
	// 参数、名称和 ID 可以分别到达，最终使用上游返回的 ID。
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"Read","arguments":"\"README.md\","}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_late","function":{"arguments":"\"pages\":\"\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	})

	tools := assembleToolUseBlocks(events)
	require.Len(t, tools, 1)
	require.Equal(t, "call_late", tools[0].ID)
	require.Equal(t, "Read", tools[0].Name)
	require.JSONEq(t, `{"path":"README.md"}`, tools[0].Input)
}

func TestChatCompletionsToAnthropicStreamState_ToolCallArgsArriveBeforeName(t *testing.T) {
	// 工具名之前到达的参数片段在 content_block_start 时输出，客户端据此重建完整 JSON。
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_early","function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"SF\""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"get_weather","arguments":"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	})

	tools := assembleToolUseBlocks(events)
	require.Len(t, tools, 1)
	require.Equal(t, "call_early", tools[0].ID)
	require.Equal(t, "get_weather", tools[0].Name)
	require.JSONEq(t, `{"city":"SF"}`, tools[0].Input)

	// 任何 delta 都不能早于对应 block 的 content_block_start。
	started := map[int]bool{}
	for _, e := range events {
		switch e.Type {
		case "content_block_start":
			started[*e.Index] = true
		case "content_block_delta":
			require.True(t, started[*e.Index], "delta before content_block_start on index %d", *e.Index)
		}
	}
}

func TestChatCompletionsToAnthropicStreamState_ToolCallNameNeverArrives(t *testing.T) {
	// 工具名始终缺失时在 finalize 以空名称发布，参数不能丢失，stop_reason 仍为 tool_use。
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_anon","function":{"arguments":"{\"a\":1}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	})

	tools := assembleToolUseBlocks(events)
	require.Len(t, tools, 1)
	require.Equal(t, "call_anon", tools[0].ID)
	require.Equal(t, "", tools[0].Name)
	require.JSONEq(t, `{"a":1}`, tools[0].Input)

	// block 的开始和结束事件配对，结束事件位于 message_stop 前。
	types := anthropicEventTypes(events)
	require.Equal(t, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}, types)
}

func TestChatCompletionsToAnthropicStreamState_EmptyArgsToolEmitsPlaceholderDelta(t *testing.T) {
	// 参数始终缺失的工具在停止前补 `input_json_delta {}`，兼容只从 delta 组装输入的客户端。
	events := collectAnthropicStreamEvents(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_empty","type":"function","function":{"name":"noop","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	})

	tools := assembleToolUseBlocks(events)
	require.Len(t, tools, 1)
	require.Equal(t, "noop", tools[0].Name)
	require.JSONEq(t, `{}`, tools[0].Input)
}

func TestDirectBridge_NonStreamingMatchesDoubleConversion_CacheWriteTokens(t *testing.T) {
	// cache_write_tokens 与 cache_creation_tokens 是同一值的替代字段，同时存在时优先 write。
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-cache",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Message:      ChatMessage{Role: "assistant", Content: json.RawMessage(`"hi"`)},
			FinishReason: "stop",
		}},
		Usage: &ChatUsage{
			PromptTokens:     100,
			CompletionTokens: 10,
			TotalTokens:      110,
			PromptTokensDetails: &ChatTokenDetails{
				CachedTokens:        20,
				CacheCreationTokens: 7,
				CacheWriteTokens:    9,
			},
		},
	}

	direct := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")

	responsesResp := ChatCompletionsResponseToResponses(testRuntime(), resp, "claude-sonnet-4-20250514", nil, nil, false, nil)
	double := ResponsesToAnthropic(responsesResp, "claude-sonnet-4-20250514")

	require.Equal(t, double.Usage.InputTokens, direct.Usage.InputTokens)
	require.Equal(t, double.Usage.OutputTokens, direct.Usage.OutputTokens)
	require.Equal(t, double.Usage.CacheReadInputTokens, direct.Usage.CacheReadInputTokens)
	require.Equal(t, double.Usage.CacheCreationInputTokens, direct.Usage.CacheCreationInputTokens)
	require.Equal(t, 9, direct.Usage.CacheCreationInputTokens)
}

func TestChatCompletionsResponseToAnthropic_GeneratesIDWhenMissing(t *testing.T) {
	resp := &ChatCompletionsResponse{
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Message:      ChatMessage{Role: "assistant", Content: json.RawMessage(`"hi"`)},
			FinishReason: "stop",
		}},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	require.NotEmpty(t, out.ID, "response id must be generated when the upstream omits one")
}

func TestDirectBridge_NonStreamingMatchesDoubleConversion_EmptyChoices(t *testing.T) {
	// 上游返回 200 且 choices 为空时，结果使用 end_turn。
	resp := &ChatCompletionsResponse{ID: "chatcmpl-empty", Model: "deepseek-v4-pro"}

	direct := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")

	responsesResp := ChatCompletionsResponseToResponses(testRuntime(), resp, "claude-sonnet-4-20250514", nil, nil, false, nil)
	double := ResponsesToAnthropic(responsesResp, "claude-sonnet-4-20250514")

	require.Equal(t, AnthropicStopReasonString(double.StopReason), AnthropicStopReasonString(direct.StopReason))
	require.Equal(t, "end_turn", AnthropicStopReasonString(direct.StopReason))
}

func TestChatCompletionsResponseToAnthropic_ContentFilterWithToolUse(t *testing.T) {
	// content_filter 与未知 finish reason 都从 blocks 推导 stop_reason。
	resp := &ChatCompletionsResponse{
		ID:    "chatcmpl-cf",
		Model: "deepseek-v4-pro",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role:    "assistant",
				Content: json.RawMessage(`"partial"`),
				ToolCalls: []ChatToolCall{{
					ID:       "call_cf",
					Type:     "function",
					Function: ChatFunctionCall{Name: "search", Arguments: `{"q":"x"}`},
				}},
			},
			FinishReason: "content_filter",
		}},
	}

	out := ChatCompletionsResponseToAnthropic(testRuntime(), resp, "claude-sonnet-4-20250514")
	require.Equal(t, "tool_use", AnthropicStopReasonString(out.StopReason))
}

// TestAnthropicChatBridge_ReasoningSurvivesOutboundInboundRoundTrip 检查 reasoning_content 转成 Anthropic thinking 块后，可以在下一轮请求中还原。
func TestAnthropicChatBridge_ReasoningSurvivesOutboundInboundRoundTrip(t *testing.T) {
	upstream := ChatMessage{
		Role:             "assistant",
		ReasoningContent: "step 1: need the weather tool",
		Content:          json.RawMessage(`"checking"`),
		ToolCalls: []ChatToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: ChatFunctionCall{Name: "get_weather", Arguments: `{"city":"SF"}`},
		}},
	}

	// 出站：Chat 响应 → Anthropic content blocks
	blocks := chatMessageToAnthropicBlocks(upstream)
	require.Equal(t, "thinking", blocks[0].Type)
	require.Equal(t, upstream.ReasoningContent, blocks[0].Thinking)

	// 客户端下一轮把同一组 blocks 原样回传
	raw, err := json.Marshal(blocks)
	require.NoError(t, err)

	// 入站：Anthropic content blocks → Chat 请求
	back, err := anthropicAssistantToChatMessages(raw)
	require.NoError(t, err)
	require.Len(t, back, 1)
	require.Equal(t, upstream.ReasoningContent, back[0].ReasoningContent,
		"出站生成的 thinking 必须能原样还原回 reasoning_content")
	require.Len(t, back[0].ToolCalls, 1)
}

func TestAnthropicThinkingToReasoningContent(t *testing.T) {
	blocksOf := func(t *testing.T, raw string) []AnthropicContentBlock {
		t.Helper()
		var blocks []AnthropicContentBlock
		require.NoError(t, json.Unmarshal([]byte(raw), &blocks))
		return blocks
	}

	cases := []struct {
		name         string
		raw          string
		hasToolCalls bool
		want         string
	}{
		{
			name:         "single_thinking_block",
			raw:          `[{"type":"thinking","thinking":"a"}]`,
			hasToolCalls: true,
			want:         "a",
		},
		{
			// 多个 thinking 块用 "\n" 连接，与 extractResponsesReasoningText 一致。
			name:         "multiple_blocks_join_with_newline",
			raw:          `[{"type":"thinking","thinking":"a"},{"type":"text","text":"x"},{"type":"thinking","thinking":"b"}]`,
			hasToolCalls: true,
			want:         "a\nb",
		},
		{
			// redacted_thinking 没有明文可回传。
			name:         "redacted_thinking_has_no_plaintext",
			raw:          `[{"type":"redacted_thinking","signature":"abc"}]`,
			hasToolCalls: true,
			want:         "",
		},
		{
			// 只带 signature 的 thinking 占位块(xAI/Codex 密文回放形态)同样无明文。
			name:         "signature_only_thinking",
			raw:          `[{"type":"thinking","thinking":"","signature":"gAAAAxxx"}]`,
			hasToolCalls: true,
			want:         "",
		},
		{
			name:         "no_tool_calls_returns_empty",
			raw:          `[{"type":"thinking","thinking":"a"}]`,
			hasToolCalls: false,
			want:         "",
		},
		{
			name:         "no_thinking_blocks",
			raw:          `[{"type":"text","text":"x"}]`,
			hasToolCalls: true,
			want:         "",
		},
		{
			name:         "empty_blocks",
			raw:          `[]`,
			hasToolCalls: true,
			want:         "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want,
				anthropicThinkingToReasoningContent(blocksOf(t, tc.raw), tc.hasToolCalls))
		})
	}
}

// TestAnthropicAssistantToChatMessages_PlainStringContentUnaffected 检查字符串形式的 assistant content 直接返回文本消息。
func TestAnthropicAssistantToChatMessages_PlainStringContentUnaffected(t *testing.T) {
	msgs, err := anthropicAssistantToChatMessages(json.RawMessage(`"just text"`))
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Empty(t, msgs[0].ReasoningContent)
	require.Equal(t, `"just text"`, string(msgs[0].Content))
}

// collectAnthropicStreamEvents 把 CC chunks 输入直连桥并追加收尾事件。
func collectAnthropicStreamEvents(t *testing.T, chunks []string) []AnthropicStreamEvent {
	t.Helper()
	state := NewChatCompletionsToAnthropicStreamState(testRuntime(), "deepseek-v4-pro")
	var events []AnthropicStreamEvent
	for _, payload := range chunks {
		var chunk ChatCompletionsChunk
		require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
		events = append(events, ChatCompletionsChunkToAnthropicEvents(testRuntime(), &chunk, state)...)
	}
	events = append(events, FinalizeChatCompletionsAnthropicStream(testRuntime(), state)...)
	return events
}

// anthropicEventTypes 提取事件类型序列，便于断言流生命周期。
func anthropicEventTypes(events []AnthropicStreamEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

// assembleToolUseBlocks 从 start 读取 ID 和名称并拼接参数 delta，模拟 Anthropic 客户端处理。
func assembleToolUseBlocks(events []AnthropicStreamEvent) []assembledToolUse {
	blockByIdx := map[int]int{} // Anthropic block index 到输出位置的映射。
	var out []assembledToolUse
	for _, e := range events {
		switch e.Type {
		case "content_block_start":
			if e.ContentBlock != nil && e.ContentBlock.Type == "tool_use" && e.Index != nil {
				blockByIdx[*e.Index] = len(out)
				out = append(out, assembledToolUse{ID: e.ContentBlock.ID, Name: e.ContentBlock.Name})
			}
		case "content_block_delta":
			if e.Delta != nil && e.Delta.Type == "input_json_delta" && e.Index != nil {
				if pos, ok := blockByIdx[*e.Index]; ok {
					out[pos].Input += e.Delta.PartialJSON
				}
			}
		}
	}
	return out
}
