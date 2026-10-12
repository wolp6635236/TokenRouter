package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	wire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

const (
	StreamTimeFlush   StreamTime = "first_downstream_flush"
	StreamTimeVisible StreamTime = "first_visible_output"
	StreamTimeData    StreamTime = "first_sse_data"
)

type StreamTime string

type CyberObservation struct {
	Code           string
	Message        string
	Body           string
	UpstreamStatus int
	UpstreamInTok  int
	UpstreamOutTok int
}

// StreamOptions 配置本次流读取的超时、输出和观测回调。
type StreamOptions struct {
	ProviderID                                                           int64
	NativeOpenAI, StageFirstOutput, CodexFailureTerminal, GrokIdlePolicy bool
	FirstOutputTimeout, StreamInterval, KeepaliveInterval                time.Duration
	MaxLineSize                                                          int
	TTFTMode                                                             func() string
	PrepareHeaders                                                       func(http.Header, bool, http.Header) http.Header
	StagedHeadersCommitted                                               func(http.Header)
	Observe                                                              func([]byte, string)
	Logf                                                                 func(string, ...any)
	MarkTime                                                             func(StreamTime)
	IsCommitted                                                          func() bool
	MarkCommitted                                                        func()
	ClientOutputStarted                                                  func(bool) bool
	ClearDisconnect                                                      func()
	RecordDisconnect                                                     func(error, string)
	TerminalSideEffects                                                  func([]byte, string, http.Header, string)
	Failover                                                             func(string, []byte, string) error
	FailoverWithModel                                                    func(string, []byte, string, string, http.Header) error
	MarkSafeFailover                                                     func(error)
	RecordError                                                          func(string, string, []byte, string)
	CompactFallback                                                      func([]byte, string) error
	ErrorRule                                                            func([]byte, string) (int, string, string, bool)
	CapacitySuppressed                                                   func(string, string)
	MarkCyber                                                            func(CyberObservation)
	ToolCorrector                                                        *CodexToolCorrector
	RestoreClientTools, RestoreNamespace                                 func([]byte) ([]byte, error)
	RestoreToolNames                                                     func([]byte, string) []byte
	EmptyCompleted                                                       func(string) error
	CountSearch                                                          func([]byte, map[string]struct{}) int
	StreamTimeout                                                        func(string)
	IdleCooldown                                                         func()
	IdleFailover                                                         func(time.Duration) error
	FirstOutputError                                                     func(time.Time, string, string, time.Duration, string, http.Header) error
	KeepaliveBytes                                                       func(int)
	BuildOpenAIResponseFailedSSE                                         func(string, string, []byte, string) string
	WrapOpenAIUpstreamWarningIfCyber                                     func(int, []byte, string, error) error
	TruncateString                                                       func(string, int) string
	OpenAIStreamDataStartsTTFT                                           func(string, string, bool, string) bool
	OpenAIStreamEventIsTerminalWithType                                  func(string, string) bool
}

// StreamingResult streaming response result.
type StreamingResult struct {
	// 分别记录内容输出、用量是否存在、HTTP 提交和重试窗口关闭状态。
	Served, HasUsage, HttpCommitted, RetryCommitted, ClientDisconnected, ObservedOnly bool
	FirstSemanticOutput                                                               *time.Duration
	Usage                                                                             *wire.ForwardUsage
	FirstTokenMs                                                                      *int
	ResponseID                                                                        string
	ImageCount                                                                        int
	ImageOutputSizes                                                                  []string
	SearchCount                                                                       int
}

