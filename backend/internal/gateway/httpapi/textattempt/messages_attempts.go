package textattempt

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/clientmeta"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewaycapture "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	textflow "github.com/TokenFlux/TokenRouter/internal/gateway/text"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	queuepolicy "github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
	"github.com/TokenFlux/TokenRouter/internal/server/clientip"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

// messageAttemptBridge 适配 Messages 单次尝试，异步完成仅捕获 completion.Input。
type messageAttemptBridge struct {
	fixed                                          *messageExecutionDependencies
	c                                              *gin.Context
	apiKey, currentAPIKey                          *apikey.APIKey
	subject                                        authctx.AuthSubject
	subscription, currentSubscription              *billing.UserSubscription
	parsedReq, attemptParsedReq                    *requeststate.ParsedRequest
	body                                           []byte
	reqModel, platform, sessionKey                 string
	reqStream, isClaudeCodeClient, hasBoundSession bool
	streamStarted                                  *bool
	sessionBoundProviderID                         int64
	reqLog                                         *zap.Logger
	fallbackGroupID                                *int64
	sessionAttempts                                *scheduler.SessionAttempts
	selection                                      *gatewaycapture.SelectionResult
	provider                                       *gatewaycapture.ExecutionProvider
	providerReleaseFunc                            func()
	attemptGroupMapping                            routing.GroupMappingResult
	writerSizeBeforeForward                        int
	result                                         *forwardcore.MessagesResult
}

// PrepareAttempt 准备当前尝试的模型和请求体。
func (b *messageAttemptBridge) PrepareAttempt() bool {
	var err error
	b.attemptParsedReq, b.attemptGroupMapping, err = b.binding().prepareGatewayAttemptRequest(
		b.c.Request.Context(), b.parsedReq, b.body, b.currentAPIKey, b.reqModel,
	)
	if err != nil {
		b.binding().errorResponse(b.c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return false
	}

	return true
}

// Select 选择支持请求模型的提供商。
func (b *messageAttemptBridge) Select(excluded map[int64]struct{}) (textflow.Selection, error) {
	// 选择支持该模型的提供商
	b.reqLog.Info("sticky.selecting_provider",
		zap.String("session_key", b.sessionKey),
		zap.Int64("sticky_bound_provider_id", b.sessionBoundProviderID),
		zap.Bool("has_bound_session", b.hasBoundSession),
		zap.Int("failed_provider_count", len(excluded)),
	)
	var err error
	b.selection, err = b.binding().selectProvider(b.c.Request.Context(), b.currentAPIKey.GroupID, b.sessionKey, b.reqModel, excluded, b.parsedReq.MetadataUserID, b.subject.UserID)
	if err != nil {
		return textflow.Selection{}, err
	}
	b.provider = b.selection.Provider
	gatewayhttp.SetOpsSelectedProvider(b.c, b.provider.Record.ID, b.provider.Record.Platform)
	if b.sessionKey != "" {
		b.binding().trackSession(b.sessionAttempts, b.provider, b.sessionKey)
	}

	// [DEBUG-STICKY] 打印提供商选择结果
	b.reqLog.Info("sticky.provider_selected",
		zap.Int64("selected_provider_id", b.provider.Record.ID),
		zap.String("provider_name", b.provider.Record.Name),
		zap.Bool("slot_acquired", b.selection.Acquired),
		zap.Bool("has_wait_plan", b.selection.WaitPlan != nil),
		zap.Int64("sticky_bound_provider_id", b.sessionBoundProviderID),
		zap.Bool("sticky_honored", b.sessionBoundProviderID > 0 && b.sessionBoundProviderID == b.provider.Record.ID),
	)

	return gatewaycapture.CaptureTextSelection(b.provider), nil
}

// FirstSelectionFailure 将首次选择失败转换为客户端错误。
func (b *messageAttemptBridge) FirstSelectionFailure(err error, fallbackUsed bool) {
	if handleGroupSelectionBusinessError(b.c, err, *b.streamStarted, func(status int, errType string, message string, responseStarted bool) {
		b.binding().handleStreamingAwareError(b.c, status, errType, message, responseStarted)
	}) {
		return
	}
	cls := classifyNoProviderErrorFromGin(b.c, b.binding().diagnoser, b.currentAPIKey, b.reqModel, b.reqModel, b.platform)
	if !cls.ModelNotFound {
		gatewayhttp.MarkOpsRoutingCapacityLimitedIfNoAvailable(b.c, err)
	}
	b.reqLog.Warn("gateway.select_provider_no_available",
		zap.String("model", b.reqModel),
		zap.Int64p("group_id", b.currentAPIKey.GroupID),
		zap.String("platform", b.platform),
		zap.Bool("fallback_used", fallbackUsed),
		zap.Bool("model_not_found", cls.ModelNotFound),
		zap.Error(err),
	)
	message := cls.Message
	if !cls.ModelNotFound {
		message = "No available providers: " + err.Error()
	}
	b.binding().handleStreamingAwareError(b.c, cls.Status, cls.ErrType, message, *b.streamStarted)
}

// Intercept 识别并直接响应 warmup 请求。
func (b *messageAttemptBridge) Intercept() bool {
	if b.provider.View().IsInterceptWarmupEnabled() {
		interceptType := clientmeta.DetectInterceptType(b.body, b.reqModel, b.parsedReq.MaxTokens, b.isClaudeCodeClient)
		if interceptType != clientmeta.InterceptTypeNone {
			if b.selection.Acquired && b.selection.ReleaseFunc != nil {
				b.selection.ReleaseFunc()
			}
			if b.reqStream {
				gatewayhttp.WriteInterceptStream(b.c, b.reqModel, interceptType)
			} else {
				gatewayhttp.WriteInterceptResponse(b.c, b.reqModel, interceptType)
			}
			return true
		}
	}

	return false
}

// Acquire 取得提供商槽位，等待成功后绑定粘性会话。
func (b *messageAttemptBridge) Acquire() bool {
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
		b.reqLog.Info("sticky.bind_after_wait",
			zap.String("session_key", b.sessionKey),
			zap.Int64("provider_id", b.provider.Record.ID),
		)
		if err := b.binding().bindSticky(b.c.Request.Context(), b.currentAPIKey.GroupID, b.sessionKey, b.provider.Record.ID); err != nil {
			b.reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
		}
	}
	// 提供商槽位/等待计数需要在超时或断开时安全回收
	b.providerReleaseFunc = scheduler.WrapRelease(b.c.Request.Context(), scheduler.ReleaseOnCancel, b.providerReleaseFunc)
	b.sessionAttempts.Own(b.provider.Record.ID, b.providerReleaseFunc)

	return true
}

