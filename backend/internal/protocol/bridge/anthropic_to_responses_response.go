package bridge

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// AnthropicEventToResponsesState tracks state for converting a sequence of
// Anthropic SSE events into Responses SSE events.
type AnthropicEventToResponsesState struct {
	ResponseID     string
	Model          string
	Created        int64
	SequenceNumber int

	// CreatedSent tracks whether response.created has been emitted.
	CreatedSent bool
	// CompletedSent tracks whether the terminal event has been emitted.
	CompletedSent bool

	// Current output tracking
	OutputIndex     int
	CurrentItemID   string
	CurrentItemType string // "message" | "function_call" | "reasoning"

	// 消息输出的内容分片索引与当前文本累积值。
	ContentIndex int
	TextAccum    string

	// 函数调用当前输出项的信息。
	CurrentCallID string
	CurrentName   string

	// 当前打开输出项的完整内容，在关闭时写入 Outputs。
	CurrentContent []ResponsesContentPart // message
	CurrentArgs    string                 // function_call
	CurrentSummary string                 // reasoning

	// 累积所有已关闭输出项，供终止事件返回完整结果。
	Outputs []ResponsesOutput

	// message_start 和 message_delta 的 InputTokens 为未缓存输入，输出 Responses usage 时加回缓存 token。
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int

	// StopReason 保存最后一个非空的 Anthropic 终止原因，供 message_stop 生成正确的 Responses 终态。
	StopReason string
}

// AnthropicToResponsesResponse converts an Anthropic Messages response into a
// Responses API response. This is the reverse of ResponsesToAnthropic and
// enables Anthropic upstream responses to be returned in OpenAI Responses format.
func AnthropicToResponsesResponse(runtime Runtime, resp *AnthropicResponse) *ResponsesResponse {
	id := resp.ID
	if id == "" {
		id = generateResponsesID(runtime)
	}

	// Anthropic 响应不携带创建时间，因此使用当前时间；规则与上方生成响应 ID 时保持一致，
	// 都是为严格客户端补齐必需字段。
	out := &ResponsesResponse{
		ID:        id,
		Object:    "response",
		CreatedAt: runtime.Now().Unix(),
		Model:     resp.Model,
	}

	var outputs []ResponsesOutput
	var msgParts []ResponsesContentPart

	for _, block := range resp.Content {
		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				outputs = append(outputs, ResponsesOutput{
					Type: "reasoning",
					ID:   generateItemID(runtime),
					Summary: []ResponsesSummary{{
						Type: "summary_text",
						Text: block.Thinking,
					}},
				})
			}
		case "text":
			if block.Text != "" {
				msgParts = append(msgParts, ResponsesContentPart{
					Type: "output_text",
					Text: block.Text,
				})
			}
		case "tool_use":
			args := "{}"
			if len(block.Input) > 0 {
				args = string(block.Input)
			}
			outputs = append(outputs, ResponsesOutput{
				Type:      "function_call",
				ID:        generateItemID(runtime),
				CallID:    toResponsesCallID(block.ID),
				Name:      block.Name,
				Arguments: args,
				Status:    "completed",
			})
		}
	}

	// Assemble message output item from text parts
	if len(msgParts) > 0 {
		outputs = append(outputs, ResponsesOutput{
			Type:    "message",
			ID:      generateItemID(runtime),
			Role:    "assistant",
			Content: msgParts,
			Status:  "completed",
		})
	}

	if len(outputs) == 0 {
		outputs = append(outputs, ResponsesOutput{
			Type:    "message",
			ID:      generateItemID(runtime),
			Role:    "assistant",
			Content: []ResponsesContentPart{{Type: "output_text", Text: ""}},
			Status:  "completed",
		})
	}
	out.Output = outputs

	// Map stop_reason → status
	out.Status = anthropicStopReasonToResponsesStatus(AnthropicStopReasonString(resp.StopReason), resp.Content)
	if out.Status == "incomplete" {
		out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}

	// Anthropic 的 input_tokens 表示未缓存输入，OpenAI Responses 的 input_tokens 表示总输入。
	// 转换时把 cache_read 和 cache_creation 加回 input_tokens。
	totalInputTokens := resp.Usage.InputTokens +
		resp.Usage.CacheReadInputTokens +
		resp.Usage.CacheCreationInputTokens
	out.Usage = &ResponsesUsage{
		InputTokens:              totalInputTokens,
		OutputTokens:             resp.Usage.OutputTokens,
		TotalTokens:              totalInputTokens + resp.Usage.OutputTokens,
		CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
	}
	if resp.Usage.CacheReadInputTokens > 0 {
		out.Usage.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: resp.Usage.CacheReadInputTokens,
		}
	}

	return out
}

