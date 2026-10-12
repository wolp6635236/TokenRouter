package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	protocolanthropic "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// ResponsesBuffered 保留当前转换链的事件推进、用量与退出顺序。
func ResponsesBuffered(in Response, out Output, originalModel, mappedModel string, reasoningEffort *string, startTime time.Time, clientToolMapping bridge.ResponsesClientToolMapping) (*Result, error) {
	requestID := in.RequestID
	// 每次转换单独记录上游模型声明，响应别名改写使用另一份数据。
	var modelObserver protocol.ResponseModelObserver

	scanner := newConversionSSEScanner(in)

	var finalResp *protocolanthropic.AnthropicResponse
	var usage upstream.TokenUsage

	resultWithUsage := func() *Result {
		return &Result{
			RequestID:             requestID,
			UpstreamHeaders:       in.Headers,
			Usage:                 usage,
			Model:                 originalModel,
			UpstreamModel:         mappedModel,
			UpstreamResponseModel: modelObserver.Model(),
			ReasoningEffort:       reasoningEffort,
			Stream:                false,
			Duration:              time.Since(startTime),
		}
	}

	for scanner.Scan() {
		payload := scanner.frame.Data
		eventType := scanner.frame.EventType

		modelObserver.ObserveAnthropic([]byte(payload))
		var event protocolanthropic.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			out.Observe("warn", "forward_as_responses buffered: failed to parse event", err, requestID, eventType)
			continue
		}
		if event.Type == "" {
			event.Type = eventType
		}

		if event.Type == "message_start" && event.Message != nil {
			finalResp = event.Message
			protocolanthropic.MergeAnthropicUsage(&usage, event.Message.Usage)
		}

		if event.Type == "message_delta" {
			if event.Usage != nil {
				protocolanthropic.MergeAnthropicUsage(&usage, *event.Usage)
			}
			if event.Delta != nil && event.Delta.StopReason != "" && finalResp != nil {
				finalResp.StopReason = bridge.AnthropicStopReasonPtr(event.Delta.StopReason)
			}
		}

		if event.Type == "content_block_start" && event.ContentBlock != nil && finalResp != nil {
			finalResp.Content = append(finalResp.Content, *event.ContentBlock)
		}
		if event.Type == "content_block_delta" && event.Delta != nil && finalResp != nil && event.Index != nil {
			idx := *event.Index
			if idx < len(finalResp.Content) {
				switch event.Delta.Type {
				case "text_delta":
					finalResp.Content[idx].Text += event.Delta.Text
				case "thinking_delta":
					finalResp.Content[idx].Thinking += event.Delta.Thinking
				case "input_json_delta":
					finalResp.Content[idx].Input = AppendRawJSON(finalResp.Content[idx].Input, event.Delta.PartialJSON)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			out.Observe("warn", "forward_as_responses buffered: read error", err, requestID, "")
		}
		if finalResp == nil || finalResp.StopReason == nil || *finalResp.StopReason == "" || errors.Is(err, ErrConversionSSEFrameTooLarge) {
			out.Error(502, "upstream_stream_error", "Upstream stream could not be read completely")
			return resultWithUsage(), err
		}
	}

	if finalResp == nil {
		out.Error(502, "server_error", "Upstream stream ended without a response")
		return nil, fmt.Errorf("upstream stream ended without response")
	}

	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		finalResp.Usage = protocolanthropic.AnthropicUsage{
			InputTokens:              usage.InputTokens,
			OutputTokens:             usage.OutputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
		}
	}

	responsesResp := bridge.AnthropicToResponsesResponse(in.Runtime, finalResp)
	responsesResp.Model = originalModel // Use original model name

	out.CopyHeaders(in.Headers)
	// 非流式响应设置 Content-Type 为 application/json。上游强制流式响应的 text/event-stream 可能已被复制，
	// Gin 的 c.Data/c.JSON 仅在响应头缺失时设置类型，此处直接 Set 覆盖，new-api 等下游据此按 JSON 处理。
	out.BeginJSON()
	if respBytes, err := json.Marshal(responsesResp); err == nil {
		respBytes = out.ReverseTools(respBytes)
		respBytes, _, err = bridge.RestoreResponsesClientToolPayload(respBytes, clientToolMapping)
		if err != nil {
			return nil, fmt.Errorf("restore responses client tools: %w", err)
		}
		out.JSONBytes(respBytes)
	} else {
		out.ResponsesJSON(responsesResp)
	}

	return resultWithUsage(), scanner.Err()
}

