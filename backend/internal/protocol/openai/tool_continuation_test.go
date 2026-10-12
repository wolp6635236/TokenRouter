package openai

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	benchmarkToolContinuationBoolSink bool

	// benchmarkIntSink 记录工具续链校验的执行次数。
	benchmarkIntSink int
)

func BenchmarkToolContinuationValidationLegacy(b *testing.B) {
	reqBody := benchmarkToolContinuationRequestBody()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkToolContinuationBoolSink = legacyValidateFunctionCallOutputContext(reqBody)
	}
}

func BenchmarkToolContinuationValidationOptimized(b *testing.B) {
	reqBody := benchmarkToolContinuationRequestBody()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkToolContinuationBoolSink = optimizedValidateFunctionCallOutputContext(reqBody)
	}
}

func TestNeedsToolContinuationSignals(t *testing.T) {
	// 覆盖触发续接的各类信号。
	cases := []struct {
		name string
		body map[string]any
		want bool
	}{
		{name: "nil", body: nil, want: false},
		{name: "previous_response_id", body: map[string]any{"previous_response_id": "resp_1"}, want: true},
		{name: "previous_response_id_blank", body: map[string]any{"previous_response_id": "  "}, want: false},
		{name: "function_call_output", body: map[string]any{"input": []any{map[string]any{"type": "function_call_output"}}}, want: true},
		{name: "tool_search_output", body: map[string]any{"input": []any{map[string]any{"type": "tool_search_output"}}}, want: true},
		{name: "custom_tool_call_output", body: map[string]any{"input": []any{map[string]any{"type": "custom_tool_call_output"}}}, want: true},
		{name: "mcp_tool_call_output", body: map[string]any{"input": []any{map[string]any{"type": "mcp_tool_call_output"}}}, want: true},
		{name: "item_reference", body: map[string]any{"input": []any{map[string]any{"type": "item_reference"}}}, want: true},
		{name: "tools", body: map[string]any{"tools": []any{map[string]any{"type": "function"}}}, want: true},
		{name: "tools_empty", body: map[string]any{"tools": []any{}}, want: false},
		{name: "tools_invalid", body: map[string]any{"tools": "bad"}, want: false},
		{name: "tool_choice", body: map[string]any{"tool_choice": "auto"}, want: true},
		{name: "tool_choice_object", body: map[string]any{"tool_choice": map[string]any{"type": "function"}}, want: true},
		{name: "tool_choice_empty_object", body: map[string]any{"tool_choice": map[string]any{}}, want: false},
		{name: "none", body: map[string]any{"input": []any{map[string]any{"type": "text", "text": "hi"}}}, want: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NeedsToolContinuation(tt.body))
		})
	}
}

func TestHasFunctionCallOutput(t *testing.T) {
	// Codex 工具输出参与续接判断，WS 据此保留 previous_response_id。
	require.False(t, HasFunctionCallOutput(nil))
	for _, typ := range []string{
		"function_call_output",
		"tool_search_output",
		"custom_tool_call_output",
		"mcp_tool_call_output",
	} {
		require.True(t, HasFunctionCallOutput(map[string]any{
			"input": []any{map[string]any{"type": typ}},
		}), typ)
	}
	require.False(t, HasFunctionCallOutput(map[string]any{
		"input": "text",
	}))
}

func TestHasToolCallContext(t *testing.T) {
	// 工具调用上下文必须包含 call_id，才能作为可关联上下文。
	require.False(t, AnalyzeToolContinuationSignals(nil).HasToolCallContext)
	for _, typ := range []string{
		"tool_call",
		"function_call",
		"local_shell_call",
		"tool_search_call",
		"custom_tool_call",
		"mcp_tool_call",
	} {
		require.True(t, AnalyzeToolContinuationSignals(map[string]any{
			"input": []any{map[string]any{"type": typ, "call_id": "call_1"}},
		}).HasToolCallContext, typ)
	}
	require.False(t, AnalyzeToolContinuationSignals(map[string]any{
		"input": []any{map[string]any{"type": "tool_call"}},
	}).HasToolCallContext)
}

