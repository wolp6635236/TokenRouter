package textattempt

import (
	"context"
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewaycapture "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	textflow "github.com/TokenFlux/TokenRouter/internal/gateway/text"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/server/clientip"
)

// geminiMessageAttemptBridge 不注册 Anthropic 空闲会话，不扩大部分失败完成资格。
type geminiMessageAttemptBridge struct {
	messageAttemptBridge
	forwardModel string
	forwardBody  []byte
	groupMapping routing.GroupMappingResult
}

// Select 保留 Gemini Messages 的既有差异，循环复用 gateway/text。
func (b *geminiMessageAttemptBridge) Select(excluded map[int64]struct{}) (textflow.Selection, error) {
	var err error
	b.selection, err = b.binding().selectProvider(b.c.Request.Context(), b.apiKey.GroupID, b.sessionKey, b.reqModel, excluded, "", int64(0)) // Gemini 不使用会话限制
	if err != nil {
		return textflow.Selection{}, err
	}
	b.provider = b.selection.Provider
	gatewayhttp.SetOpsSelectedProvider(b.c, b.provider.Record.ID, b.provider.Record.Platform)
	return gatewaycapture.CaptureTextSelection(b.provider), nil
}

// FirstSelectionFailure 保留 Gemini Messages 的既有差异，循环复用 gateway/text。
func (b *geminiMessageAttemptBridge) FirstSelectionFailure(err error, _ bool) {
	if handleGroupSelectionBusinessError(b.c, err, *b.streamStarted, func(status int, errType string, message string, responseStarted bool) {
		b.binding().handleStreamingAwareError(b.c, status, errType, message, responseStarted)
	}) {
		return
	}
	cls := classifyNoProviderErrorFromGin(b.c, b.binding().diagnoser, b.apiKey, b.reqModel, b.reqModel, capability.PlatformGemini)
	if !cls.ModelNotFound {
		gatewayhttp.MarkOpsRoutingCapacityLimitedIfNoAvailable(b.c, err)
	}
	b.reqLog.Warn("gateway.select_provider_no_available",
		zap.String("model", b.reqModel),
		zap.Int64p("group_id", b.apiKey.GroupID),
		zap.String("platform", b.platform),
		zap.Bool("model_not_found", cls.ModelNotFound),
		zap.Error(err),
	)
	message := cls.Message
	if !cls.ModelNotFound {
		message = "No available providers: " + err.Error()
	}
	b.binding().handleStreamingAwareError(b.c, cls.Status, cls.ErrType, message, *b.streamStarted)
}