// ResponsesStreaming 保留当前转换链的事件推进、用量与退出顺序。
func ResponsesStreaming(in Response, out Output, originalModel, mappedModel string, reasoningEffort *string, startTime time.Time, clientToolMapping bridge.ResponsesClientToolMapping) (*Result, error) {
	requestID := in.RequestID
	// 每次转换单独记录上游模型声明，响应别名改写使用另一份数据。
	var modelObserver protocol.ResponseModelObserver

	out.CopyHeaders(in.Headers)
	out.BeginStream()

	state := bridge.NewAnthropicEventToResponsesState(in.Runtime)
	state.Model = originalModel
	clientToolRestorer := bridge.NewResponsesClientToolStreamRestorer(clientToolMapping)
	var usage upstream.TokenUsage
	var firstTokenMs *int
	firstChunk := true

	scanner := newConversionSSEScanner(in)

	resultWithUsage := func() *Result {
		return &Result{
			RequestID:             requestID,
			UpstreamHeaders:       in.Headers,
			Usage:                 usage,
			Model:                 originalModel,
			UpstreamModel:         mappedModel,
			UpstreamResponseModel: modelObserver.Model(),
			ReasoningEffort:       reasoningEffort,
			Stream:                true,
			Duration:              time.Since(startTime),
			FirstTokenMs:          firstTokenMs,
		}
	}

	// writeEvent 统一完成名称反转、客户端工具还原与 SSE 写出，尾部事件也复用此路径。
	writeEvent := func(evt protocolopenai.ResponsesStreamEvent) bool {
		payload, err := json.Marshal(evt)
		if err != nil {
			out.Observe("warn", "forward_as_responses stream: failed to marshal event", err, requestID, "")
			return false
		}
		payload = out.ReverseTools(payload)
		payloads, _, err := clientToolRestorer.RestoreEvent(payload)
		if err != nil {
			out.Observe("warn", "forward_as_responses stream: failed to restore client tools", err, requestID, "")
			return false
		}
		for _, restored := range payloads {
			eventType := gjson.GetBytes(restored, "type").String()
			if _, err := out.Event(eventType, restored); err != nil {
				out.Observe("info", "forward_as_responses stream: client disconnected", nil, requestID, "")
				return true
			}
		}
		return false
	}

	processEvent := func(event *protocolanthropic.AnthropicStreamEvent) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		if event.Type == "message_delta" && event.Usage != nil {
			protocolanthropic.MergeAnthropicUsage(&usage, *event.Usage)
		}
		if event.Type == "message_start" && event.Message != nil {
			protocolanthropic.MergeAnthropicUsage(&usage, event.Message.Usage)
		}

		events := bridge.AnthropicEventToResponsesEvents(in.Runtime, event, state)
		if slices.ContainsFunc(events, writeEvent) {
			return true
		}
		if len(events) > 0 {
			out.Flush()
		}
		return false
	}

	finalizeStream := func() (*Result, error) {
		if finalEvents := bridge.FinalizeAnthropicResponsesStream(state); len(finalEvents) > 0 {
			if slices.ContainsFunc(finalEvents, writeEvent) {
				return resultWithUsage(), nil
			}
			out.Flush()
		}
		return resultWithUsage(), nil
	}

	for scanner.Scan() {
		payload := scanner.frame.Data
		eventType := scanner.frame.EventType

		modelObserver.ObserveAnthropic([]byte(payload))
		var event protocolanthropic.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			out.Observe("warn", "forward_as_responses stream: failed to parse event", err, requestID, eventType)
			continue
		}
		if event.Type == "" {
			event.Type = eventType
		}

		if processEvent(&event) {
			return resultWithUsage(), nil
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			out.Observe("warn", "forward_as_responses stream: read error", err, requestID, "")
		}
		if (!state.CompletedSent && state.StopReason == "") || errors.Is(err, ErrConversionSSEFrameTooLarge) {
			if !state.CompletedSent {
				writeConversionStreamError(out, true, state)
			}
			return resultWithUsage(), err
		}
	}

	result, err := finalizeStream()
	return result, errors.Join(err, scanner.Err())
}

