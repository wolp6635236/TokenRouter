package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anthropicInboundBlockTypes 是 Anthropic Messages 接受的 content block 类型集合。
// 输出集合外的类型会触发 400 Request body format invalid（issue #5329）。
var anthropicInboundBlockTypes = map[string]bool{
	"text":              true,
	"image":             true,
	"document":          true,
	"tool_use":          true,
	"tool_result":       true,
	"thinking":          true,
	"redacted_thinking": true,
}

func TestResponsesToAnthropicRequest_ToolChoiceFunctionName(t *testing.T) {
	req := &ResponsesRequest{
		Model:      "gpt-5.2",
		Input:      json.RawMessage(`[{"role":"user","content":"Hello"}]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"get_weather"}`),
	}

	resp, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)

	var tc map[string]string
	require.NoError(t, json.Unmarshal(resp.ToolChoice, &tc))
	assert.Equal(t, "tool", tc["type"])
	assert.Equal(t, "get_weather", tc["name"])
}

func TestResponsesToAnthropicRequest_ToolChoiceLegacyFunctionName(t *testing.T) {
	req := &ResponsesRequest{
		Model:      "gpt-5.2",
		Input:      json.RawMessage(`[{"role":"user","content":"Hello"}]`),
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"get_weather"}}`),
	}

	resp, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)

	var tc map[string]string
	require.NoError(t, json.Unmarshal(resp.ToolChoice, &tc))
	assert.Equal(t, "tool", tc["type"])
	assert.Equal(t, "get_weather", tc["name"])
}

func TestResponsesToAnthropicRequest_RejectsUltraReasoningEffort(t *testing.T) {
	req := &ResponsesRequest{
		Model:     "gpt-5.6-sol",
		Input:     json.RawMessage(`"Hello"`),
		Reasoning: &ResponsesReasoning{Effort: "ultra"},
	}

	resp, err := ResponsesToAnthropicRequest(req)
	require.ErrorContains(t, err, "not supported")
	require.Nil(t, resp)
}

func TestOpenAIPriorityConvertsToClaudeFast(t *testing.T) {
	input, err := json.Marshal("hello")
	require.NoError(t, err)
	converted, err := ResponsesToAnthropicRequest(&ResponsesRequest{
		Model:       "claude-opus-4.8",
		Input:       input,
		ServiceTier: "priority",
	})
	require.NoError(t, err)
	require.Equal(t, "fast", converted.Speed)
}

func TestResponsesToAnthropicRequest_Instructions(t *testing.T) {
	t.Run("instructions_becomes_system", func(t *testing.T) {
		req := &ResponsesRequest{
			Model:        "claude-sonnet-4-20250514",
			Instructions: "You are a helpful assistant.",
			Input:        json.RawMessage(`[{"role":"user","content":"hello"}]`),
		}

		result, err := ResponsesToAnthropicRequest(req)
		require.NoError(t, err)

		var system string
		require.NoError(t, json.Unmarshal(result.System, &system))
		assert.Equal(t, "You are a helpful assistant.", system)
		assert.NotEmpty(t, result.Messages)
	})

	t.Run("empty_instructions_no_system", func(t *testing.T) {
		req := &ResponsesRequest{
			Model: "claude-sonnet-4-20250514",
			Input: json.RawMessage(`[{"role":"user","content":"hello"}]`),
		}

		result, err := ResponsesToAnthropicRequest(req)
		require.NoError(t, err)
		assert.Nil(t, result.System)
	})

	t.Run("instructions_and_system_item_concatenated", func(t *testing.T) {
		req := &ResponsesRequest{
			Model:        "claude-sonnet-4-20250514",
			Instructions: "Top-level instruction.",
			Input: json.RawMessage(`[
				{"role":"system","content":"Input-level system prompt."},
				{"role":"user","content":"hello"}
			]`),
		}

		result, err := ResponsesToAnthropicRequest(req)
		require.NoError(t, err)

		var system string
		require.NoError(t, json.Unmarshal(result.System, &system))
		assert.Contains(t, system, "Top-level instruction.")
		assert.Contains(t, system, "Input-level system prompt.")
	})

	t.Run("instructions_with_string_input", func(t *testing.T) {
		req := &ResponsesRequest{
			Model:        "claude-sonnet-4-20250514",
			Instructions: "Be concise.",
			Input:        json.RawMessage(`"What is Go?"`),
		}

		result, err := ResponsesToAnthropicRequest(req)
		require.NoError(t, err)

		var system string
		require.NoError(t, json.Unmarshal(result.System, &system))
		assert.Equal(t, "Be concise.", system)
		require.Len(t, result.Messages, 1)
		assert.Equal(t, "user", result.Messages[0].Role)
	})
}

func TestConvertResponsesInputToAnthropic_DeveloperRole(t *testing.T) {
	t.Run("developer_becomes_system", func(t *testing.T) {
		input := `[
			{"role":"developer","content":[{"type":"input_text","text":"You are a code reviewer."}]},
			{"role":"user","content":"review this code"}
		]`

		system, messages, err := convertResponsesInputToAnthropic("", json.RawMessage(input))
		require.NoError(t, err)

		var systemText string
		require.NoError(t, json.Unmarshal(system, &systemText))
		assert.Equal(t, "You are a code reviewer.", systemText)

		require.Len(t, messages, 1)
		assert.Equal(t, "user", messages[0].Role)
	})

	t.Run("developer_does_not_become_user", func(t *testing.T) {
		input := `[
			{"role":"developer","content":[{"type":"input_text","text":"System prompt."}]},
			{"role":"user","content":"hi"}
		]`

		_, messages, err := convertResponsesInputToAnthropic("", json.RawMessage(input))
		require.NoError(t, err)

		for _, m := range messages {
			if m.Role == "user" {
				var s string
				if json.Unmarshal(m.Content, &s) == nil {
					assert.NotContains(t, s, "System prompt.")
				}
			}
		}
	})

	t.Run("instructions_and_developer_concatenated_in_order", func(t *testing.T) {
		input := `[
			{"role":"developer","content":"Extra context."},
			{"role":"user","content":"hello"}
		]`

		system, _, err := convertResponsesInputToAnthropic("Main instruction.", json.RawMessage(input))
		require.NoError(t, err)

		var systemText string
		require.NoError(t, json.Unmarshal(system, &systemText))
		assert.Equal(t, "Main instruction.\n\nExtra context.", systemText)
	})
}

// TestResponsesToAnthropic_ReasoningItemWithContentIsDropped 检查带 content 的 reasoning 回放项被丢弃（issue #5329）。
// Anthropic 请求不接受 reasoning_text 块。
func TestResponsesToAnthropic_ReasoningItemWithContentIsDropped(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"run a shell command"}]},
		{"type":"reasoning","id":"rs_1","summary":[],"content":[{"type":"reasoning_text","text":"let me think"}]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Len(t, messages, 1)
	require.NotContains(t, string(messages[0].Content), "reasoning_text")
	require.NotContains(t, string(messages[0].Content), "let me think")
}

// TestResponsesToAnthropic_ReasoningItemSummaryOnlyStillDropped 检查仅含 summary 和 encrypted_content 的 reasoning item 被丢弃。
func TestResponsesToAnthropic_ReasoningItemSummaryOnlyStillDropped(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"s"}],"encrypted_content":"gAAAA"}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Len(t, messages, 1)
	require.NotContains(t, string(messages[0].Content), "gAAAA")
}

// TestResponsesToAnthropic_UnknownItemTypeContentIsSanitized 检查未知 item type 中的内容按 Anthropic 支持的分片类型过滤。
func TestResponsesToAnthropic_UnknownItemTypeContentIsSanitized(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"web_search_call","id":"ws_1","content":[{"type":"web_search_result","text":"payload"}]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Empty(t, messages, "整条内容都无法映射时不应发出消息")
}

// TestResponsesToAnthropic_UnknownItemTypeKeepsRecognizableText 检查未知 item type 中的可识别文本被保留。
func TestResponsesToAnthropic_UnknownItemTypeKeepsRecognizableText(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"some_future_item","content":[
			{"type":"input_text","text":"keep me"},
			{"type":"reasoning_text","text":"drop me"}
		]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Len(t, messages, 1)
	require.Contains(t, string(messages[0].Content), "keep me")
	require.NotContains(t, string(messages[0].Content), "drop me")
}

// TestResponsesToAnthropic_UserMessageWithOnlyUnknownPartsIsDropped 检查仅含未知分片的 user 消息被丢弃，Anthropic 拒收空内容消息。
func TestResponsesToAnthropic_UserMessageWithOnlyUnknownPartsIsDropped(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"message","role":"user","content":[{"type":"input_file","file_id":"file_1"}]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Empty(t, messages)
}

// TestResponsesToAnthropic_AssistantMessageWithOnlyUnknownPartsIsDropped 检查仅含未知分片的 assistant 消息被丢弃，Anthropic 拒收空文本块。
func TestResponsesToAnthropic_AssistantMessageWithOnlyUnknownPartsIsDropped(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"no"}]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].Role)
}

// TestResponsesToAnthropic_BlankTextMessagesAreDropped 验证 user、assistant 和未来 item 中的纯空白文本都必须丢弃；Anthropic 对字符串与
// text block 使用相同的非空白约束。
func TestResponsesToAnthropic_BlankTextMessagesAreDropped(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"user_string", `[{"type":"message","role":"user","content":"   "}]`},
		{"user_block", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"\n\t"}]}]`},
		{"assistant_block", `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"  "}]}]`},
		{"future_item", `[{"type":"future_item","content":[{"type":"input_text","text":"  "}]}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			messages := responsesToAnthropicMessages(t, tc.input)
			requireAnthropicMessagesAreSendable(t, messages)
			require.Empty(t, messages)
		})
	}
}

// TestResponsesToAnthropic_BlankTextBesideImageKeepsImage 检查空白文本被移除后，合法图片仍在输出中。
func TestResponsesToAnthropic_BlankTextBesideImageKeepsImage(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"message","role":"user","content":[
			{"type":"input_text","text":"  "},
			{"type":"input_image","image_url":"data:image/png;base64,YQ=="}
		]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)
	require.Len(t, messages, 1)
	blocks := parseContentBlocks(messages[0].Content)
	require.Len(t, blocks, 1)
	require.Equal(t, "image", blocks[0].Type)
}

// TestResponsesToAnthropic_CodexToolRoundStaysIntactAndSendable 验证完整的 Codex 工具续接回放：tool_use / tool_result 配对必须保持不变，
// 同时整个序列满足可发送不变式。
func TestResponsesToAnthropic_CodexToolRoundStaysIntactAndSendable(t *testing.T) {
	messages := responsesToAnthropicMessages(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"run ls"}]},
		{"type":"reasoning","id":"rs_1","summary":[],"content":[{"type":"reasoning_text","text":"plan"}],"encrypted_content":"gAAAA"},
		{"type":"function_call","id":"fc_1","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
		{"type":"function_call_output","call_id":"call_1","output":"file1"},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
	]`)

	requireAnthropicMessagesAreSendable(t, messages)

	var sawToolUse, sawToolResult bool
	for _, m := range messages {
		for _, b := range parseContentBlocks(m.Content) {
			switch b.Type {
			case "tool_use":
				sawToolUse = true
				require.Equal(t, "call_1", b.ID)
				require.Equal(t, "shell", b.Name)
			case "tool_result":
				sawToolResult = true
				require.Equal(t, "call_1", b.ToolUseID)
			}
		}
	}
	require.True(t, sawToolUse, "function_call 必须转成 tool_use")
	require.True(t, sawToolResult, "function_call_output 必须转成 tool_result")
	require.NotContains(t, string(mustMarshal(t, messages)), "reasoning_text")
	require.NotContains(t, string(mustMarshal(t, messages)), "gAAAA")
}