// Acquire 保留 Gemini Messages 的既有差异，循环复用 gateway/text。
func (b *geminiMessageAttemptBridge) Acquire() bool {
	// 3. 获取提供商并发槽位
	b.providerReleaseFunc = b.selection.ReleaseFunc
	if !b.selection.Acquired {
		if b.selection.WaitPlan == nil {
			gatewayhttp.MarkOpsRoutingCapacityLimited(b.c)
			b.reqLog.Warn("gateway.select_provider_no_slot_no_wait_plan",
				zap.Int64("provider_id", b.provider.Record.ID),
				zap.String("model", b.reqModel),
				zap.String("platform", b.platform),
			)
			b.binding().handleStreamingAwareError(b.c, http.StatusServiceUnavailable, "api_error", "No available providers", *b.streamStarted)
			return false
		}
		providerWaitCounted := false
		waitEntry, err := b.binding().concurrencyHelper.EnterProviderWait(b.c.Request.Context(), b.provider.Record.ID, b.selection.WaitPlan.MaxWaiting)
		canWait := waitEntry.Allowed
		if err != nil {
			b.reqLog.Warn("gateway.provider_wait_counter_increment_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
		} else if !canWait {
			b.reqLog.Info("gateway.provider_wait_queue_full",
				zap.Int64("provider_id", b.provider.Record.ID),
				zap.Int("max_waiting", b.selection.WaitPlan.MaxWaiting),
			)
			b.binding().handleStreamingAwareErrorWithCode(b.c, http.StatusTooManyRequests, "rate_limit_error", gatewayhttp.GatewayQueueFullCode, "Too many pending requests, please retry later", *b.streamStarted)
			return false
		}
		if err == nil && canWait {
			providerWaitCounted = true
		}
		releaseWait := func() {
			if providerWaitCounted {
				waitEntry.Release()
				providerWaitCounted = false
			}
		}

		b.providerReleaseFunc, err = b.binding().concurrencyHelper.AcquireProviderSlotWithWaitTimeout(
			b.c,
			b.provider.Record.ID,
			b.selection.WaitPlan.MaxConcurrency,
			b.selection.WaitPlan.Timeout,
			b.reqStream,
			b.streamStarted,
		)
		if err != nil {
			b.reqLog.Warn("gateway.provider_slot_acquire_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
			releaseWait()
			b.binding().handleConcurrencyError(b.c, err, "provider", *b.streamStarted)
			return false
		}
		// Slot acquired: no longer waiting in queue.
		releaseWait()
		if err := b.binding().bindSticky(b.c.Request.Context(), b.apiKey.GroupID, b.sessionKey, b.provider.Record.ID); err != nil {
			b.reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
		}
	}
	// 提供商槽位/等待计数需要在超时或断开时安全回收
	b.providerReleaseFunc = scheduler.WrapRelease(b.c.Request.Context(), scheduler.ReleaseOnCancel, b.providerReleaseFunc)

	return true
}

// Forward 保留 Gemini Messages 的既有差异，循环复用 gateway/text。
func (b *geminiMessageAttemptBridge) Forward(state textflow.AttemptState) textflow.Outcome {
	var err error
	// 转发请求 - 根据提供商平台分流

	requestCtx := b.c.Request.Context()
	if state.SwitchCount > 0 {
		requestCtx = requeststate.WithProviderSwitchCount(requestCtx, state.SwitchCount)
	}
	// 记录 Forward 前已写入字节数，Forward 后若增加则说明 SSE 内容已发，禁止 failover
	b.writerSizeBeforeForward = b.c.Writer.Size()
	if b.provider.Record.Platform == capability.PlatformAntigravity {
		b.result, err = b.binding().forwardAntigravityGemini(
			requestCtx,
			b.c,
			b.provider,
			b.forwardModel,
			"generateContent",
			b.reqStream,
			b.forwardBody,
			b.hasBoundSession,
			forwardcore.WithGeminiSession(derefGroupID(b.apiKey.GroupID), b.sessionKey),
		)
	} else {
		b.result, err = b.binding().forwardGemini(requestCtx, b.c, b.provider, b.forwardBody)
	}
	if b.providerReleaseFunc != nil {
		b.providerReleaseFunc()
	}
	b.binding().reportSchedule(b.selection, b.provider.Record.ID, err == nil, b.result)
	out := textflow.Outcome{Attempt: messageObservedAttempt(b.result, err), Err: err, HasResult: b.result != nil, OutputChanged: b.c.Writer.Size() != b.writerSizeBeforeForward}
	out.Attempt.HTTPCommitted = b.c.Writer.Written()
	out.Attempt.RetryCommitted = out.OutputChanged
	if retry, ok := errors.AsType[*forwardcore.UpstreamFailoverError](err); ok {
		out.Failure = &textflow.AttemptFailure{Cause: retry, Policy: retry.RetryFailure()}
	}
	return out
}

// Success 保留 Gemini Messages 的既有差异，循环复用 gateway/text。
func (b *geminiMessageAttemptBridge) Success() {
	// RPM 计数递增（Forward 成功后）
	// 注意：TOCTOU 竞态是已知且可接受的设计权衡，与 WindowCost 一致的 soft-limit 模式。
	// 在高并发下可能短暂超出 RPM 限制，但不会导致请求失败。
	if b.provider.View().IsAnthropicOAuthOrSetupToken() && gatewaycapture.ExecutionRuntimeConfig(b.provider).GetBaseRPM() > 0 {
		if err := b.binding().incrementRPM(b.c.Request.Context(), b.provider.Record.ID); err != nil {
			b.reqLog.Warn("gateway.rpm_increment_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
		}
	}
}

// Complete 保留 Gemini Messages 的既有差异，循环复用 gateway/text。
func (b *geminiMessageAttemptBridge) Complete(state textflow.AttemptState) {
	// 捕获请求信息，供异步任务记录。
	userAgent := b.c.GetHeader("User-Agent")
	clientIP := clientip.GetClientIP(b.c)
	requestPayloadHash := billing.HashUsageRequestPayload(b.body)
	inboundEndpoint := gatewayhttp.GetInboundEndpoint(b.c)
	upstreamEndpoint := gatewayhttp.GetUpstreamEndpoint(b.c, b.provider.Record.Platform)

	if b.result.ReasoningEffort == nil {
		b.result.ReasoningEffort = protocol.NormalizeClaudeOutputEffort(b.parsedReq.OutputEffort)
	}
	if b.result.ReasoningEffort == nil && b.parsedReq.ThinkingEnabled {
		protocolModel := b.result.UpstreamModel
		if protocolModel == "" {
			protocolModel = b.result.Model
		}
		b.result.ReasoningEffort = gatewaycapture.DefaultEffortForThinkingEnabled(protocolModel)
	}

	// 用量记录通过有界 worker 池提交。
	// 完成任务捕获 ForceCacheBilling 的布尔值，failover 响应体可随请求释放。
	forceCacheBilling := state.ForceCacheBilling

	clientSessionID := gatewayhttp.ExtractClientSessionID(b.c)
	gatewayhttp.StampForwardRequestedReasoningEffort(b.result, b.c)
	// 入队前捕获资金和报文数据，worker 使用这份快照。
	completionInput := gatewaycapture.CaptureMessages(gatewayhttp.CompletionContext(b.c), &gatewaycapture.MessagesCapture{
		Result: b.result,

		APIKey:             b.apiKey,
		User:               b.apiKey.User,
		Provider:           gatewaycapture.ExecutionCompletionRecord(b.provider),
		Subscription:       b.subscription,
		InboundEndpoint:    inboundEndpoint,
		UpstreamEndpoint:   upstreamEndpoint,
		UserAgent:          userAgent,
		IPAddress:          clientIP,
		ClientSessionID:    clientSessionID,
		RequestPayloadHash: requestPayloadHash,
		RequestBody:        append([]byte(nil), b.body...),
		ForceCacheBilling:  forceCacheBilling,
		APIKeyService:      b.binding().apiKeyService,
		PricingUsageFields: b.groupMapping.ToUsageFields(b.reqModel, b.result.UpstreamModel),
	})
	completionUserID := b.subject.UserID
	completionModel := b.reqModel
	completionRuntime := b.binding().recorder
	b.binding().submitUsageRecordTask(b.c, func(ctx context.Context) {
		if err := completionRuntime.Record(ctx, completionInput, false); err != nil {
			logging.L().With(
				zap.String("component", "handler.gateway.messages"),
				zap.Int64("user_id", completionUserID),
				zap.Int64("api_key_id", completionInput.APIKey.ID),
				zap.Any("group_id", completionInput.APIKey.GroupID),
				zap.String("model", completionModel),
				zap.Int64("provider_id", completionInput.Provider.ID),
			).Error("gateway.record_usage_failed", zap.Error(err))
		}
	})
}

func (b *geminiMessageAttemptBridge) Begin() {
	b.currentAPIKey = b.apiKey
	b.currentSubscription = b.subscription
	if b.binding().singleProviderGroup(b.Context(), b.apiKey.GroupID) {
		b.SingleProviderRetry()
	}
}
func (b *geminiMessageAttemptBridge) PrepareAttempt() bool { return true }
func (b *geminiMessageAttemptBridge) Abandon(int64)        {}
func (b *geminiMessageAttemptBridge) Exhausted(err *textflow.AttemptFailure, _ string, stream bool) {
	b.messageAttemptBridge.Exhausted(err, capability.PlatformGemini, stream)
}