// ChatBuffered 保留当前转换链的事件推进、用量与退出顺序。
func ChatBuffered(in Response, out Output, originalModel, mappedModel string, reasoningEffort *string, startTime time.Time) (*Result, error) {
	requestID := in.RequestID
	// 每次转换单独记录上游模型声明，响应别名改写使用另一份数据。
	var modelObserver protocol.ResponseModelObserver

	scanner := newConversionSSEScanner(in)

	var finalResp *protocolanthropic.AnthropicResponse
	var usage upstream.TokenUsage

	resultWithUsage := func() *Result {
		return &Result{
			RequestID:             requestID,
			UpstreamHeaders:       in.Headers,
			Usage:                 usage,
			Model:                 originalModel,
			UpstreamModel:         mappedModel,
			UpstreamResponseModel: modelObserver.Model(),
			ReasoningEffort:       reasoningEffort,
			Stream:                false,
			Duration:              time.Since(startTime),
		}
	}

	for scanner.Scan() {
		payload := scanner.frame.Data
		eventType := scanner.frame.EventType

		modelObserver.ObserveAnthropic([]byte(payload))
		var event protocolanthropic.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if event.Type == "" {
			event.Type = eventType
		}

		if event.Type == "message_start" && event.Message != nil {
			finalResp = event.Message
			protocolanthropic.MergeAnthropicUsage(&usage, event.Message.Usage)
		}

		if event.Type == "message_delta" {
			if event.Usage != nil {
				protocolanthropic.MergeAnthropicUsage(&usage, *event.Usage)
			}
			if event.Delta != nil && event.Delta.StopReason != "" && finalResp != nil {
				finalResp.StopReason = bridge.AnthropicStopReasonPtr(event.Delta.StopReason)
			}
		}
		if event.Type == "content_block_start" && event.ContentBlock != nil && finalResp != nil {
			finalResp.Content = append(finalResp.Content, *event.ContentBlock)
		}
		if event.Type == "content_block_delta" && event.Delta != nil && finalResp != nil && event.Index != nil {
			idx := *event.Index
			if idx < len(finalResp.Content) {
				switch event.Delta.Type {
				case "text_delta":
					finalResp.Content[idx].Text += event.Delta.Text
				case "thinking_delta":
					finalResp.Content[idx].Thinking += event.Delta.Thinking
				case "input_json_delta":
					finalResp.Content[idx].Input = AppendRawJSON(finalResp.Content[idx].Input, event.Delta.PartialJSON)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			out.Observe("warn", "forward_as_cc buffered: read error", err, requestID, "")
		}
		if finalResp == nil || finalResp.StopReason == nil || *finalResp.StopReason == "" || errors.Is(err, ErrConversionSSEFrameTooLarge) {
			out.Error(502, "upstream_stream_error", "Upstream stream could not be read completely")
			return resultWithUsage(), err
		}
	}

	if finalResp == nil {
		out.Error(502, "server_error", "Upstream stream ended without a response")
		return nil, fmt.Errorf("upstream stream ended without response")
	}

	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		finalResp.Usage = protocolanthropic.AnthropicUsage{
			InputTokens:              usage.InputTokens,
			OutputTokens:             usage.OutputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
		}
	}

	responsesResp := bridge.AnthropicToResponsesResponse(in.Runtime, finalResp)
	ccResp := bridge.ResponsesToChatCompletions(in.Runtime, responsesResp, originalModel)

	out.CopyHeaders(in.Headers)
	// 非流式响应设置 Content-Type 为 application/json。上游强制流式响应的 text/event-stream 可能已被复制，
	// Gin 的 c.Data/c.JSON 仅在响应头缺失时设置类型，此处直接 Set 覆盖，new-api 等下游据此按 JSON 处理。
	out.BeginJSON()
	if respBytes, err := json.Marshal(ccResp); err == nil {
		respBytes = out.ReverseTools(respBytes)
		out.JSONBytes(respBytes)
	} else {
		out.ChatJSON(ccResp)
	}

	return resultWithUsage(), scanner.Err()
}

// ChatStreaming 保留当前转换链的事件推进、用量与退出顺序。
func ChatStreaming(in Response, out Output, originalModel, mappedModel string, reasoningEffort *string, startTime time.Time, includeUsage bool) (*Result, error) {
	requestID := in.RequestID
	// 每次转换单独记录上游模型声明，响应别名改写使用另一份数据。
	var modelObserver protocol.ResponseModelObserver

	out.CopyHeaders(in.Headers)
	out.BeginStream()

	anthState := bridge.NewAnthropicEventToResponsesState(in.Runtime)
	anthState.Model = originalModel
	ccState := bridge.NewResponsesEventToChatState(in.Runtime)
	ccState.Model = originalModel
	ccState.IncludeUsage = includeUsage

	var usage upstream.TokenUsage
	var firstTokenMs *int
	firstChunk := true

	scanner := newConversionSSEScanner(in)

	resultWithUsage := func() *Result {
		return &Result{
			RequestID:             requestID,
			UpstreamHeaders:       in.Headers,
			Usage:                 usage,
			Model:                 originalModel,
			UpstreamModel:         mappedModel,
			UpstreamResponseModel: modelObserver.Model(),
			ReasoningEffort:       reasoningEffort,
			Stream:                true,
			Duration:              time.Since(startTime),
			FirstTokenMs:          firstTokenMs,
		}
	}

	writeChunk := func(chunk protocolopenai.ChatCompletionsChunk) bool {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return false
		}
		payload = out.ReverseTools(payload)
		// HTTP Adapter 持有请求侧工具名称映射；无映射时仅做静态前缀还原。
		if _, err := out.Event("", payload); err != nil {
			return true // client disconnected
		}
		return false
	}

	processAnthropicEvent := func(event *protocolanthropic.AnthropicStreamEvent) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		if event.Type == "message_delta" && event.Usage != nil {
			protocolanthropic.MergeAnthropicUsage(&usage, *event.Usage)
		}
		if event.Type == "message_start" && event.Message != nil {
			protocolanthropic.MergeAnthropicUsage(&usage, event.Message.Usage)
		}

		responsesEvents := bridge.AnthropicEventToResponsesEvents(in.Runtime, event, anthState)
		for _, resEvt := range responsesEvents {
			ccChunks := bridge.ResponsesEventToChatChunks(&resEvt, ccState)
			for _, chunk := range ccChunks {
				if disconnected := writeChunk(chunk); disconnected {
					return true
				}
			}
		}
		out.Flush()
		return false
	}

	for scanner.Scan() {
		payload := scanner.frame.Data
		eventType := scanner.frame.EventType

		modelObserver.ObserveAnthropic([]byte(payload))
		var event protocolanthropic.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if event.Type == "" {
			event.Type = eventType
		}

		if processAnthropicEvent(&event) {
			return resultWithUsage(), nil
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			out.Observe("warn", "forward_as_cc stream: read error", err, requestID, "")
		}
		if (!anthState.CompletedSent && anthState.StopReason == "") || errors.Is(err, ErrConversionSSEFrameTooLarge) {
			if !anthState.CompletedSent {
				writeConversionStreamError(out, false, anthState)
			}
			return resultWithUsage(), err
		}
	}

	finalResEvents := bridge.FinalizeAnthropicResponsesStream(anthState)
	for _, resEvt := range finalResEvents {
		ccChunks := bridge.ResponsesEventToChatChunks(&resEvt, ccState)
		for _, chunk := range ccChunks {
			writeChunk(chunk)
		}
	}
	finalCCChunks := bridge.FinalizeResponsesChatStream(ccState)
	for _, chunk := range finalCCChunks {
		writeChunk(chunk)
	}

	out.Event("", []byte("[DONE]")) //nolint:errcheck // 流已经结束，[DONE] 写失败时没有后续处理。
	out.Flush()

	return resultWithUsage(), scanner.Err()
}