// anthropicStopReasonToResponsesStatus maps Anthropic stop_reason to Responses status.
func anthropicStopReasonToResponsesStatus(stopReason string, blocks []AnthropicContentBlock) string {
	switch stopReason {
	case "max_tokens":
		return "incomplete"
	case "end_turn", "tool_use", "stop_sequence":
		return "completed"
	default:
		return "completed"
	}
}

// NewAnthropicEventToResponsesState returns an initialised stream state.
func NewAnthropicEventToResponsesState(runtime Runtime) *AnthropicEventToResponsesState {
	return &AnthropicEventToResponsesState{
		Created: runtime.Now().Unix(),
	}
}

// AnthropicEventToResponsesEvents converts a single Anthropic SSE event into
// zero or more Responses SSE events, updating state as it goes.
func AnthropicEventToResponsesEvents(runtime Runtime,
	evt *AnthropicStreamEvent,
	state *AnthropicEventToResponsesState,
) []ResponsesStreamEvent {
	switch evt.Type {
	case "message_start":
		return anthToResHandleMessageStart(evt, state)
	case "content_block_start":
		return anthToResHandleContentBlockStart(runtime, evt, state)
	case "content_block_delta":
		return anthToResHandleContentBlockDelta(evt, state)
	case "content_block_stop":
		return anthToResHandleContentBlockStop(evt, state)
	case "message_delta":
		return anthToResHandleMessageDelta(evt, state)
	case "message_stop":
		return anthToResHandleMessageStop(state)
	default:
		return nil
	}
}

// FinalizeAnthropicResponsesStream emits synthetic termination events if the
// stream ended without a proper message_stop.
func FinalizeAnthropicResponsesStream(state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if !state.CreatedSent || state.CompletedSent {
		return nil
	}

	var events []ResponsesStreamEvent

	// Close any open item
	events = append(events, closeCurrentResponsesItem(state)...)

	status, incompleteDetails := anthropicResponsesStreamTerminalState(state.StopReason)
	events = append(events, makeResponsesCompletedEvent(state, status, incompleteDetails))
	state.CompletedSent = true
	return events
}

// ResponsesEventToSSE formats a ResponsesStreamEvent as an SSE data line.
func ResponsesEventToSSE(evt ResponsesStreamEvent) (string, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", evt.Type, data), nil
}

func anthToResHandleMessageStart(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.Message != nil {
		state.ResponseID = evt.Message.ID
		if state.Model == "" {
			state.Model = evt.Message.Model
		}
		if evt.Message.Usage.InputTokens > 0 {
			state.InputTokens = evt.Message.Usage.InputTokens
		}
		if evt.Message.Usage.CacheReadInputTokens > 0 {
			state.CacheReadInputTokens = evt.Message.Usage.CacheReadInputTokens
		}
		if evt.Message.Usage.CacheCreationInputTokens > 0 {
			state.CacheCreationInputTokens = evt.Message.Usage.CacheCreationInputTokens
		}
	}

	if state.CreatedSent {
		return nil
	}
	state.CreatedSent = true

	// Emit response.created
	return []ResponsesStreamEvent{makeResponsesCreatedEvent(state)}
}