func TestAnthropicContentIsEmpty(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{``, true},
		{`""`, true},
		{`"   "`, true},
		{`null`, true},
		{`[]`, true},
		{`  []  `, true},
		{`"hi"`, false},
		{`" hi "`, false},
		{`[{"type":"text","text":"hi"}]`, false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, anthropicContentIsEmpty(json.RawMessage(tc.raw)), "raw=%q", tc.raw)
	}
}

func TestAnthropicContentIsOnlyBlankText(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`[{"type":"text","text":""}]`, true},
		{`[{"type":"text","text":"   "}]`, true},
		{`[{"type":"text","text":""},{"type":"text","text":" "}]`, true},
		{`[{"type":"text","text":"hi"}]`, false},
		{`[{"type":"text","text":""},{"type":"image","source":{}}]`, false},
		{`[]`, false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, anthropicContentIsOnlyBlankText(json.RawMessage(tc.raw)), "raw=%q", tc.raw)
	}
}

// TestAnthropicPairing_DeveloperMessageBetween 验证 function_call 和 output 之间插入的 developer/审批消息必须移出 tool_use→tool_result 邻接关系，
// 这是线上触发 “tool_result 必须在前一条消息有对应 tool_use” 400 的典型形态。
func TestAnthropicPairing_DeveloperMessageBetween(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"do it"}]},
		{"type":"function_call","call_id":"call_A","name":"exec","arguments":"{}"},
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"Approved command prefix saved"}]},
		{"type":"function_call_output","call_id":"call_A","output":"ok"}
	]`)
	// assistant tool_use 消息后紧跟对应 tool_result。
	for i, m := range msgs {
		if hasToolUse(parseContentBlocks(m.Content), "call_A") {
			require.Equal(t, "user", msgs[i+1].Role)
			require.True(t, hasToolResult(parseContentBlocks(msgs[i+1].Content), "call_A"))
		}
	}
}

// TestAnthropicPairing_ParallelBothAnswered 验证并行工具调用的两个输出都到达时，应保持为一条 assistant 消息包含两个 tool_use，
// 下一条 user 消息包含两个结果。
func TestAnthropicPairing_ParallelBothAnswered(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"features?"}]},
		{"type":"function_call","call_id":"call_c0","name":"exec","arguments":"{}"},
		{"type":"function_call","call_id":"call_c1","name":"exec","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_c0","output":"log"},
		{"type":"function_call_output","call_id":"call_c1","output":"tags"}
	]`)
	var sawGrouped bool
	for _, m := range msgs {
		blocks := parseContentBlocks(m.Content)
		if hasToolUse(blocks, "call_c0") && hasToolUse(blocks, "call_c1") {
			sawGrouped = true
		}
	}
	require.True(t, sawGrouped, "parallel tool_use blocks should share one assistant message")
}