// writeConversionStreamError 用客户端协议报告读取失败，已写出的流以错误事件结束。
func writeConversionStreamError(out Output, responses bool, state *bridge.AnthropicEventToResponsesState) {
	streamError := &protocolopenai.ResponsesError{Code: "upstream_stream_error", Message: "Upstream stream could not be read completely"}
	var value any = map[string]any{"error": streamError}
	eventType := ""
	if responses {
		eventType = "response.failed"
		value = protocolopenai.ResponsesStreamEvent{
			Type:           eventType,
			SequenceNumber: state.SequenceNumber,
			Response: &protocolopenai.ResponsesResponse{
				ID:        state.ResponseID,
				Object:    "response",
				CreatedAt: state.Created,
				Model:     state.Model,
				Status:    "failed",
				Error:     streamError,
				Output:    []protocolopenai.ResponsesOutput{},
			},
		}
	}
	payload, err := json.Marshal(value)
	if err == nil {
		_, _ = out.Event(eventType, payload)
		out.Flush()
	}
}

func AppendRawJSON(existing json.RawMessage, fragment string) json.RawMessage {
	var existingObject map[string]json.RawMessage
	isEmptyObject := json.Unmarshal(existing, &existingObject) == nil && existingObject != nil && len(existingObject) == 0
	if len(existing) == 0 || isEmptyObject {
		return json.RawMessage(fragment)
	}
	return json.RawMessage(string(existing) + fragment)
}