func anthToResHandleContentBlockStart(runtime Runtime, evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.ContentBlock == nil {
		return nil
	}

	var events []ResponsesStreamEvent

	switch evt.ContentBlock.Type {
	case "thinking":
		// 开始 thinking item 前先关闭当前 item。message 的 text 块结束后仍保持 item 打开，以接收后续 text 块。
		// 若直接覆盖 CurrentItemType 和 CurrentItemID，CurrentContent 中的文本会丢失，completed 仅剩 reasoning。
		// interleaved-thinking 会产生先 text 后 thinking 的顺序，网关支持该 beta。
		events = append(events, closeCurrentResponsesItem(state)...)

		state.CurrentItemID = generateItemID(runtime)
		state.CurrentItemType = "reasoning"
		state.ContentIndex = 0

		events = append(events, makeResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
			OutputIndex: state.OutputIndex,
			Item: &ResponsesOutput{
				Type: "reasoning",
				ID:   state.CurrentItemID,
			},
		}))

	case "text":
		// If we don't have an open message item, open one
		if state.CurrentItemType != "message" {
			// 前一个 reasoning 或 function_call 已在 content_block_stop 时关闭，此时 CurrentItemType 为空。
			// text、thinking 和 tool_use 分支都在打开新 item 前执行关闭。
			events = append(events, closeCurrentResponsesItem(state)...)

			state.CurrentItemID = generateItemID(runtime)
			state.CurrentItemType = "message"
			state.ContentIndex = 0

			events = append(events, makeResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
				OutputIndex: state.OutputIndex,
				Item: &ResponsesOutput{
					Type:   "message",
					ID:     state.CurrentItemID,
					Role:   "assistant",
					Status: "in_progress",
				},
			}))
		}

		// OpenAI SDK 只有收到 content_part.added 才会创建对应内容分片，
		// 因此该事件必须先于同一分片的首个 output_text.delta。
		state.ContentIndex = len(state.CurrentContent)
		events = append(events, makeResponsesEvent(state, "response.content_part.added", &ResponsesStreamEvent{
			OutputIndex:  state.OutputIndex,
			ContentIndex: state.ContentIndex,
			ItemID:       state.CurrentItemID,
			Part:         &ResponsesContentPart{Type: "output_text", Text: ""},
		}))
		state.TextAccum = ""

	case "tool_use":
		// Close previous item if any
		events = append(events, closeCurrentResponsesItem(state)...)

		state.CurrentItemID = generateItemID(runtime)
		state.CurrentItemType = "function_call"
		state.CurrentCallID = toResponsesCallID(evt.ContentBlock.ID)
		state.CurrentName = evt.ContentBlock.Name

		events = append(events, makeResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
			OutputIndex: state.OutputIndex,
			Item: &ResponsesOutput{
				Type:   "function_call",
				ID:     state.CurrentItemID,
				CallID: state.CurrentCallID,
				Name:   state.CurrentName,
				Status: "in_progress",
			},
		}))
	}

	return events
}

func anthToResHandleContentBlockDelta(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.Delta == nil {
		return nil
	}

	switch evt.Delta.Type {
	case "text_delta":
		if evt.Delta.Text == "" {
			return nil
		}
		state.TextAccum += evt.Delta.Text
		return []ResponsesStreamEvent{makeResponsesEvent(state, "response.output_text.delta", &ResponsesStreamEvent{
			OutputIndex:  state.OutputIndex,
			ContentIndex: state.ContentIndex,
			Delta:        evt.Delta.Text,
			ItemID:       state.CurrentItemID,
		})}

	case "thinking_delta":
		if evt.Delta.Thinking == "" {
			return nil
		}
		state.CurrentSummary += evt.Delta.Thinking
		return []ResponsesStreamEvent{makeResponsesEvent(state, "response.reasoning_summary_text.delta", &ResponsesStreamEvent{
			OutputIndex:  state.OutputIndex,
			SummaryIndex: 0,
			Delta:        evt.Delta.Thinking,
			ItemID:       state.CurrentItemID,
		})}

	case "input_json_delta":
		if evt.Delta.PartialJSON == "" {
			return nil
		}
		state.CurrentArgs += evt.Delta.PartialJSON
		return []ResponsesStreamEvent{makeResponsesEvent(state, "response.function_call_arguments.delta", &ResponsesStreamEvent{
			OutputIndex: state.OutputIndex,
			Delta:       evt.Delta.PartialJSON,
			ItemID:      state.CurrentItemID,
			CallID:      state.CurrentCallID,
			Name:        state.CurrentName,
		})}

	case "signature_delta":
		// Anthropic signature deltas have no Responses equivalent; skip
		return nil
	}

	return nil
}