// TestAnthropicPairing_ParallelOneUnanswered 检查并行调用中缺少输出的调用被丢弃，剩余 tool_use 各自有结果。
func TestAnthropicPairing_ParallelOneUnanswered(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"q"}]},
		{"type":"function_call","call_id":"call_A","name":"exec","arguments":"{}"},
		{"type":"function_call","call_id":"call_B","name":"exec","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_A","output":"oa"}
	]`)
	for _, m := range msgs {
		require.Falsef(t, hasToolUse(parseContentBlocks(m.Content), "call_B"),
			"unanswered tool_use call_B should have been dropped")
	}
}

// TestAnthropicPairing_OrphanToolResultDropped 验证没有对应 tool_use 的孤儿 tool_result 必须丢弃。
func TestAnthropicPairing_OrphanToolResultDropped(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"q"}]},
		{"type":"function_call_output","call_id":"call_ghost","output":"orphan"}
	]`)
	for _, m := range msgs {
		require.Falsef(t, hasToolResult(parseContentBlocks(m.Content), "call_ghost"),
			"orphan tool_result should have been dropped")
	}
}

// TestAnthropicPairing_DanglingCallDropped 验证历史末尾尚无输出的悬空 tool_call 会丢弃只包含该调用的 assistant 消息。
func TestAnthropicPairing_DanglingCallDropped(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"q"}]},
		{"type":"function_call","call_id":"call_A","name":"exec","arguments":"{}"}
	]`)
	for _, m := range msgs {
		require.Falsef(t, hasToolUse(parseContentBlocks(m.Content), "call_A"),
			"dangling tool_use call_A should have been dropped")
	}
}

// TestAnthropicPairing_SingleCall 验证基线：单个已回答调用应正确配对，并保留前后轮次。
func TestAnthropicPairing_SingleCall(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"latest sha?"}]},
		{"type":"function_call","call_id":"call_A","name":"exec","arguments":"{\"cmd\":\"git rev-parse HEAD\"}"},
		{"type":"function_call_output","call_id":"call_A","output":"deadbeef"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"It is deadbeef."}]}
	]`)
	// 顺序应为 user、assistant(tool_use)、user(tool_result)、assistant(text)。
	require.GreaterOrEqual(t, len(msgs), 4)
	require.Equal(t, "user", msgs[0].Role)
	require.True(t, hasToolUse(parseContentBlocks(msgs[1].Content), "call_A"))
	require.True(t, hasToolResult(parseContentBlocks(msgs[2].Content), "call_A"))
}