// Forward 应用消息队列和兼容策略，再向所选平台转发请求。
func (b *messageAttemptBridge) Forward(state textflow.AttemptState) textflow.Outcome {
	var err error
	// 用户消息串行队列开始。
	var queueRelease func()
	umqMode := b.binding().getUserMsgQueueMode(b.provider, b.attemptParsedReq)

	switch umqMode {
	case queuepolicy.MessageQueueSerialize:
		// 串行模式：获取锁 + RPM 延迟 + 释放（当前行为不变）
		baseRPM := gatewaycapture.ExecutionRuntimeConfig(b.provider).GetBaseRPM()
		release, qErr := b.binding().userMsgQueueHelper.AcquireWithWait(
			b.c, b.provider.Record.ID, baseRPM, b.reqStream, b.streamStarted,
			b.binding().messageWaitTimeout,
			b.reqLog,
		)
		if qErr != nil {
			// fail-open: 记录 warn，不阻止请求
			b.reqLog.Warn("gateway.umq_acquire_failed",
				zap.Int64("provider_id", b.provider.Record.ID),
				zap.Error(qErr),
			)
		} else {
			queueRelease = release
		}

	case queuepolicy.MessageQueueThrottle:
		// 按 RPM 添加自适应延迟，并发槽位继续按并发限制管理。
		baseRPM := gatewaycapture.ExecutionRuntimeConfig(b.provider).GetBaseRPM()
		if tErr := b.binding().userMsgQueueHelper.ThrottleWithPing(
			b.c, b.provider.Record.ID, baseRPM, b.reqStream, b.streamStarted,
			b.binding().messageWaitTimeout,
			b.reqLog,
		); tErr != nil {
			b.reqLog.Warn("gateway.umq_throttle_failed",
				zap.Int64("provider_id", b.provider.Record.ID),
				zap.Error(tErr),
			)
		}

	default:
		if umqMode != "" {
			b.reqLog.Warn("gateway.umq_unknown_mode",
				zap.String("mode", umqMode),
				zap.Int64("provider_id", b.provider.Record.ID),
			)
		}
	}

	// context 取消时自动释放串行锁，queueRelease 由 serialize 模式创建。
	queueRelease = scheduler.WrapRelease(b.c.Request.Context(), scheduler.ReleaseOnCancel, queueRelease)
	b.sessionAttempts.Own(b.provider.Record.ID, queueRelease)
	// 注入回调到 ParsedRequest：使用外层 wrapper 以便提前清理 AfterFunc
	b.attemptParsedReq.OnUpstreamAccepted = queueRelease
	// 用户消息串行队列结束。

	// Bedrock CC 兼容：清理 body 专有字段 + 过滤 anthropic-beta header，适用于所有转发路径
	if err := b.attemptParsedReq.ReplaceBody(b.binding().bedrockCompat(b.c, b.attemptParsedReq.Body.Bytes(), b.attemptParsedReq.Model, b.provider, b.currentAPIKey.GroupID)); err != nil {
		b.binding().errorResponse(b.c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return textflow.Outcome{Stop: true}
	}
	attemptBody := b.attemptParsedReq.Body.Bytes()

	// 转发请求 - 根据提供商平台分流
	b.c.Set("parsed_request", b.attemptParsedReq)

	requestCtx := b.c.Request.Context()
	if state.SwitchCount > 0 {
		requestCtx = requeststate.WithProviderSwitchCount(requestCtx, state.SwitchCount)
	}
	if state.ForceCacheBilling {
		// 将故障转移后的缓存计费标记传给同步响应改写函数。
		requestCtx = requeststate.WithForceCacheBilling(requestCtx)
		// 分组回退会重建提供商尝试状态，请求 context 保存已触发的计费策略直至完成。
		b.c.Request = b.c.Request.WithContext(requestCtx)
	}
	// 记录 Forward 前已写入字节数，Forward 后若增加则说明 SSE 内容已发，禁止 failover
	b.writerSizeBeforeForward = b.c.Writer.Size()
	if b.provider.Record.Platform == capability.PlatformAntigravity {
		b.result, err = b.binding().forwardAntigravity(requestCtx, b.c, b.provider, attemptBody, b.hasBoundSession)
	} else {
		b.result, err = b.binding().forwardMessages(requestCtx, b.c, b.provider, b.attemptParsedReq)
	}

	// 释放尚未由回调释放的串行锁。
	if queueRelease != nil {
		queueRelease()
	}
	// 清理回调引用，防止 failover 重试时旧回调被错误调用
	b.attemptParsedReq.OnUpstreamAccepted = nil

	if b.providerReleaseFunc != nil {
		b.providerReleaseFunc()
	}
	b.binding().reportSchedule(b.selection, b.provider.Record.ID, err == nil, b.result)

	out := textflow.Outcome{Attempt: messageObservedAttempt(b.result, err), Err: err, HasResult: b.result != nil, OutputChanged: b.c.Writer.Size() != b.writerSizeBeforeForward}
	out.Attempt.HTTPCommitted = b.c.Writer.Written()
	out.Attempt.RetryCommitted = out.OutputChanged
	var policy *anthropic.BetaBlockedError
	var prompt *antigravity.PromptTooLongError
	var retry *forwardcore.UpstreamFailoverError
	switch {
	case errors.As(err, &policy):
		out.Kind = textflow.FailurePolicy
	case errors.As(err, &prompt):
		out.Kind = textflow.FailurePromptTooLong
	case errors.As(err, &retry):
		out.Failure = &textflow.AttemptFailure{Cause: retry, Policy: retry.RetryFailure()}
	}
	return out
}

// Complete 捕获用量和请求数据并提交完成任务。
func (b *messageAttemptBridge) Complete(state textflow.AttemptState) {
	usageResult := b.result

	if usageResult == nil {
		return
	}
	gatewayhttp.StampForwardRequestedReasoningEffort(usageResult, b.c)
	userAgent := b.c.GetHeader("User-Agent")
	clientIP := clientip.GetClientIP(b.c)
	requestPayloadHash := billing.HashUsageRequestPayload(b.attemptParsedReq.Body.Bytes())
	inboundEndpoint := gatewayhttp.GetInboundEndpoint(b.c)
	upstreamEndpoint := gatewayhttp.GetUpstreamEndpoint(b.c, b.provider.Record.Platform)

	if usageResult.ReasoningEffort == nil {
		usageResult.ReasoningEffort = protocol.NormalizeClaudeOutputEffort(b.attemptParsedReq.OutputEffort)
	}
	if usageResult.ReasoningEffort == nil && b.attemptParsedReq.ThinkingEnabled {
		protocolModel := usageResult.UpstreamModel
		if protocolModel == "" {
			protocolModel = usageResult.Model
		}
		usageResult.ReasoningEffort = gatewaycapture.DefaultEffortForThinkingEnabled(protocolModel)
	}

	// 完成任务捕获 ForceCacheBilling 的布尔值，failover 响应体可随请求释放。
	forceCacheBilling := state.ForceCacheBilling || requeststate.IsForceCacheBilling(b.Context())

	clientSessionID := gatewayhttp.ExtractClientSessionID(b.c)
	// 入队前捕获资金和报文数据，worker 使用这份快照。
	completionInput := gatewaycapture.CaptureMessages(gatewayhttp.CompletionContext(b.c), &gatewaycapture.MessagesCapture{
		Result: usageResult,

		APIKey:             b.currentAPIKey,
		User:               b.currentAPIKey.User,
		Provider:           gatewaycapture.ExecutionCompletionRecord(b.provider),
		Subscription:       b.currentSubscription,
		InboundEndpoint:    inboundEndpoint,
		UpstreamEndpoint:   upstreamEndpoint,
		UserAgent:          userAgent,
		IPAddress:          clientIP,
		ClientSessionID:    clientSessionID,
		RequestPayloadHash: requestPayloadHash,
		RequestBody:        append([]byte(nil), b.body...),
		ForceCacheBilling:  forceCacheBilling,
		APIKeyService:      b.binding().apiKeyService,
		PricingUsageFields: b.attemptGroupMapping.ToUsageFields(b.reqModel, usageResult.UpstreamModel),
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

// OtherFailure 补充尚未输出的客户端错误并记录失败日志。
func (b *messageAttemptBridge) OtherFailure(err error) {
	upstreamErrorAlreadyCommunicated := gatewayhttp.ForwardErrorAlreadyCommunicated(b.c, b.writerSizeBeforeForward, err)
	wroteFallback := false
	if !upstreamErrorAlreadyCommunicated {
		wroteFallback = b.binding().ensureForwardErrorResponse(b.c, *b.streamStarted)
	}
	forwardFailedFields := []zap.Field{
		zap.Int64("provider_id", b.provider.Record.ID),
		zap.String("provider_name", b.provider.Record.Name),
		zap.String("provider_platform", b.provider.Record.Platform),
		zap.Bool("fallback_error_response_written", wroteFallback),
		zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
		zap.Error(err),
	}
	if b.provider.Record.Proxy != nil {
		forwardFailedFields = append(forwardFailedFields,
			zap.Int64("proxy_id", b.provider.Record.Proxy.ID),
			zap.String("proxy_name", b.provider.Record.Proxy.Name),
			zap.String("proxy_host", b.provider.Record.Proxy.Host),
			zap.Int("proxy_port", b.provider.Record.Proxy.Port),
		)
	} else if b.provider.Record.ProxyID != nil {
		forwardFailedFields = append(forwardFailedFields, zap.Int64p("proxy_id", b.provider.Record.ProxyID))
	}
	b.reqLog.Error("gateway.forward_failed", forwardFailedFields...)
}

// Success 增加 RPM 计数并更新粘性会话。
func (b *messageAttemptBridge) Success() {
	// RPM 计数递增（Forward 成功后）
	// 注意：TOCTOU 竞态是已知且可接受的设计权衡，与 WindowCost 一致的 soft-limit 模式。
	// 在高并发下可能短暂超出 RPM 限制，但不会导致请求失败。
	if b.provider.View().IsAnthropicOAuthOrSetupToken() && gatewaycapture.ExecutionRuntimeConfig(b.provider).GetBaseRPM() > 0 {
		if err := b.binding().incrementRPM(b.c.Request.Context(), b.provider.Record.ID); err != nil {
			b.reqLog.Warn("gateway.rpm_increment_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
		}
	}

	// 绑定粘性会话（成功转发后绑定/刷新）
	// - 无现有绑定（首次请求）：创建绑定
	// - 选中提供商与粘性提供商一致：刷新 TTL
	// - 粘性提供商因负载/RPM 被跳过、选中了其他提供商：不覆盖原绑定，
	//   下次请求粘性提供商恢复后仍可命中
	if b.sessionKey != "" && (b.sessionBoundProviderID == 0 || b.sessionBoundProviderID == b.provider.Record.ID) {
		if err := b.binding().bindSticky(b.c.Request.Context(), b.currentAPIKey.GroupID, b.sessionKey, b.provider.Record.ID); err != nil {
			b.reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("provider_id", b.provider.Record.ID), zap.Error(err))
		}
	}
}

func (b *messageAttemptBridge) Context() context.Context { return b.c.Request.Context() }
func (b *messageAttemptBridge) Begin() {
	b.currentAPIKey = b.apiKey
	b.currentSubscription = b.subscription
	if b.apiKey.Group != nil {
		b.fallbackGroupID = b.apiKey.Group.FallbackGroupIDOnInvalidRequest
	}
	b.sessionAttempts = b.binding().newSessionAttempts()
	if b.binding().singleProviderGroup(b.Context(), b.currentAPIKey.GroupID) {
		b.SingleProviderRetry()
	}
}

func (b *messageAttemptBridge) Finish(served bool) {
	if b.sessionAttempts != nil {
		b.sessionAttempts.Finish(scheduler.AttemptOutcome{Served: served})
	}
}

func (b *messageAttemptBridge) SingleProviderRetry() {
	b.c.Request = b.c.Request.WithContext(requeststate.WithSingleProviderRetry(b.Context(), true))
}
func (b *messageAttemptBridge) Canceled() { gatewayhttp.FailoverClientGone(b.c) }
func (b *messageAttemptBridge) Exhausted(err *textflow.AttemptFailure, platform string, forceStream bool) {
	if platform == "" {
		platform = b.platform
	}
	if err == nil {
		b.binding().handleFailoverExhaustedSimple(b.c, 502, *b.streamStarted)
		return
	}
	if original, ok := errors.AsType[*forwardcore.UpstreamFailoverError](err.Cause); ok {
		b.binding().handleFailoverExhausted(b.c, original, platform, forceStream || *b.streamStarted)
	}
}

func (b *messageAttemptBridge) PolicyFailure(err error) {
	if original, ok := errors.AsType[*anthropic.BetaBlockedError](err); ok {
		gatewayhttp.MarkOpsClientBusinessLimited(b.c, gatewayhttp.OpsClientBusinessLimitedReasonLocalPolicyDenied)
		b.binding().errorResponse(b.c, http.StatusBadRequest, "invalid_request_error", original.Message)
	}
}

func (b *messageAttemptBridge) Switched() {
	b.binding().providerSwitched(b.selection)
}
func (b *messageAttemptBridge) Abandon(id int64) { b.sessionAttempts.Abandon(id) }
func (b *messageAttemptBridge) TempUnscheduleRetryableError(ctx context.Context, id int64, err *textflow.AttemptFailure) {
	if original, ok := errors.AsType[*forwardcore.UpstreamFailoverError](err.Cause); ok {
		b.binding().tempUnschedule(ctx, id, original)
	}
}
