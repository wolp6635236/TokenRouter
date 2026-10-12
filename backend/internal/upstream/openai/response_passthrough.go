package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	wire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// PassthroughOptions 配置响应透传的观测、HTTP 输出和心跳。
type PassthroughOptions struct {
	// ObserveModel 在恢复响应模型前读取上游模型声明。
	ObserveModel func([]byte, string)
	StreamOptions
	NonStream                    NonStreamOptions
	Headers                      func(http.Header, http.Header)
	StartKeepalive               func(http.Header) func()
	MissingUsage                 func(*http.Response, *wire.ForwardUsage, string, bool)
	MissingTerminal              func(string)
	PassthroughFailoverWithModel func(string, []byte, string, string) error
}

// ReadPassthroughStreaming 透传 SSE，并记录用量和终态。
func ReadPassthroughStreaming(ctx context.Context, resp *http.Response, c *upstream.OutputContext, options PassthroughOptions, startTime time.Time, originalModel, mappedModel string) (*StreamingResult, error) {
	options.Headers(c.Writer.Header(), resp.Header)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	if v := resp.Header.Get("x-request-id"); v != "" {
		c.Header("x-request-id", v)
	}

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}

	usage := &wire.ForwardUsage{}
	imageCounter := wire.NewOpenAIImageOutputCounter()
	var firstTokenMs *int
	ttftMode := options.TTFTMode()
	responseID := ""
	clientDisconnected := false
	sawDone := false
	sawTerminalEvent := false
	sawFailedEvent := false
	sawBareError := false
	sawResponseFailed := false
	terminalEventType := ""
	semanticOutputSeen := false
	capacityFailoverSuppressedLogged := false
	failedMessage := ""
	var failedPayload []byte
	clientOutputStarted := false
	codexFailureTerminal := options.NativeOpenAI
	failureDelivered := false
	suppressCurrentEvent := false
	responseFailedPending := false
	var bareErrorPayload []byte
	bareErrorProviderSideEffectsPending := false
	upstreamRequestID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	// pendingLines 在首个可见输出前保留前导事件，确保无输出失败仍可安全 failover。
	pendingLines := make([]string, 0, 8)

	// pendingLines 在首段内容前缓冲事件，首次写入前 HTTP 响应头也处于待提交状态。
	// 推理等待期间发送 SSE 注释心跳，提交响应头并刷新中间代理的空闲超时。
	// 某生产部署在 12 小时内有 44 个 /v1/responses 请求等待 600~900s 才产生输出，
	// 每个请求生成 3~5 万 output token，均被 nginx 的 proxy_read_timeout(600s) 中断为 504。
	// proxy_read_timeout 衡量两次读取的间隔，心跳可维持这些长推理请求的连接。
	// pendingLines 和提供商请求头在内容开始时交付，心跳字节由
	// OpenAICompactKeepaliveAdjustedWrittenSize 排除（#3887），首输出前仍可切换提供商。
	// 此处上游已返回 text/event-stream，使用普通 SSE 心跳；Compact 心跳还要求 compact 标记。
	stopKeepalive := options.StartKeepalive(c.Writer.Header())
	// 任何返回路径都要停拍。Stop 与心跳 goroutine 之间有互斥锁，
	// 返回后不会再有字节写出。
	defer stopKeepalive()
	// flushPending 标记尚未遇到 SSE 空行的写入。函数退出时刷新残留数据，已断连时跳过。
	flushPending := false
	pendingSSEEventType := ""
	flushPendingOutput := func() {
		if clientDisconnected || !flushPending {
			return
		}
		flusher.Flush()
		options.MarkTime(StreamTimeFlush)
		flushPending = false
	}
	defer flushPendingOutput()
	writePendingLines := func() bool {
		for _, pending := range pendingLines {
			if _, err := fmt.Fprintln(w, pending); err != nil {
				clientDisconnected = true
				options.Logf("[OpenAI passthrough] Client disconnected during streaming, continue draining upstream for usage: provider=%d", options.ProviderID)
				return false
			}
		}
		pendingLines = pendingLines[:0]
		return true
	}
	ensureResponseFailedTerminal := func() {
		if !sawBareError || sawResponseFailed || failureDelivered {
			return
		}
		if bareErrorProviderSideEffectsPending {
			options.TerminalSideEffects(bareErrorPayload, failedMessage, resp.Header, mappedModel)
			bareErrorProviderSideEffectsPending = false
		}
		if clientDisconnected || !writePendingLines() {
			return
		}
		if _, err := fmt.Fprint(w, options.BuildOpenAIResponseFailedSSE(responseID, originalModel, bareErrorPayload, failedMessage)); err != nil {
			clientDisconnected = true
			return
		}
		clientOutputStarted = true
		failureDelivered = true
		flushPending = true
		flushPendingOutput()
	}

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := options.MaxLineSize
	scanBuf := httpclient.GetSSEScannerBuf64K()
	scanner.Buffer(scanBuf[:0], maxLineSize)
	defer httpclient.PutSSEScannerBuf64K(scanBuf)
	documentScanner := wire.NewSSEJSONDocumentScanner(scanner)

	needModelReplace := strings.TrimSpace(originalModel) != "" && strings.TrimSpace(mappedModel) != "" && strings.TrimSpace(originalModel) != strings.TrimSpace(mappedModel)
	responseAccumulator := bridge.NewBufferedResponseAccumulator()
	streamDoneItems := bridge.NewResponsesStreamOutputItems()
	streamImageOutputs := make([]json.RawMessage, 0, 1)
	streamSeenImages := make(map[string]struct{})
	resultWithUsage := func() *StreamingResult {
		return &StreamingResult{
			Usage:            usage,
			FirstTokenMs:     firstTokenMs,
			ResponseID:       responseID,
			ImageCount:       imageCounter.Count(),
			ImageOutputSizes: imageCounter.Sizes(),
		}
	}

	var lifecycleNormalizer wire.ResponseLifecycleNormalizer
	pendingLifecycleHeader := ""
	for documentScanner.Scan() {
		line := documentScanner.Text()
		if eventType, ok := wire.ExtractSSEEventLine(line); ok {
			pendingSSEEventType = eventType
			eventType = strings.TrimSpace(eventType)
			suppressCurrentEvent = codexFailureTerminal && (eventType == "error" || (sawBareError && !sawResponseFailed && eventType != "response.failed"))
			if wire.IsResponseLifecycleEvent(eventType) {
				// 生命周期事件头与 data 一起下发，避免留下已过滤终态的孤立 event 行。
				pendingLifecycleHeader = line
				continue
			}
		}
		lineStartsClientOutput := false
		forceFlushFailedEvent := false
		if data, ok := wire.ExtractSSEDataLine(line); ok {
			options.MarkTime(StreamTimeData)
			dataBytes := []byte(data)
			trimmedData := strings.TrimSpace(data)
			rawEventType := wire.EffectiveOpenAISSEEventType(dataBytes, pendingSSEEventType)
			if options.ObserveModel != nil {
				options.ObserveModel(dataBytes, rawEventType)
			}
			normalizedData, normalizedType, duplicateTerminal := lifecycleNormalizer.Normalize(dataBytes, rawEventType)
			if !bytes.Equal(normalizedData, dataBytes) {
				dataBytes = normalizedData
				data = string(dataBytes)
				trimmedData = strings.TrimSpace(data)
				line = "data: " + data
			}
			rawEventType = normalizedType
			if needModelReplace && strings.Contains(data, mappedModel) {
				line = wire.ReplaceModelInSSELine(line, mappedModel, originalModel)
				if replacedData, replaced := wire.ExtractSSEDataLine(line); replaced {
					dataBytes = []byte(replacedData)
					trimmedData = strings.TrimSpace(replacedData)
				}
			}
			if normalizedData, normalized := wire.NormalizeOpenAIResponsesFunctionCallArguments(dataBytes); normalized {
				dataBytes = normalizedData
				trimmedData = strings.TrimSpace(string(normalizedData))
				line = "data: " + string(normalizedData)
			}
			if normalizedData, normalized := wire.NormalizeCompletedImageGenerationStatus(dataBytes); normalized {
				dataBytes = normalizedData
				trimmedData = strings.TrimSpace(string(normalizedData))
				line = "data: " + string(normalizedData)
			}
			if trimmedData != "[DONE]" {
				restoredData, restoreErr := options.RestoreNamespace(dataBytes)
				if restoreErr != nil {
					return resultWithUsage(), fmt.Errorf("restore OpenAI passthrough namespace response: %w", restoreErr)
				}
				restoredData = options.RestoreToolNames(restoredData, rawEventType)
				if !bytes.Equal(restoredData, dataBytes) {
					dataBytes = restoredData
					trimmedData = strings.TrimSpace(string(restoredData))
					line = "data: " + string(restoredData)
				}
			}
			eventType := wire.EffectiveOpenAISSEEventType(dataBytes, rawEventType)
			// 按规范化后的类型重新裁决，清除旧 done 事件头留下的抑制标记。
			suppressCurrentEvent = codexFailureTerminal && (eventType == "error" || (sawBareError && !sawResponseFailed && eventType != "response.failed"))
			if !capacityFailoverSuppressedLogged && options.NativeOpenAI &&
				(eventType == "error" || eventType == "response.failed") &&
				options.ClientOutputStarted(clientOutputStarted) &&
				IsOpenAIUpstreamCapacityShedEvent(dataBytes) {
				options.CapacitySuppressed(upstreamRequestID, eventType)
				capacityFailoverSuppressedLogged = true
			}
			cyberHit := false
			if eventType == "response.failed" || eventType == "error" {
				if codexFailureTerminal && eventType == "error" {
					sawBareError = true
					bareErrorPayload = append(bareErrorPayload[:0], dataBytes...)
					suppressCurrentEvent = true
				} else if codexFailureTerminal && eventType == "response.failed" {
					sawResponseFailed = true
				}
				responseFailedPending = !codexFailureTerminal || eventType == "response.failed"
				failedMessage = ExtractOpenAISSEErrorMessage(dataBytes)
				if failedMessage == "" {
					failedMessage = "Upstream response failed"
				}
				// response.failed 自带上游已消耗的 usage（input token 通常已扣）；必须先解析
				// 再打 cyber 标记，否则 mark 记到的是解析前的 0，导致流式 cyber 按 0 token 计费
				// 而漏记真实用量。对齐 WS V2 / Chat 流式路径（均先解析 usage 再 Mark）。
				wire.ParseSSEUsageBytesWithType(dataBytes, eventType, usage)
				if hit, code, msg := DetectOpenAICyberPolicy(dataBytes); hit {
					cyberHit = true
					options.MarkCyber(CyberObservation{
						Code:           code,
						Message:        msg,
						Body:           options.TruncateString(string(dataBytes), 4096),
						UpstreamStatus: http.StatusOK,
						UpstreamInTok:  usage.InputTokens,
						UpstreamOutTok: usage.OutputTokens,
					})
				}
				outputStarted := options.ClientOutputStarted(clientOutputStarted)
				if !outputStarted && !cyberHit {
					if compactErr := options.CompactFallback(dataBytes, failedMessage); compactErr != nil {
						return resultWithUsage(), compactErr
					}
				}
				if outputStarted && !cyberHit {
					if codexFailureTerminal && eventType == "error" {
						// Wait for the authoritative response.failed before mutating
						// provider health; EOF synthesis applies the pending effect.
						bareErrorProviderSideEffectsPending = true
					} else {
						options.TerminalSideEffects(dataBytes, failedMessage, resp.Header, mappedModel)
						bareErrorProviderSideEffectsPending = false
					}
				}
				if !outputStarted {
					shouldFailover := false
					if !cyberHit {
						if eventType == "error" {
							shouldFailover = OpenAIStreamErrorEventShouldFailover(dataBytes, failedMessage)
						} else {
							shouldFailover = OpenAIStreamFailedEventShouldFailover(dataBytes, failedMessage)
						}
					}
					if shouldFailover {
						return resultWithUsage(),
							options.PassthroughFailoverWithModel(upstreamRequestID, dataBytes, failedMessage, mappedModel)
					}
					if !cyberHit && !sawBareError {
						if status, errType, errMsg, matched := options.ErrorRule(dataBytes, failedMessage); matched {
							// 命中透传规则也要记录 ops 上游错误事件（对齐 CC/Messages 与
							// antigravity 先例），否则透传命中的 failed 在监控中不可见。
							options.RecordError(upstreamRequestID, "http_error", dataBytes, failedMessage)
							options.MarkCommitted()
							c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
							c.JSON(status, map[string]any{
								"error": map[string]any{
									"type":    errType,
									"message": errMsg,
								},
							})
							return resultWithUsage(), fmt.Errorf("upstream response failed: passthrough rule matched message=%s", errMsg)
						}
					}
				}
				forceFlushFailedEvent = true
				sawFailedEvent = true
			}
			if trimmedData == "[DONE]" {
				sawDone = true
				terminalEventType = "[DONE]"
			}
			if options.OpenAIStreamEventIsTerminalWithType(trimmedData, eventType) {
				sawTerminalEvent = true
				if trimmedData != "[DONE]" {
					terminalEventType = eventType
				}
			}
			if responseID == "" {
				responseID = wire.ExtractOpenAIResponseIDFromJSONBytes(dataBytes)
			}
			var responseEvent wire.ResponsesStreamEvent
			if err := json.Unmarshal(dataBytes, &responseEvent); err == nil {
				responseAccumulator.ProcessEvent(&responseEvent)
			}
			if imageOutput, ok := wire.ExtractImageGenerationOutputFromSSEData(dataBytes, streamSeenImages); ok {
				streamImageOutputs = append(streamImageOutputs, imageOutput)
			}
			streamDoneItems.Observe(dataBytes)
			if normalizedData, normalized := bridge.NormalizeResponsesStreamingTerminalOutput(dataBytes, responseAccumulator, streamDoneItems, streamImageOutputs); normalized {
				dataBytes = normalizedData
				data = string(normalizedData)
				trimmedData = strings.TrimSpace(data)
				line = "data: " + data
				eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			}
			imageCounter.AddSSEData(dataBytes)
			if sanitizedData, sanitized := SanitizeOpenAIResponseFailedEventForClient(
				dataBytes,
				eventType,
				options.ClientOutputStarted(clientOutputStarted),
			); sanitized {
				dataBytes = sanitizedData
				trimmedData = strings.TrimSpace(string(sanitizedData))
				line = "data: " + string(sanitizedData)
			}
			lineStartsClientOutput = forceFlushFailedEvent || OpenAIStreamDataStartsClientOutput(trimmedData, eventType)
			if lineStartsClientOutput && trimmedData != "[DONE]" && !wire.OpenAIStreamEventTypeIsTerminal(eventType) {
				semanticOutputSeen = true
			}
			// 透传流在写出前也要识别空 completed，确保仍可安全切换提供商。
			if (eventType == "response.completed" || eventType == "response.done") &&
				!sawFailedEvent && !semanticOutputSeen && !clientOutputStarted &&
				wire.OpenAIResponsesCompletedEventIsEmpty(dataBytes, usage) {
				return resultWithUsage(), options.EmptyCompleted(upstreamRequestID)
			}
			startsVisibleOutput := wire.StreamDataStartsVisibleOutput(trimmedData, eventType)
			if startsVisibleOutput {
				options.MarkTime(StreamTimeVisible)
			}
			if firstTokenMs == nil && options.OpenAIStreamDataStartsTTFT(trimmedData, eventType, forceFlushFailedEvent, ttftMode) {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
			wire.ParseSSEUsageBytesWithType(dataBytes, eventType, usage)
			if pendingLifecycleHeader != "" {
				header := pendingLifecycleHeader
				if eventType != strings.TrimSpace(pendingSSEEventType) {
					header = "event: " + eventType
				}
				line = header + "\n" + line
				pendingLifecycleHeader = ""
			}
			suppressCurrentEvent = suppressCurrentEvent || duplicateTerminal
		}
		if line == "" {
			pendingLifecycleHeader = ""
			pendingSSEEventType = ""
			if suppressCurrentEvent {
				suppressCurrentEvent = false
				responseFailedPending = false
				continue
			}
		}

		if !clientDisconnected && !failureDelivered && !suppressCurrentEvent {
			if !clientOutputStarted && !lineStartsClientOutput {
				pendingLines = append(pendingLines, line)
				continue
			}
			// 内容输出开始时停止心跳。停止操作可重复调用，并等待心跳 goroutine 完成写入，
			// 此后 ResponseWriter 由本循环独占。
			if !clientOutputStarted {
				stopKeepalive()
			}
			if !clientOutputStarted && len(pendingLines) > 0 {
				if !writePendingLines() {
					continue
				}
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				clientDisconnected = true
				options.Logf("[OpenAI passthrough] Client disconnected during streaming, continue draining upstream for usage: provider=%d", options.ProviderID)
			} else {
				clientOutputStarted = true
				flushPending = true
				if line == "" {
					flushPendingOutput()
				}
			}
		}
		if line == "" && responseFailedPending {
			responseFailedPending = false
			failureDelivered = true
		}
	}
	ensureResponseFailedTerminal()
	if err := documentScanner.Err(); err != nil {
		if (sawDone || sawTerminalEvent) && !sawFailedEvent {
			options.ClearDisconnect()
			return resultWithUsage(), nil
		}
		if sawFailedEvent {
			err := fmt.Errorf("upstream response failed: %s", failedMessage)
			return resultWithUsage(), options.WrapOpenAIUpstreamWarningIfCyber(resp.StatusCode, failedPayload, failedMessage, err)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", err)
		}
		if errors.Is(err, bufio.ErrTooLong) {
			options.Logf("[OpenAI passthrough] SSE line too long: provider=%d max_size=%d error=%v", options.ProviderID, maxLineSize, err)
			return resultWithUsage(), err
		}
		if !options.ClientOutputStarted(clientOutputStarted) {
			msg := "OpenAI stream disconnected before completion"
			if errText := strings.TrimSpace(err.Error()); errText != "" {
				msg += ": " + errText
			}
			return resultWithUsage(),
				options.Failover(upstreamRequestID, nil, msg)
		}
		if clientDisconnected {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", err)
		}
		options.RecordDisconnect(err, upstreamRequestID)
		options.Logf("[OpenAI passthrough] 流读取异常中断: provider=%d request_id=%s err=%v",
			options.ProviderID,
			upstreamRequestID,
			err,
		)
		return resultWithUsage(), fmt.Errorf("stream read error: %w", err)
	}
	if sawFailedEvent {
		err := fmt.Errorf("upstream response failed: %s", failedMessage)
		return resultWithUsage(), options.WrapOpenAIUpstreamWarningIfCyber(resp.StatusCode, failedPayload, failedMessage, err)
	}
	if !clientDisconnected && !sawDone && !sawTerminalEvent && ctx.Err() == nil {
		options.MissingTerminal(upstreamRequestID)
		if !options.ClientOutputStarted(clientOutputStarted) {
			return resultWithUsage(),
				options.Failover(upstreamRequestID, nil, "OpenAI stream ended before a terminal event")
		}
		options.RecordDisconnect(errors.New("stream ended before terminal event"), upstreamRequestID)
		return resultWithUsage(), errors.New("stream usage incomplete: missing terminal event")
	}
	if (sawDone || sawTerminalEvent) && !sawFailedEvent {
		options.ClearDisconnect()
	}
	options.MissingUsage(resp, usage, terminalEventType, clientDisconnected)

	return resultWithUsage(), nil
}