func anthToResHandleContentBlockStop(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	switch state.CurrentItemType {
	case "reasoning":
		// Emit reasoning summary done + output item done
		events := []ResponsesStreamEvent{
			makeResponsesEvent(state, "response.reasoning_summary_text.done", &ResponsesStreamEvent{
				OutputIndex:  state.OutputIndex,
				SummaryIndex: 0,
				ItemID:       state.CurrentItemID,
			}),
		}
		events = append(events, closeCurrentResponsesItem(state)...)
		return events

	case "function_call":
		// Emit function_call_arguments.done + output item done
		events := []ResponsesStreamEvent{
			makeResponsesEvent(state, "response.function_call_arguments.done", &ResponsesStreamEvent{
				OutputIndex: state.OutputIndex,
				ItemID:      state.CurrentItemID,
				CallID:      state.CurrentCallID,
				Name:        state.CurrentName,
			}),
		}
		events = append(events, closeCurrentResponsesItem(state)...)
		return events

	case "message":
		// 文本块结束时按 OpenAI 顺序发送两个完成事件，并携带完整文本；
		// message 输出项保持打开，以便继续接收后续文本块。
		text := state.TextAccum
		state.TextAccum = ""
		contentIndex := state.ContentIndex
		state.CurrentContent = append(state.CurrentContent, ResponsesContentPart{Type: "output_text", Text: text})
		// 关闭 part 后递增 content_index，让同一 item 中的后续 text 块使用新下标。
		// SDK 按 content[content_index] 累积，重复下标会覆盖此前的可见文本。
		// 新 item 的 content_index 由 closeCurrentResponsesItem 重置为 0。
		state.ContentIndex++
		return []ResponsesStreamEvent{
			makeResponsesEvent(state, "response.output_text.done", &ResponsesStreamEvent{
				OutputIndex:  state.OutputIndex,
				ContentIndex: contentIndex,
				ItemID:       state.CurrentItemID,
				Text:         text,
			}),
			makeResponsesEvent(state, "response.content_part.done", &ResponsesStreamEvent{
				OutputIndex:  state.OutputIndex,
				ContentIndex: contentIndex,
				ItemID:       state.CurrentItemID,
				Part:         &ResponsesContentPart{Type: "output_text", Text: text},
			}),
		}
	}

	return nil
}

func anthToResHandleMessageDelta(evt *AnthropicStreamEvent, state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if evt.Usage != nil {
		state.OutputTokens = evt.Usage.OutputTokens
		if evt.Usage.InputTokens > 0 {
			state.InputTokens = evt.Usage.InputTokens
		}
		if evt.Usage.CacheReadInputTokens > 0 {
			state.CacheReadInputTokens = evt.Usage.CacheReadInputTokens
		}
		if evt.Usage.CacheCreationInputTokens > 0 {
			state.CacheCreationInputTokens = evt.Usage.CacheCreationInputTokens
		}
	}
	if evt.Delta != nil && evt.Delta.StopReason != "" {
		state.StopReason = evt.Delta.StopReason
	}

	return nil
}

func anthToResHandleMessageStop(state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if state.CompletedSent {
		return nil
	}

	var events []ResponsesStreamEvent
	events = append(events, closeCurrentResponsesItem(state)...)

	status, incompleteDetails := anthropicResponsesStreamTerminalState(state.StopReason)
	events = append(events, makeResponsesCompletedEvent(state, status, incompleteDetails))
	state.CompletedSent = true
	return events
}

func anthropicResponsesStreamTerminalState(stopReason string) (string, *ResponsesIncompleteDetails) {
	if stopReason == "max_tokens" {
		return "incomplete", &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}
	return "completed", nil
}