func TestFunctionCallOutputCallIDs(t *testing.T) {
	// 仅提取工具输出的非空 call_id，去重后返回。
	require.Empty(t, AnalyzeToolContinuationSignals(nil).FunctionCallOutputCallIDs)
	callIDs := AnalyzeToolContinuationSignals(map[string]any{
		"input": []any{
			map[string]any{"type": "function_call_output", "call_id": "call_1"},
			map[string]any{"type": "tool_search_output", "call_id": "call_search"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "call_custom"},
			map[string]any{"type": "mcp_tool_call_output", "call_id": "call_mcp"},
			map[string]any{"type": "function_call_output", "call_id": ""},
			map[string]any{"type": "function_call_output", "call_id": "call_1"},
		},
	}).FunctionCallOutputCallIDs
	require.ElementsMatch(t, []string{"call_1", "call_search", "call_custom", "call_mcp"}, callIDs)
}

func TestHasFunctionCallOutputMissingCallID(t *testing.T) {
	require.False(t, AnalyzeToolContinuationSignals(nil).HasFunctionCallOutputMissingCallID)
	require.True(t, AnalyzeToolContinuationSignals(map[string]any{
		"input": []any{map[string]any{"type": "function_call_output"}},
	}).HasFunctionCallOutputMissingCallID)
	require.True(t, AnalyzeToolContinuationSignals(map[string]any{
		"input": []any{map[string]any{"type": "tool_search_output"}},
	}).HasFunctionCallOutputMissingCallID)
	require.False(t, AnalyzeToolContinuationSignals(map[string]any{
		"input": []any{map[string]any{"type": "tool_search_output", "call_id": "call_1"}},
	}).HasFunctionCallOutputMissingCallID)
}

func TestHasItemReferenceForCallIDs(t *testing.T) {
	// item_reference 需要覆盖所有 call_id 才视为可关联上下文。
	require.False(t, HasItemReferenceForCallIDs(nil, []string{"call_1"}))
	require.False(t, HasItemReferenceForCallIDs(map[string]any{}, []string{"call_1"}))
	req := map[string]any{
		"input": []any{
			map[string]any{"type": "item_reference", "id": "call_1"},
			map[string]any{"type": "item_reference", "id": "call_2"},
		},
	}
	require.True(t, HasItemReferenceForCallIDs(req, []string{"call_1"}))
	require.True(t, HasItemReferenceForCallIDs(req, []string{"call_1", "call_2"}))
	require.False(t, HasItemReferenceForCallIDs(req, []string{"call_1", "call_3"}))
}

func TestValidateFunctionCallOutputContextBytesMatchesMapValidation(t *testing.T) {
	// handler 的 raw JSON 预校验与 service 的 map 校验应得到相同结果。
	cases := []struct {
		name string
		body map[string]any
	}{
		{
			name: "no_input",
			body: map[string]any{"model": "gpt-5.4"},
		},
		{
			name: "missing_call_id",
			body: map[string]any{"input": []any{map[string]any{"type": "function_call_output"}}},
		},
		{
			name: "call_id_without_reference",
			body: map[string]any{"input": []any{map[string]any{"type": "function_call_output", "call_id": "call_1"}}},
		},
		{
			name: "matching_reference",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call_output", "call_id": "call_1"},
				map[string]any{"type": "item_reference", "id": "call_1"},
			}},
		},
		{
			name: "partial_reference",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call_output", "call_id": "call_1"},
				map[string]any{"type": "tool_search_output", "call_id": "call_2"},
				map[string]any{"type": "item_reference", "id": "call_1"},
			}},
		},
		{
			name: "tool_context",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call_output", "call_id": "call_1"},
				map[string]any{"type": "function_call", "call_id": "call_1"},
			}},
		},
		{
			name: "all_codex_tool_outputs",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call_output", "call_id": "call_function"},
				map[string]any{"type": "tool_search_output", "call_id": "call_search"},
				map[string]any{"type": "custom_tool_call_output", "call_id": "call_custom"},
				map[string]any{"type": "mcp_tool_call_output", "call_id": "call_mcp"},
				map[string]any{"type": "item_reference", "id": "call_function"},
				map[string]any{"type": "item_reference", "id": "call_search"},
				map[string]any{"type": "item_reference", "id": "call_custom"},
				map[string]any{"type": "item_reference", "id": "call_mcp"},
			}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			require.Equal(t, ValidateFunctionCallOutputContext(tt.body), ValidateFunctionCallOutputContextBytes(bodyBytes))
		})
	}
}