func TestResponsesToAnthropic_FunctionOutputContentArray(t *testing.T) {
	msgs := convertAnthropic(t, `[
		{"type":"function_call","call_id":"call_A","name":"view_image","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_A","output":[
			{"type":"input_text","text":"image loaded"},
			{"type":"input_image","image_url":"data:image/png;base64,YQ=="}
		]}
	]`)

	require.Len(t, msgs, 2)
	resultBlocks := parseContentBlocks(msgs[1].Content)
	require.Len(t, resultBlocks, 1)
	require.Equal(t, "tool_result", resultBlocks[0].Type)

	var content []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(resultBlocks[0].Content, &content))
	require.Len(t, content, 2)
	require.Equal(t, "text", content[0].Type)
	require.Equal(t, "image loaded", content[0].Text)
	require.Equal(t, "image", content[1].Type)
	require.NotNil(t, content[1].Source)
	require.Equal(t, "image/png", content[1].Source.MediaType)
	require.Equal(t, "YQ==", content[1].Source.Data)
}

func TestResponsesToAnthropic_CustomGrammarToolUsesObjectSchema(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5.2",
		"input": "apply this patch",
		"tools": [{
			"type": "custom",
			"name": "apply_patch",
			"description": "Apply a patch to the working tree",
			"format": {
				"type": "grammar",
				"syntax": "lark",
				"definition": "start: /.+/"
			}
		}]
	}`)

	var req ResponsesRequest
	require.NoError(t, json.Unmarshal(body, &req))

	anthropicReq, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	require.Len(t, anthropicReq.Tools, 1)

	tool := anthropicReq.Tools[0]
	assert.Empty(t, tool.Type)
	assert.Equal(t, "apply_patch", tool.Name)
	assert.Equal(t, "Apply a patch to the working tree", tool.Description)
	requireObjectInputSchema(t, tool.InputSchema)
	assert.JSONEq(t, `{"type":"object","properties":{}}`, string(tool.InputSchema))

	wire, err := json.Marshal(tool)
	require.NoError(t, err)
	assert.NotContains(t, string(wire), `"type":"custom"`)
	assert.NotContains(t, string(wire), `"format"`)
	assert.NotContains(t, string(wire), `"grammar"`)
}

func TestResponsesToAnthropic_CustomToolPreservesSchemaParameters(t *testing.T) {
	tools := convertResponsesToAnthropicTools([]ResponsesTool{{
		Type:        "custom",
		Name:        "edit_file",
		Description: "Edit a file",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"patch":{"type":"string"}},"required":["patch"]}`),
	}})

	require.Len(t, tools, 1)
	assert.Empty(t, tools[0].Type)
	assert.Equal(t, "edit_file", tools[0].Name)

	schema := requireObjectInputSchema(t, tools[0].InputSchema)
	assert.JSONEq(t, `{"patch":{"type":"string"}}`, string(schema["properties"]))
	assert.JSONEq(t, `["patch"]`, string(schema["required"]))
}