func closeCurrentResponsesItem(state *AnthropicEventToResponsesState) []ResponsesStreamEvent {
	if state.CurrentItemType == "" {
		return nil
	}

	// 组装完整输出项，交给 output_item.done 和终止事件输出。
	item := ResponsesOutput{
		Type:   state.CurrentItemType,
		ID:     state.CurrentItemID,
		Status: "completed",
	}
	switch state.CurrentItemType {
	case "message":
		item.Role = "assistant"
		item.Content = state.CurrentContent
	case "function_call":
		item.CallID = state.CurrentCallID
		item.Name = state.CurrentName
		args := state.CurrentArgs
		if args == "" {
			args = "{}"
		}
		item.Arguments = args
	case "reasoning":
		if state.CurrentSummary != "" {
			item.Summary = []ResponsesSummary{{Type: "summary_text", Text: state.CurrentSummary}}
		}
	}
	state.Outputs = append(state.Outputs, item)

	// 清理当前输出项状态。
	state.CurrentItemType = ""
	state.CurrentItemID = ""
	state.CurrentCallID = ""
	state.CurrentName = ""
	state.CurrentContent = nil
	state.CurrentArgs = ""
	state.CurrentSummary = ""
	state.TextAccum = ""
	state.OutputIndex++
	state.ContentIndex = 0

	return []ResponsesStreamEvent{makeResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
		OutputIndex: state.OutputIndex - 1, // 使用递增前的输出索引。
		Item:        &item,
	})}
}

func makeResponsesCreatedEvent(state *AnthropicEventToResponsesState) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++
	return ResponsesStreamEvent{
		Type:           "response.created",
		SequenceNumber: seq,
		Response: &ResponsesResponse{
			ID:        state.ResponseID,
			Object:    "response",
			CreatedAt: state.Created,
			Model:     state.Model,
			Status:    "in_progress",
			Output:    []ResponsesOutput{},
		},
	}
}

func makeResponsesCompletedEvent(
	state *AnthropicEventToResponsesState,
	status string,
	incompleteDetails *ResponsesIncompleteDetails,
) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++

	// Anthropic 的 input_tokens 为未缓存输入，此处加回缓存 token 后输出 Responses 的输入总量。
	totalInputTokens := state.InputTokens + state.CacheReadInputTokens + state.CacheCreationInputTokens
	usage := &ResponsesUsage{
		InputTokens:              totalInputTokens,
		OutputTokens:             state.OutputTokens,
		TotalTokens:              totalInputTokens + state.OutputTokens,
		CacheCreationInputTokens: state.CacheCreationInputTokens,
	}
	if state.CacheReadInputTokens > 0 {
		usage.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: state.CacheReadInputTokens,
		}
	}

	eventType := "response.completed"
	if status == "incomplete" {
		eventType = "response.incomplete"
	}

	// SDK 会直接从终止事件重建最终响应，因此必须带上流中累积的输出项。
	outputs := state.Outputs
	if outputs == nil {
		outputs = []ResponsesOutput{}
	}

	return ResponsesStreamEvent{
		Type:           eventType,
		SequenceNumber: seq,
		Response: &ResponsesResponse{
			ID:                state.ResponseID,
			Object:            "response",
			CreatedAt:         state.Created,
			Model:             state.Model,
			Status:            status,
			Output:            outputs,
			Usage:             usage,
			IncompleteDetails: incompleteDetails,
		},
	}
}

func makeResponsesEvent(state *AnthropicEventToResponsesState, eventType string, template *ResponsesStreamEvent) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++

	evt := *template
	evt.Type = eventType
	evt.SequenceNumber = seq
	return evt
}

func generateResponsesID(runtime Runtime) string {
	b := make([]byte, 12)
	_, _ = runtime.ReadRandom(b)
	return "resp_" + hex.EncodeToString(b)
}

func generateItemID(runtime Runtime) string {
	b := make([]byte, 12)
	_, _ = runtime.ReadRandom(b)
	return "item_" + hex.EncodeToString(b)
}