func TestAnalyzeToolCallOutputContextCoverageBytes(t *testing.T) {
	cases := []struct {
		name         string
		body         map[string]any
		hasOutput    bool
		coversAllIDs bool
	}{
		{
			name:         "no_input",
			body:         map[string]any{"model": "gpt-5.1"},
			hasOutput:    false,
			coversAllIDs: false,
		},
		{
			name: "no_tool_output",
			body: map[string]any{"input": []any{
				map[string]any{"type": "message", "content": "hi"},
			}},
			hasOutput:    false,
			coversAllIDs: false,
		},
		{
			name: "object_tool_output_requires_context_replay",
			body: map[string]any{"input": map[string]any{
				"type": "custom_tool_call_output", "call_id": "call_a",
			}},
			hasOutput:    true,
			coversAllIDs: false,
		},
		{
			name: "all_outputs_covered_by_context",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call", "call_id": "call_a"},
				map[string]any{"type": "function_call_output", "call_id": "call_a"},
			}},
			hasOutput:    true,
			coversAllIDs: true,
		},
		{
			name: "all_outputs_covered_by_item_reference",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call_output", "call_id": "call_a"},
				map[string]any{"type": "item_reference", "id": "call_a"},
			}},
			hasOutput:    true,
			coversAllIDs: true,
		},
		{
			// input 含一个调用的上下文，另一个输出的 call_id 仍需通过 previous_response_id 解析。
			// 这类请求需要保留上游会话 ID。
			name: "partial_coverage_not_movable",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call", "call_id": "call_a"},
				map[string]any{"type": "function_call_output", "call_id": "call_a"},
				map[string]any{"type": "function_call_output", "call_id": "call_b"},
			}},
			hasOutput:    true,
			coversAllIDs: false,
		},
		{
			name: "unrelated_context_does_not_cover",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call", "call_id": "call_x"},
				map[string]any{"type": "function_call_output", "call_id": "call_b"},
			}},
			hasOutput:    true,
			coversAllIDs: false,
		},
		{
			name: "output_missing_call_id_not_movable",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call", "call_id": "call_a"},
				map[string]any{"type": "function_call_output"},
				map[string]any{"type": "function_call_output", "call_id": "call_a"},
			}},
			hasOutput:    true,
			coversAllIDs: false,
		},
		{
			name: "mixed_context_and_reference_cover_all",
			body: map[string]any{"input": []any{
				map[string]any{"type": "function_call", "call_id": "call_a"},
				map[string]any{"type": "function_call_output", "call_id": "call_a"},
				map[string]any{"type": "function_call_output", "call_id": "call_b"},
				map[string]any{"type": "item_reference", "id": "call_b"},
			}},
			hasOutput:    true,
			coversAllIDs: true,
		},
		{
			name: "all_codex_output_types_covered",
			body: map[string]any{"input": []any{
				map[string]any{"type": "tool_search_output", "call_id": "call_s"},
				map[string]any{"type": "tool_search_call", "call_id": "call_s"},
				map[string]any{"type": "mcp_tool_call_output", "call_id": "call_m"},
				map[string]any{"type": "mcp_tool_call", "call_id": "call_m"},
			}},
			hasOutput:    true,
			coversAllIDs: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			coverage := AnalyzeToolCallOutputContextCoverageBytes(bodyBytes)
			require.Equal(t, tt.hasOutput, coverage.HasFunctionCallOutput, "HasFunctionCallOutput")
			require.Equal(t, tt.coversAllIDs, coverage.ContextCoversAllCallIDs, "ContextCoversAllCallIDs")
		})
	}
}

func benchmarkToolContinuationRequestBody() map[string]any {
	input := make([]any, 0, 64)
	for range 24 {
		input = append(input, map[string]any{
			"type": "text",
			"text": "benchmark text",
		})
	}
	for i := range 10 {
		callID := "call_" + strconv.Itoa(i)
		input = append(input, map[string]any{
			"type":    "tool_call",
			"call_id": callID,
		})
		input = append(input, map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
		})
		input = append(input, map[string]any{
			"type": "item_reference",
			"id":   callID,
		})
	}
	return map[string]any{
		"model": "gpt-5.3-codex",
		"input": input,
	}
}

func legacyValidateFunctionCallOutputContext(reqBody map[string]any) bool {
	if !legacyHasFunctionCallOutput(reqBody) {
		return true
	}
	previousResponseID, _ := reqBody["previous_response_id"].(string)
	if strings.TrimSpace(previousResponseID) != "" {
		return true
	}
	if legacyHasToolCallContext(reqBody) {
		return true
	}
	if legacyHasFunctionCallOutputMissingCallID(reqBody) {
		return false
	}
	callIDs := legacyFunctionCallOutputCallIDs(reqBody)
	return legacyHasItemReferenceForCallIDs(reqBody, callIDs)
}