// ReadPassthroughNonStreaming 读取并透传非流式响应。
func ReadPassthroughNonStreaming(ctx context.Context, resp *http.Response, sink upstream.OutputSink, options PassthroughOptions, originalModel, mappedModel string) (*NonStreamingResult, error) {
	body, err := options.NonStream.ReadBody(resp.Body)
	if err != nil {
		return nil, err
	}

	if IsEventStreamResponse(resp.Header) {
		options.NonStream.ObserveSSE(string(body))
		return ReadPassthroughSSEAsJSON(resp, sink, options, body, originalModel, mappedModel)
	}
	options.NonStream.ObserveTier(body)

	usage := &wire.ForwardUsage{}
	usageParsed := false
	if len(body) > 0 {
		if parsedUsage, ok := wire.ExtractOpenAIUsageFromJSONBytes(body); ok {
			*usage = parsedUsage
			usageParsed = true
		}
	}
	if !usageParsed {
		usage = wire.ParseSSEUsageFromBody(string(body))
	}
	options.MissingUsage(resp, usage, "json", false)

	c := upstream.NewOutputContext(sink)
	options.Headers(c.Writer.Header(), resp.Header)

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	if originalModel != "" && mappedModel != "" && originalModel != mappedModel {
		body = wire.ReplaceModelInResponseBody(body, mappedModel, originalModel)
	}
	body, err = options.RestoreNamespace(body)
	if err != nil {
		return nil, fmt.Errorf("restore OpenAI passthrough namespace response: %w", err)
	}
	body = options.NonStream.RestoreToolNames(body)
	body, err = options.NonStream.RestoreOpenAITools(body)
	if err != nil {
		return nil, fmt.Errorf("restore OpenAI Responses client tools: %w", err)
	}
	if !options.NonStream.WriteCompactBridge(resp.StatusCode, body) {
		c.Data(resp.StatusCode, contentType, body)
	}
	return &NonStreamingResult{
		Usage:            usage,
		ResponseID:       wire.ExtractOpenAIResponseIDFromJSONBytes(body),
		ImageCount:       wire.CountOpenAIResponseImageOutputsFromJSONBytes(body),
		ImageOutputSizes: wire.CollectOpenAIResponseImageOutputSizesFromJSONBytes(body),
	}, nil
}