func TestResponsesToAnthropic_FunctionToolSchemaUnchanged(t *testing.T) {
	parameters := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)
	tools := convertResponsesToAnthropicTools([]ResponsesTool{{
		Type:        "function",
		Name:        "get_weather",
		Description: "Get weather",
		Parameters:  parameters,
	}})

	require.Len(t, tools, 1)
	assert.Empty(t, tools[0].Type)
	assert.Equal(t, "get_weather", tools[0].Name)
	assert.Equal(t, "Get weather", tools[0].Description)
	assert.JSONEq(t, string(parameters), string(tools[0].InputSchema))
}

func TestResponsesToAnthropic_MixedToolsProduceValidAnthropicTools(t *testing.T) {
	tools := convertResponsesToAnthropicTools([]ResponsesTool{
		{
			Type:       "function",
			Name:       "read_file",
			Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		{
			Type: "custom",
			Name: "apply_patch",
		},
		{
			Type: "web_search",
		},
	})

	require.Len(t, tools, 3)
	assert.Empty(t, tools[0].Type)
	assert.Equal(t, "read_file", tools[0].Name)
	requireObjectInputSchema(t, tools[0].InputSchema)

	assert.Empty(t, tools[1].Type)
	assert.Equal(t, "apply_patch", tools[1].Name)
	assert.JSONEq(t, `{"type":"object","properties":{}}`, string(tools[1].InputSchema))

	assert.Equal(t, "web_search_20250305", tools[2].Type)
	assert.Equal(t, "web_search", tools[2].Name)
	assert.Empty(t, tools[2].InputSchema)
	serverToolWire, err := json.Marshal(tools[2])
	require.NoError(t, err)
	assert.NotContains(t, string(serverToolWire), `"input_schema"`)
}

func TestResponsesToAnthropic_DefaultToolNormalizesInputSchema(t *testing.T) {
	tools := convertResponsesToAnthropicTools([]ResponsesTool{{
		Type: "local_shell",
		Name: "shell",
	}})

	require.Len(t, tools, 1)
	assert.Equal(t, "local_shell", tools[0].Type)
	assert.Equal(t, "shell", tools[0].Name)
	assert.JSONEq(t, `{"type":"object","properties":{}}`, string(tools[0].InputSchema))
}

func responsesToAnthropicMessages(t *testing.T, input string) []AnthropicMessage {
	t.Helper()
	var req ResponsesRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"glm-5.2","input":`+input+`}`), &req))
	out, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	return out.Messages
}

// requireAnthropicMessagesAreSendable 断言消息序列不含 Anthropic 会拒收的形态：
// 未知 block 类型、空内容消息、纯空白 text 块。
func requireAnthropicMessagesAreSendable(t *testing.T, messages []AnthropicMessage) {
	t.Helper()
	for i, m := range messages {
		raw := strings.TrimSpace(string(m.Content))
		require.NotContains(t, []string{"", "null", `""`, "[]"}, raw,
			"messages[%d] 内容为空，Anthropic 拒收空内容消息", i)

		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			require.NotEmpty(t, strings.TrimSpace(s), "messages[%d] 字符串内容不能全为空白", i)
			continue
		}
		blocks := parseContentBlocks(m.Content)
		require.NotEmpty(t, blocks, "messages[%d] 解析不出任何 block", i)
		for j, b := range blocks {
			require.True(t, anthropicInboundBlockTypes[b.Type],
				"messages[%d].content[%d] 是 Anthropic 不认识的 block 类型 %q", i, j, b.Type)
			if b.Type == "text" {
				require.NotEmpty(t, strings.TrimSpace(b.Text),
					"messages[%d].content[%d] 是空白 text 块，Anthropic 拒收", i, j)
			}
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// assertAnthropicPairing 校验 Anthropic Messages 的工具配对，配对错误会使上游返回 400。
func assertAnthropicPairing(t *testing.T, messages []AnthropicMessage) {
	t.Helper()
	for i, m := range messages {
		blocks := parseContentBlocks(m.Content)

		// 不允许连续两条消息角色相同。
		if i > 0 {
			require.NotEqualf(t, messages[i-1].Role, m.Role, "consecutive %s messages at %d", m.Role, i)
		}

		for _, b := range blocks {
			switch b.Type {
			case "tool_result":
				// tool_result 与前一条消息的 tool_use 配对。
				require.Positivef(t, i, "tool_result %s has no previous message", b.ToolUseID)
				prev := parseContentBlocks(messages[i-1].Content)
				require.Truef(t, hasToolUse(prev, b.ToolUseID),
					"tool_result %s has no corresponding tool_use in previous message", b.ToolUseID)
			case "tool_use":
				// tool_use 与后一条消息的 tool_result 配对。
				require.Lessf(t, i+1, len(messages), "tool_use %s has no following message", b.ID)
				next := parseContentBlocks(messages[i+1].Content)
				require.Truef(t, hasToolResult(next, b.ID),
					"tool_use %s is not answered in the next message", b.ID)
			}
		}
	}
}

func hasToolUse(blocks []AnthropicContentBlock, id string) bool {
	for _, b := range blocks {
		if b.Type == "tool_use" && b.ID == id {
			return true
		}
	}
	return false
}

func hasToolResult(blocks []AnthropicContentBlock, toolUseID string) bool {
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID == toolUseID {
			return true
		}
	}
	return false
}

func convertAnthropic(t *testing.T, input string) []AnthropicMessage {
	t.Helper()
	_, messages, err := convertResponsesInputToAnthropic("", json.RawMessage(input))
	require.NoError(t, err)
	assertAnthropicPairing(t, messages)
	return messages
}

func requireObjectInputSchema(t *testing.T, schema json.RawMessage) map[string]json.RawMessage {
	t.Helper()

	require.NotEmpty(t, schema)

	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(schema, &parsed))
	require.JSONEq(t, `"object"`, string(parsed["type"]))
	require.Contains(t, parsed, "properties")

	var properties map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(parsed["properties"], &properties))

	return parsed
}