func optimizedValidateFunctionCallOutputContext(reqBody map[string]any) bool {
	validation := ValidateFunctionCallOutputContext(reqBody)
	if !validation.HasFunctionCallOutput {
		return true
	}
	previousResponseID, _ := reqBody["previous_response_id"].(string)
	if strings.TrimSpace(previousResponseID) != "" {
		return true
	}
	if validation.HasToolCallContext {
		return true
	}
	if validation.HasFunctionCallOutputMissingCallID {
		return false
	}
	return validation.HasItemReferenceForAllCallIDs
}

func legacyHasFunctionCallOutput(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType == "function_call_output" {
			return true
		}
	}
	return false
}

func legacyHasToolCallContext(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "tool_call" && itemType != "function_call" {
			continue
		}
		if callID, ok := itemMap["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			return true
		}
	}
	return false
}

func legacyFunctionCallOutputCallIDs(reqBody map[string]any) []string {
	if reqBody == nil {
		return nil
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return nil
	}
	ids := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "function_call_output" {
			continue
		}
		if callID, ok := itemMap["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			ids[callID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	callIDs := make([]string, 0, len(ids))
	for id := range ids {
		callIDs = append(callIDs, id)
	}
	return callIDs
}

func legacyHasFunctionCallOutputMissingCallID(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "function_call_output" {
			continue
		}
		callID, _ := itemMap["call_id"].(string)
		if strings.TrimSpace(callID) == "" {
			return true
		}
	}
	return false
}

func legacyHasItemReferenceForCallIDs(reqBody map[string]any, callIDs []string) bool {
	if reqBody == nil || len(callIDs) == 0 {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	referenceIDs := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "item_reference" {
			continue
		}
		idValue, _ := itemMap["id"].(string)
		idValue = strings.TrimSpace(idValue)
		if idValue == "" {
			continue
		}
		referenceIDs[idValue] = struct{}{}
	}
	if len(referenceIDs) == 0 {
		return false
	}
	for _, callID := range callIDs {
		if _, ok := referenceIDs[callID]; !ok {
			return false
		}
	}
	return true
}

func BenchmarkOpenAIResponses_LargeInputFunctionCallValidation(b *testing.B) {
	for _, size := range benchmarkBodySizes() {
		b.Run(size.name, func(b *testing.B) {
			body := buildLargeOpenAIResponsesToolContinuationBody(size.bytes)

			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				validation := ValidateFunctionCallOutputContextBytes(body)
				if !validation.HasFunctionCallOutput || !validation.HasItemReferenceForAllCallIDs {
					b.Fatalf("工具续链校验结果异常: %+v", validation)
				}
				benchmarkIntSink++
			}
		})
	}
}

// benchmarkBodySizes 指定工具调用上下文校验基准的目标输入大小。
func benchmarkBodySizes() []struct {
	name  string
	bytes int
} {
	return []struct {
		name  string
		bytes int
	}{
		{name: "4MB", bytes: 4 << 20},
		{name: "8MB", bytes: 8 << 20},
		{name: "16MB", bytes: 16 << 20},
		{name: "32MB", bytes: 32 << 20},
	}
}

// buildLargeOpenAIResponsesToolContinuationBody 构造同时包含 item_reference 和 function_call_output 的请求。
func buildLargeOpenAIResponsesToolContinuationBody(targetBytes int) []byte {
	var builder strings.Builder
	builder.Grow(targetBytes + 1024)
	_, _ = builder.WriteString(`{"model":"gpt-5.4","stream":true,"previous_response_id":"resp_benchmark","input":[`)
	for i := 0; builder.Len() < targetBytes; i++ {
		if i > 0 {
			_ = builder.WriteByte(',')
		}
		callID := "call_" + strconv.Itoa(i)
		_, _ = builder.WriteString(`{"type":"item_reference","id":"`)
		_, _ = builder.WriteString(callID)
		_, _ = builder.WriteString(`"},{"type":"function_call_output","call_id":"`)
		_, _ = builder.WriteString(callID)
		_, _ = builder.WriteString(`","output":"`)
		_, _ = builder.WriteString(strings.Repeat("tool output payload ", 48))
		_, _ = builder.WriteString(strconv.Itoa(i))
		_, _ = builder.WriteString(`"}`)
	}
	_, _ = builder.WriteString(`]}`)
	return []byte(builder.String())
}