// ReadPassthroughSSEAsJSON 收集 SSE 终态并输出 JSON 响应。
func ReadPassthroughSSEAsJSON(resp *http.Response, sink upstream.OutputSink, options PassthroughOptions, body []byte, originalModel, mappedModel string) (*NonStreamingResult, error) {
	bodyText := string(body)
	terminalType, terminalPayload, terminalOK := wire.ExtractOpenAISSETerminalEvent(bodyText)
	if terminalOK && (terminalType == "response.failed" || terminalType == "error") {
		msg := ExtractOpenAISSEErrorMessage(terminalPayload)
		if msg == "" {
			msg = "Upstream compact response failed"
		}
		if compactErr := options.CompactFallback(terminalPayload, msg); compactErr != nil {
			return nil, compactErr
		}
		if failoverErr := options.NonStream.TerminalFailover(resp, terminalType, terminalPayload, msg, mappedModel); failoverErr != nil {
			return nil, failoverErr
		}
		return nil, options.NonStream.ProtocolError(resp, msg)
	}
	finalResponse, ok := wire.ExtractCodexFinalResponse(bodyText)

	usage := wire.ParseSSEUsageFromBody(bodyText)
	if ok {
		if parsedUsage, parsed := wire.ExtractOpenAIUsageFromJSONBytes(finalResponse); parsed {
			*usage = parsedUsage
		}

		if len(gjson.GetBytes(finalResponse, "output").Array()) == 0 {
			if outputJSON, reconstructed := bridge.ReconstructResponseOutputFromSSE(bodyText); reconstructed {
				if patched, err := sjson.SetRawBytes(finalResponse, "output", outputJSON); err == nil {
					finalResponse = patched
				}
			}
		}
		finalResponse = options.NonStream.SupplementCompaction(finalResponse, bodyText)
		body = finalResponse
		if originalModel != "" && mappedModel != "" && originalModel != mappedModel {
			body = wire.ReplaceModelInResponseBody(body, mappedModel, originalModel)
		}

		body = options.NonStream.CorrectToolCalls(body)
		restoredBody, restoreErr := options.RestoreNamespace(body)
		if restoreErr != nil {
			return nil, fmt.Errorf("restore OpenAI passthrough namespace response: %w", restoreErr)
		}
		restoredBody = options.NonStream.RestoreToolNames(restoredBody)
		body = restoredBody
		body, restoreErr = options.NonStream.RestoreOpenAITools(body)
		if restoreErr != nil {
			return nil, fmt.Errorf("restore OpenAI Responses client tools: %w", restoreErr)
		}
	} else {
		if originalModel != "" && mappedModel != "" && originalModel != mappedModel {
			bodyText = wire.ReplaceModelInSSEBody(bodyText, mappedModel, originalModel)
		}
		body = []byte(bodyText)
	}

	c := upstream.NewOutputContext(sink)
	options.Headers(c.Writer.Header(), resp.Header)
	options.MissingUsage(resp, usage, terminalType, false)

	contentType := "application/json; charset=utf-8"
	if !ok {
		contentType = resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "text/event-stream"
		}
	}
	if !options.NonStream.WriteCompactBridge(resp.StatusCode, body) {
		c.Data(resp.StatusCode, contentType, body)
	}

	return &NonStreamingResult{
		Usage:            usage,
		ResponseID:       wire.ExtractOpenAIResponseIDFromJSONBytes(body),
		ImageCount:       wire.CountOpenAIImageOutputsFromSSEBody(bodyText),
		ImageOutputSizes: wire.CollectOpenAIImageOutputSizesFromSSEBody(bodyText),
	}, nil
}