func ReadStreamingResponse(ctx context.Context, resp *http.Response, c *upstream.OutputContext, options StreamOptions, startTime time.Time, originalModel, mappedModel, reasoningEffort string) (observed *StreamingResult, failure error) {
	firstOutputTimeout := options.FirstOutputTimeout
	guardFirstOutput := firstOutputTimeout > 0
	stageFirstOutput := options.StageFirstOutput
	attemptResponseHeaders := options.PrepareHeaders(resp.Header, stageFirstOutput, c.Writer.Header())

	// Set SSE response headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Pass through other headers
	if !stageFirstOutput && resp.Header.Get("x-request-id") != "" {
		v := resp.Header.Get("x-request-id")
		c.Header("x-request-id", v)
	}
	applyAttemptResponseHeaders := func() {
		if !stageFirstOutput || len(attemptResponseHeaders) == 0 || c.Writer.Written() {
			return
		}
		for key, values := range attemptResponseHeaders {
			for _, value := range values {
				c.Writer.Header().Add(key, value)
			}
		}
		options.StagedHeadersCommitted(attemptResponseHeaders)
		// 这些 header 描述网关自己的 SSE 流，跨提供商尝试保持稳定，优先级高于上游值。
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
	}

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}
	maxLineSize := options.MaxLineSize
	var firstTokenMs *int
	ttftMode := options.TTFTMode()
	firstOutputProgressObserved := false
	bufferedWriter := bufio.NewWriterSize(w, 4*1024)
	var firstOutputStage *OpenAIFirstOutputStage
	if stageFirstOutput {
		firstOutputStage = NewDefaultOpenAIFirstOutputStage()
		defer func() {
			if err := firstOutputStage.Close(); err != nil {
				options.Logf("OpenAI first-output staging cleanup failed: provider=%d model=%s error=%v", options.ProviderID, originalModel, err)
			}
		}()
	}
	writePendingString := func(value string) (int, error) {
		if firstOutputStage != nil && !firstOutputStage.Closed() {
			return firstOutputStage.WriteString(value)
		}
		return bufferedWriter.WriteString(value)
	}
	pendingBytes := func() int64 {
		if firstOutputStage != nil && !firstOutputStage.Closed() {
			return firstOutputStage.Buffered()
		}
		return int64(bufferedWriter.Buffered())
	}
	flushBuffered := func() error {
		// 缓冲区有数据时，刷新才算向下游写出响应。
		hadPendingBytes := pendingBytes() > 0
		if firstOutputStage != nil && !firstOutputStage.Closed() {
			if err := firstOutputStage.CommitTo(w); err != nil {
				return err
			}
		} else {
			if err := bufferedWriter.Flush(); err != nil {
				return err
			}
		}
		flusher.Flush()
		if hadPendingBytes {
			options.MarkTime(StreamTimeFlush)
		}
		return nil
	}

	usage := &wire.ForwardUsage{}
	hasObservedUsage := false
	var firstSemanticOutput *time.Duration
	imageCounter := wire.NewOpenAIImageOutputCounter()
	responseID := ""
	var firstOutputScanGuard atomic.Bool
	firstOutputScanGuard.Store(stageFirstOutput)
	scanner := bufio.NewScanner(resp.Body)
	scanBuf := httpclient.GetSSEScannerBuf64K()
	scanner.Buffer(scanBuf[:0], maxLineSize)
	if stageFirstOutput {
		scanner.Split(OpenAIFirstOutputDynamicScanLines(&firstOutputScanGuard))
	}
	documentScanner := wire.NewSSEJSONDocumentScanner(scanner)

	streamInterval := options.StreamInterval
	// 按上游数据到达间隔监控超时，下游写入耗时单独处理
	var intervalTicker *time.Ticker
	if streamInterval > 0 {
		intervalTicker = time.NewTicker(streamInterval)
		defer intervalTicker.Stop()
	}
	var intervalCh <-chan time.Time
	if intervalTicker != nil {
		intervalCh = intervalTicker.C
	}

	keepaliveInterval := options.KeepaliveInterval
	// 下游 keepalive 仅用于防止代理空闲断开
	var keepaliveTicker *time.Ticker
	if keepaliveInterval > 0 {
		keepaliveTicker = time.NewTicker(keepaliveInterval)
		defer keepaliveTicker.Stop()
	}
	var keepaliveCh <-chan time.Time
	if keepaliveTicker != nil {
		keepaliveCh = keepaliveTicker.C
	}

	var firstOutputTimer *time.Timer
	var firstOutputCh <-chan time.Time
	if firstOutputTimeout > 0 {
		remaining := time.Until(startTime.Add(firstOutputTimeout))
		if remaining <= 0 {
			remaining = time.Nanosecond
		}
		firstOutputTimer = time.NewTimer(remaining)
		firstOutputCh = firstOutputTimer.C
		defer firstOutputTimer.Stop()
	}
	stopFirstOutputTimer := func() {
		if firstOutputTimer == nil {
			return
		}
		if !firstOutputTimer.Stop() {
			select {
			case <-firstOutputTimer.C:
			default:
			}
		}
		firstOutputTimer = nil
		firstOutputCh = nil
	}
	// Track downstream writes separately from upstream reads: pre-output failover
	// can buffer response.created / response.in_progress, so keepalive must be
	// based on downstream idle time.
	lastDownstreamWriteAt := time.Now()

	// 仅发送一次错误事件，避免多次写入导致协议混乱。
	// 注意：OpenAI `/v1/responses` streaming 事件必须符合 OpenAI Responses schema；
	// 否则下游 SDK（例如 OpenCode）会因为类型校验失败而报错。
	errorEventSent := false
	clientDisconnected := false // 客户端断开后继续 drain 上游以收集 usage
	sawTerminalEvent := false
	sawFailedEvent := false
	sawBareError := false
	sawResponseFailed := false
	responsesSemanticOutputSeen := false
	capacityFailoverSuppressedLogged := false
	failedMessage := ""
	var failedPayload []byte
	clientOutputStarted := false
	codexFailureTerminal := options.CodexFailureTerminal
	upstreamRequestID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	var streamEarlyErr error
	terminalFailurePending := false
	failureDelivered := false
	suppressCurrentEvent := false
	var bareErrorPayload []byte
	bareErrorProviderSideEffectsPending := false
	pendingSSEEventType := ""
	eventInProgress := false
	eventStartsClientOutput := false
	eventStartsVisibleOutput := false
	eventStartsTTFTOutput := false
	eventShouldFlush := false
	handlePendingWriteError := func(err error) {
		if firstOutputStage != nil && !firstOutputStage.Closed() {
			message := "OpenAI first-output staging failed"
			if errors.Is(err, ErrOpenAIFirstOutputStageLimit) {
				message = "OpenAI first-output staging limit exceeded"
			}
			options.Logf("%s: provider=%d model=%s error=%v", message, options.ProviderID, originalModel, err)
			failoverErr := options.Failover(upstreamRequestID, nil, message)
			options.MarkSafeFailover(failoverErr)
			streamEarlyErr = failoverErr
			_ = resp.Body.Close()
			return
		}
		clientDisconnected = true
		options.Logf("Client disconnected during streaming, continuing to drain upstream for billing")
	}
	completeGuardedEvent := func(queueDrained bool) {
		completedProgressEvent := eventStartsClientOutput
		completedVisibleEvent := eventStartsVisibleOutput
		completedTTFTEvent := eventStartsTTFTOutput
		shouldFlush := eventShouldFlush || (queueDrained && clientOutputStarted)
		eventInProgress = false
		if !clientDisconnected {
			if completedProgressEvent {
				applyAttemptResponseHeaders()
			}
			if shouldFlush {
				if err := flushBuffered(); err != nil {
					clientDisconnected = true
					options.Logf("Client disconnected during streaming flush, continuing to drain upstream for billing")
				} else {
					clientOutputStarted = true
					lastDownstreamWriteAt = time.Now()
				}
			}
		}
		if completedProgressEvent && !firstOutputProgressObserved {
			firstOutputScanGuard.Store(false)
			firstOutputProgressObserved = true
			stopFirstOutputTimer()
		}
		if completedVisibleEvent && firstTokenMs == nil {
			options.MarkTime(StreamTimeVisible)
		}
		if completedTTFTEvent && firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		eventStartsClientOutput = false
		eventStartsVisibleOutput = false
		eventStartsTTFTOutput = false
		eventShouldFlush = false
	}
	sendErrorEvent := func(reason string) {
		if errorEventSent || clientDisconnected {
			return
		}
		errorEventSent = true
		payload := `{"type":"error","sequence_number":0,"error":{"type":"upstream_error","message":` + strconv.Quote(reason) + `,"code":` + strconv.Quote(reason) + `}}`
		if err := flushBuffered(); err != nil {
			clientDisconnected = true
			return
		}
		if _, err := writePendingString("data: " + payload + "\n\n"); err != nil {
			clientDisconnected = true
			return
		}
		if err := flushBuffered(); err != nil {
			clientDisconnected = true
			return
		}
		clientOutputStarted = true
		lastDownstreamWriteAt = time.Now()
	}

	needModelReplace := originalModel != mappedModel
	responseAccumulator := bridge.NewBufferedResponseAccumulator()
	streamDoneItems := bridge.NewResponsesStreamOutputItems()
	streamImageOutputs := make([]json.RawMessage, 0, 1)
	streamSeenImages := make(map[string]struct{})
	searchCounter := 0
	// 对 SSE 事件中的搜索工具调用去重，例如 item.done 与 response.completed
	// 可能包含同一 call_id，重复统计会使附加费接近翻倍。
	streamSearchSeen := make(map[string]struct{})
	resultWithUsage := func() *StreamingResult {
		return &StreamingResult{
			Served:              firstSemanticOutput != nil,
			HasUsage:            hasObservedUsage,
			HttpCommitted:       c.Writer.Written(),
			RetryCommitted:      options.IsCommitted(),
			ClientDisconnected:  clientDisconnected,
			FirstSemanticOutput: firstSemanticOutput,

			Usage: usage,

			FirstTokenMs: firstTokenMs,

			ResponseID: responseID,

			ImageCount: imageCounter.Count(),

			ImageOutputSizes: imageCounter.Sizes(),

			SearchCount: searchCounter,
		}
	}
	// 读取失败时返回已收集的用量，并标记 ObservedOnly。
	defer func() {
		if observed == nil && failure != nil {
			observed = resultWithUsage()
			observed.ObservedOnly = true
		}
	}()
	flushPending := func(disconnectMessage string) {
		if clientDisconnected || pendingBytes() == 0 {
			return
		}
		if err := flushBuffered(); err != nil {
			clientDisconnected = true
			options.Logf("%s", disconnectMessage)
			return
		}
		clientOutputStarted = true
		lastDownstreamWriteAt = time.Now()
	}
	synthesizeBareErrorFailure := func() bool {
		if !codexFailureTerminal || !sawBareError || sawResponseFailed || clientDisconnected {
			return false
		}
		if bareErrorProviderSideEffectsPending {
			options.TerminalSideEffects(bareErrorPayload, failedMessage, resp.Header, mappedModel)
			bareErrorProviderSideEffectsPending = false
		}
		applyAttemptResponseHeaders()
		if _, err := writePendingString(options.BuildOpenAIResponseFailedSSE(responseID, originalModel, bareErrorPayload, failedMessage)); err != nil {
			handlePendingWriteError(err)
			return false
		}
		failedPayload = append(failedPayload[:0], bareErrorPayload...)
		sawResponseFailed = true
		sawFailedEvent = true
		sawTerminalEvent = true
		failureDelivered = true
		return true
	}
	finalizeStream := func() (*StreamingResult, error) {
		if stageFirstOutput && eventInProgress {
			// 即使没有结尾空行，EOF 也会派发最后一个 SSE 事件。
			completeGuardedEvent(true)
		}
		synthesizeBareErrorFailure()
		if sawTerminalEvent && !sawFailedEvent {
			options.ClearDisconnect()
		}
		if !sawTerminalEvent && !options.ClientOutputStarted(clientOutputStarted) && !eventShouldFlush {
			return resultWithUsage(), options.Failover(
				upstreamRequestID,
				nil,
				"OpenAI stream ended before a terminal event",
			)
		}
		flushPending("Client disconnected during final flush, returning collected usage")
		if !sawTerminalEvent {
			if options.ClientOutputStarted(clientOutputStarted) && !clientDisconnected {
				options.RecordDisconnect(errors.New("stream ended before terminal event"), upstreamRequestID)
			}
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: missing terminal event")
		}
		if sawFailedEvent {
			err := fmt.Errorf("upstream response failed: %s", failedMessage)
			return resultWithUsage(), options.WrapOpenAIUpstreamWarningIfCyber(resp.StatusCode, failedPayload, failedMessage, err)
		}
		return resultWithUsage(), nil
	}
	handleScanErr := func(scanErr error) (*StreamingResult, error, bool) {
		if scanErr == nil {
			return nil, nil, false
		}
		if errors.Is(scanErr, ErrOpenAIFirstOutputScannerLimit) && !firstOutputProgressObserved {
			options.Logf("SSE token exceeded guarded first-output limit: provider=%d limit=%d error=%v", options.ProviderID, OpenAIFirstOutputStageMaxBytes+OpenAIFirstOutputScannerFramingAllowance, scanErr)
			failoverErr := options.Failover(upstreamRequestID, nil,
				"OpenAI SSE line exceeds guarded first-output limit",
			)
			options.MarkSafeFailover(failoverErr)
			return resultWithUsage(), failoverErr, true
		}
		if errors.Is(scanErr, bufio.ErrTooLong) && stageFirstOutput && !firstOutputProgressObserved {
			options.Logf("SSE line too long before first output: provider=%d max_size=%d error=%v", options.ProviderID, maxLineSize, scanErr)
			failoverErr := options.Failover(upstreamRequestID, nil,
				"OpenAI SSE line exceeds guarded first-output limit",
			)
			options.MarkSafeFailover(failoverErr)
			return resultWithUsage(), failoverErr, true
		}
		if sawTerminalEvent {
			if !sawFailedEvent {
				options.ClearDisconnect()
				options.Logf("Upstream scan ended after terminal event: %v", scanErr)
			}
			result, err := finalizeStream()
			return result, err, true
		}
		// 客户端断开/取消请求时，上游读取往往会返回 context canceled。
		// /v1/responses 的 SSE 事件必须符合 OpenAI 协议；这里不注入自定义 error event，避免下游 SDK 解析失败。
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			if eventShouldFlush {
				flushPending("Client disconnected during canceled stream flush, returning collected usage")
			}
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", scanErr), true
		}
		if errors.Is(scanErr, bufio.ErrTooLong) {
			options.Logf("SSE line too long: provider=%d max_size=%d error=%v", options.ProviderID, maxLineSize, scanErr)
			sendErrorEvent("response_too_large")
			return resultWithUsage(), scanErr, true
		}
		if !options.ClientOutputStarted(clientOutputStarted) && !eventShouldFlush {
			msg := "OpenAI stream disconnected before completion"
			if errText := strings.TrimSpace(scanErr.Error()); errText != "" {
				msg += ": " + errText
			}
			return resultWithUsage(), options.Failover(upstreamRequestID, nil, msg), true
		}
		// 客户端已断开时，上游出错仅影响体验，不影响计费；返回已收集 usage
		if clientDisconnected {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", scanErr), true
		}
		options.RecordDisconnect(scanErr, upstreamRequestID)
		sendErrorEvent("stream_read_error")
		return resultWithUsage(), fmt.Errorf("stream read error: %w", scanErr), true
	}
	var lifecycleNormalizer wire.ResponseLifecycleNormalizer
	pendingLifecycleHeader := ""
	processSSELine := func(line string, queueDrained bool) {
		if streamEarlyErr != nil {
			return
		}
		if eventType, ok := wire.ExtractSSEEventLine(line); ok {
			pendingSSEEventType = eventType
			eventType = strings.TrimSpace(eventType)
			suppressCurrentEvent = codexFailureTerminal && (eventType == "error" || (sawBareError && !sawResponseFailed && eventType != "response.failed"))
			if wire.IsResponseLifecycleEvent(eventType) {
				// 等 data 到达后再写事件头，确保 done 改名和重复终态过滤覆盖整个事件。
				pendingLifecycleHeader = line
				return
			}
		}
		// Extract data from SSE line (supports both "data: " and "data:" formats)
		if data, ok := wire.ExtractSSEDataLine(line); ok {
			options.MarkTime(StreamTimeData)
			dataBytes := []byte(data)
			eventType := wire.EffectiveOpenAISSEEventType(dataBytes, pendingSSEEventType)
			options.Observe(dataBytes, eventType)
			normalizedData, normalizedType, duplicateTerminal := lifecycleNormalizer.Normalize(dataBytes, eventType)
			if !bytes.Equal(normalizedData, dataBytes) {
				dataBytes = normalizedData
				data = string(dataBytes)
				line = "data: " + data
			}
			eventType = normalizedType
			if codexFailureTerminal && sawBareError && !sawResponseFailed &&
				eventType == "response.completed" {
				// 只有规范化后确认成功的终态才能覆盖此前暂存的独立错误。
				sawBareError = false
				sawFailedEvent = false
				terminalFailurePending = false
				suppressCurrentEvent = false
				bareErrorPayload = nil
				bareErrorProviderSideEffectsPending = false
				failedMessage = ""
			}
			// 失败终态需要清除旧事件头的抑制状态，避免误丢收尾事件。
			suppressCurrentEvent = codexFailureTerminal && (eventType == "error" || (sawBareError && !sawResponseFailed && eventType != "response.failed"))
			// 使用规范化后的类型判定终态，兼容 data 中缺少 type 的生命周期事件。
			if options.OpenAIStreamEventIsTerminalWithType(data, eventType) {
				sawTerminalEvent = true
			}
			if responseID == "" {
				responseID = wire.ExtractOpenAIResponseIDFromJSONBytes(dataBytes)
			}
			forceFlushFailedEvent := false
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
				failedMessage = ExtractOpenAISSEErrorMessage(dataBytes)
				if failedMessage == "" {
					failedMessage = "Upstream response failed"
				}
				// response.failed 自带上游已消耗的 usage（input token 通常已扣）；必须先解析
				// 再打 cyber 标记，否则 mark 记到的是解析前的 0，导致流式 cyber 按 0 token 计费
				// 而漏记真实用量。对齐 WS V2 / Chat 流式路径（均先解析 usage 再 Mark）。
				hasObservedUsage = wire.ParseSSEUsageBytesWithType(dataBytes, eventType, usage) || hasObservedUsage
				if hit, code, msg := DetectOpenAICyberPolicy(dataBytes); hit {
					cyberHit = true
					options.MarkCyber(CyberObservation{
						Code: code,

						Message: msg,

						Body: options.TruncateString(string(dataBytes), 4096),

						UpstreamStatus: http.StatusOK,

						UpstreamInTok: usage.InputTokens,

						UpstreamOutTok: usage.OutputTokens,
					})
				}
				outputStarted := options.ClientOutputStarted(clientOutputStarted)
				if !outputStarted && !cyberHit {
					if compactErr := options.CompactFallback(dataBytes, failedMessage); compactErr != nil {
						sawFailedEvent = true
						streamEarlyErr = compactErr
						return
					}
				}
				if outputStarted && !cyberHit {
					if codexFailureTerminal && eventType == "error" {
						// OpenAI commonly follows a bare error with response.failed.
						// Defer provider health updates so the pair is applied once.
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
						sawFailedEvent = true
						streamEarlyErr = options.FailoverWithModel(upstreamRequestID, dataBytes, failedMessage, mappedModel, resp.Header)
						return
					}
					if !cyberHit && !sawBareError {
						if status, errType, errMsg, matched := options.ErrorRule(dataBytes, failedMessage); matched {
							sawFailedEvent = true
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
							streamEarlyErr = fmt.Errorf("upstream response failed: passthrough rule matched message=%s", errMsg)
							return
						}
					}
				}
				forceFlushFailedEvent = true
				sawFailedEvent = true
				failedPayload = append(failedPayload[:0], dataBytes...)
				terminalFailurePending = !codexFailureTerminal || eventType == "response.failed"
			}
			if normalizedData, normalized := wire.NormalizeCompletedImageGenerationStatus(dataBytes); normalized {
				dataBytes = normalizedData
				data = string(normalizedData)
				line = "data: " + data
			}
			imageCounter.AddSSEData(dataBytes)
			searchCounter += options.CountSearch(dataBytes, streamSearchSeen)

			// Correct Codex tool calls if needed (apply_patch -> edit, etc.)
			if correctedData, corrected := options.ToolCorrector.CorrectToolCallsInSSEBytes(dataBytes); corrected {
				dataBytes = correctedData
				data = string(correctedData)
				line = "data: " + data
				eventType = wire.EffectiveOpenAISSEEventType(dataBytes, eventType)
			}
			if imageOutput, ok := wire.ExtractImageGenerationOutputFromSSEData(dataBytes, streamSeenImages); ok {
				streamImageOutputs = append(streamImageOutputs, imageOutput)
			}
			streamDoneItems.Observe(dataBytes)
			if wire.ResponsesStreamEventMayContributeToOutput(eventType) {
				var streamEvent wire.ResponsesStreamEvent
				if err := json.Unmarshal(dataBytes, &streamEvent); err == nil {
					responseAccumulator.ProcessEvent(&streamEvent)
				}
			}
			if normalizedData, normalized := bridge.NormalizeResponsesStreamingTerminalOutput(dataBytes, responseAccumulator, streamDoneItems, streamImageOutputs); normalized {
				dataBytes = normalizedData
				data = string(normalizedData)
				line = "data: " + data
				eventType = wire.EffectiveOpenAISSEEventType(dataBytes, eventType)
			}
			restoredData, restoreErr := options.RestoreClientTools(dataBytes)
			if restoreErr != nil {
				streamEarlyErr = fmt.Errorf("restore Grok Responses client tool response: %w", restoreErr)
				return
			}
			restoredData, restoreErr = options.RestoreNamespace(restoredData)
			if restoreErr != nil {
				streamEarlyErr = fmt.Errorf("restore OpenAI namespace response: %w", restoreErr)
				return
			}
			restoredData = options.RestoreToolNames(restoredData, eventType)
			if !bytes.Equal(restoredData, dataBytes) {
				dataBytes = restoredData
				data = string(restoredData)
				line = "data: " + data
				eventType = wire.EffectiveOpenAISSEEventType(dataBytes, eventType)
			}
			if sanitizedData, sanitized := SanitizeOpenAIResponseFailedEventForClient(
				dataBytes,
				eventType,
				options.ClientOutputStarted(clientOutputStarted),
			); sanitized {
				dataBytes = sanitizedData
				data = string(sanitizedData)
				line = "data: " + data
			}
			// Replace model in response if needed.
			// Fast path: most events do not contain model field values.
			if needModelReplace && mappedModel != "" && strings.Contains(line, mappedModel) {
				line = wire.ReplaceModelInSSELine(line, mappedModel, originalModel)
			}
			startsClientOutput := forceFlushFailedEvent || OpenAIStreamDataStartsClientOutput(data, eventType)
			startsVisibleOutput := wire.StreamDataStartsVisibleOutput(data, eventType)
			if startsVisibleOutput && firstSemanticOutput == nil {
				elapsed := time.Since(startTime)
				firstSemanticOutput = &elapsed
			}
			startsTTFTOutput := options.OpenAIStreamDataStartsTTFT(data, eventType, forceFlushFailedEvent, ttftMode)
			if stageFirstOutput {
				eventStartsClientOutput = eventStartsClientOutput || startsClientOutput
				eventStartsVisibleOutput = eventStartsVisibleOutput || startsVisibleOutput
				eventStartsTTFTOutput = eventStartsTTFTOutput || startsTTFTOutput
				if startsClientOutput {
					firstOutputScanGuard.Store(false)
				}
			}
			if startsClientOutput && !wire.OpenAIStreamEventTypeIsTerminal(eventType) {
				responsesSemanticOutputSeen = true
			}
			// OpenAI Responses streams that terminate with an empty
			// response.completed (no output, no usage, no error, nothing sent
			// to the client) are silent upstream refusals: fail over instead of
			// recording a successful 0/0 usage turn (issue #5009).
			if options.NativeOpenAI &&
				(eventType == "response.completed" || eventType == "response.done") &&
				!sawFailedEvent && !responsesSemanticOutputSeen && !clientOutputStarted &&
				wire.OpenAIResponsesCompletedEventIsEmpty(dataBytes, usage) {
				sawTerminalEvent = true
				streamEarlyErr = options.EmptyCompleted(upstreamRequestID)
				return
			}

			if pendingLifecycleHeader != "" {
				header := pendingLifecycleHeader
				if eventType != strings.TrimSpace(pendingSSEEventType) {
					header = "event: " + eventType
				}
				line = header + "\n" + line
				pendingLifecycleHeader = ""
			}
			suppressCurrentEvent = suppressCurrentEvent || duplicateTerminal
			// 写入客户端（客户端断开后继续 drain 上游）
			if !clientDisconnected && !failureDelivered && !suppressCurrentEvent {
				shouldFlush := queueDrained && (clientOutputStarted || startsClientOutput)
				if firstTokenMs == nil && startsTTFTOutput {
					// 保证首个 token 事件尽快出站，避免影响 TTFT。
					shouldFlush = true
				}
				eventShouldFlush = eventShouldFlush || shouldFlush
				if _, err := writePendingString(line); err != nil {
					handlePendingWriteError(err)
				} else if _, err := writePendingString("\n"); err != nil {
					handlePendingWriteError(err)
				} else {
					eventInProgress = true
				}
			}

			// Record first token time
			if startsVisibleOutput {
				options.MarkTime(StreamTimeVisible)
			}
			if !guardFirstOutput && firstTokenMs == nil && startsTTFTOutput {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
				stopFirstOutputTimer()
			}
			hasObservedUsage = wire.ParseSSEUsageBytesWithType(dataBytes, eventType, usage) || hasObservedUsage
			return
		}

		// A blank line dispatches a guarded event from the attempt-local stage.
		if stageFirstOutput && line == "" {
			pendingLifecycleHeader = ""
			pendingSSEEventType = ""
			if suppressCurrentEvent {
				suppressCurrentEvent = false
				terminalFailurePending = false
				eventInProgress = false
				eventStartsClientOutput = false
				eventStartsVisibleOutput = false
				eventShouldFlush = false
				return
			}
			if failureDelivered {
				terminalFailurePending = false
				eventInProgress = false
				eventStartsClientOutput = false
				eventStartsVisibleOutput = false
				eventShouldFlush = false
				return
			}
			if !clientDisconnected {
				if _, err := writePendingString("\n"); err != nil {
					handlePendingWriteError(err)
				}
			}
			if streamEarlyErr == nil {
				completeGuardedEvent(queueDrained)
			}
			if terminalFailurePending && streamEarlyErr == nil {
				terminalFailurePending = false
				failureDelivered = true
			}
			return
		}
		// Non-guarded streams retain upstream's event-boundary flushing: a keepalive
		// or queue-drain flush must never split an open SSE event.
		shouldFlush := false
		if line == "" {
			pendingLifecycleHeader = ""
			pendingSSEEventType = ""
			if suppressCurrentEvent {
				suppressCurrentEvent = false
				terminalFailurePending = false
				eventInProgress = false
				eventShouldFlush = false
				return
			}
			shouldFlush = eventShouldFlush || (queueDrained && clientOutputStarted)
			eventShouldFlush = false
			if failureDelivered {
				terminalFailurePending = false
			}
		}
		if !clientDisconnected && !failureDelivered && !suppressCurrentEvent {
			if _, err := writePendingString(line); err != nil {
				handlePendingWriteError(err)
			} else if _, err := writePendingString("\n"); err != nil {
				handlePendingWriteError(err)
			} else {
				eventInProgress = line != ""
				if shouldFlush {
					if err := flushBuffered(); err != nil {
						clientDisconnected = true
						options.Logf("Client disconnected during streaming flush, continuing to drain upstream for billing")
					} else {
						clientOutputStarted = true
						lastDownstreamWriteAt = time.Now()
					}
				}
			}
			if line == "" && terminalFailurePending && streamEarlyErr == nil {
				terminalFailurePending = false
				failureDelivered = true
			}
		}
	}

	// 无超时/无 keepalive 的常见路径走同步扫描，减少 goroutine 与 channel 开销。
	if streamInterval <= 0 && keepaliveInterval <= 0 && firstOutputTimeout <= 0 {
		defer httpclient.PutSSEScannerBuf64K(scanBuf)
		for documentScanner.Scan() {
			processSSELine(documentScanner.Text(), true)
			if streamEarlyErr != nil {
				return resultWithUsage(), streamEarlyErr
			}
		}
		if result, err, done := handleScanErr(documentScanner.Err()); done {
			return result, err
		}
		return finalizeStream()
	}

	type scanEvent struct {
		line      string
		err       error
		processed chan struct{}
	}
	// 独立 goroutine 读取上游，避免读取阻塞影响 keepalive/超时处理
	// 保护模式允许一个排队 token 和一个正在处理的 token；配合 scanner 上限，
	// scanner/channel 保留量约束在 16 MiB 附近。禁用超时时队列深度为 16。
	events := make(chan scanEvent, OpenAIFirstOutputEventQueueSize(guardFirstOutput))
	done := make(chan struct{})
	sendEvent := func(ev scanEvent) bool {
		if firstOutputScanGuard.Load() {
			ev.processed = make(chan struct{})
		}
		select {
		case events <- ev:
		case <-done:
			return false
		}
		if ev.processed == nil {
			return true
		}
		select {
		case <-ev.processed:
			return true
		case <-done:
			return false
		}
	}
	markEventProcessed := func(ev scanEvent) {
		if ev.processed != nil {
			close(ev.processed)
		}
	}
	var lastReadAt int64
	atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
	go func(scanBuf *httpclient.SSEScannerBuf64K) {
		defer httpclient.PutSSEScannerBuf64K(scanBuf)
		defer close(events)
		for documentScanner.Scan() {
			atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
			if !sendEvent(scanEvent{line: documentScanner.Text()}) {
				return
			}
		}
		if err := documentScanner.Err(); err != nil {
			_ = sendEvent(scanEvent{err: err})
		}
	}(scanBuf)
	defer close(done)

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				if stageFirstOutput && eventInProgress {
					// 即使没有结尾空行，EOF 也会派发最后一个 SSE 事件；
					// 不在下游线路上合成额外字节。
					completeGuardedEvent(true)
				}
				return finalizeStream()
			}
			if result, err, done := handleScanErr(ev.err); done {
				markEventProcessed(ev)
				return result, err
			}
			processSSELine(ev.line, len(events) == 0)
			markEventProcessed(ev)
			if streamEarlyErr != nil {
				return resultWithUsage(), streamEarlyErr
			}

		case <-intervalCh:
			if failureDelivered {
				return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
			}
			lastRead := time.Unix(0, atomic.LoadInt64(&lastReadAt))
			if time.Since(lastRead) < streamInterval {
				continue
			}
			if codexFailureTerminal && sawBareError && !sawResponseFailed {
				_ = resp.Body.Close()
				return finalizeStream()
			}
			if clientDisconnected {
				return resultWithUsage(), fmt.Errorf("stream usage incomplete after timeout")
			}
			options.Logf("Stream data interval timeout: provider=%d model=%s interval=%s", options.ProviderID, originalModel, streamInterval)
			// 处理流超时，可能标记提供商为临时不可调度或错误状态
			options.StreamTimeout(originalModel)
			// Grok 在尚未向客户端提交可见字节时执行短期冷却与提供商故障转移。
			// 输出开始后按 stream_timeout 处理，已发送的 SSE 保持原样。
			if options.GrokIdlePolicy {
				options.IdleCooldown()
				if !options.ClientOutputStarted(clientOutputStarted) && !eventShouldFlush {
					_ = resp.Body.Close()
					return resultWithUsage(), options.IdleFailover(streamInterval)
				}
			}
			if synthesizeBareErrorFailure() {
				flushPending("Client disconnected during bare error timeout flush, returning collected usage")
				return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
			}
			sendErrorEvent("stream_timeout")
			return resultWithUsage(), fmt.Errorf("stream data interval timeout")

		case <-firstOutputCh:
			if firstOutputProgressObserved {
				stopFirstOutputTimer()
				continue
			}
			if synthesizeBareErrorFailure() {
				flushPending("Client disconnected during bare error first-output timeout flush, returning collected usage")
				_ = resp.Body.Close()
				for ev := range events {
					markEventProcessed(ev)
				}
				return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
			}
			_ = resp.Body.Close()
			for ev := range events {
				markEventProcessed(ev)
			}
			return resultWithUsage(), options.FirstOutputError(startTime, originalModel, reasoningEffort,
				firstOutputTimeout, "semantic_output", resp.Header,
			)

		case <-keepaliveCh:
			if clientDisconnected {
				continue
			}
			if eventInProgress {
				continue
			}
			if time.Since(lastDownstreamWriteAt) < keepaliveInterval {
				continue
			}
			if stageFirstOutput {
				// 绕过当前尝试的本地缓冲帧；稳定 SSE 注释可以提交，
				// 但提供商相关 header 在出现语义输出前仍保持私有。
				n, err := w.Write([]byte(":\n\n"))
				options.KeepaliveBytes(n)
				if err != nil {
					clientDisconnected = true
					options.Logf("Client disconnected during streaming, continuing to drain upstream for billing")
					continue
				}
				flusher.Flush()
				lastDownstreamWriteAt = time.Now()
				continue
			}
			if _, err := writePendingString(":\n\n"); err != nil {
				clientDisconnected = true
				options.Logf("Client disconnected during streaming, continuing to drain upstream for billing")
				continue
			}
			if err := flushBuffered(); err != nil {
				clientDisconnected = true
				options.Logf("Client disconnected during keepalive flush, continuing to drain upstream for billing")
			} else {
				lastDownstreamWriteAt = time.Now()
			}
		}
	}
}
