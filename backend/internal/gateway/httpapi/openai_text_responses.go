package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"

	"github.com/TokenFlux/TokenRouter/internal/gateway/execution"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	textflow "github.com/TokenFlux/TokenRouter/internal/gateway/text"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Responses 保留 HTTP Responses 的 compact、归属和等待后资金检查顺序。
// @project-doc docs/interfaces/openai_upstream.md#openai_protocol_dispatch
func (h *OpenAITextHandler) Responses(c *gin.Context) {
	done, accepted := h.beginRequest(c, "openai")
	if !accepted {
		return
	}
	defer done()

	// 捕获 handler 内部 panic，按当前响应状态输出错误。
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	compactStartedAt := time.Now()
	defer h.LogRemoteCompactOutcome(c, compactStartedAt)
	h.backend.TransportHTTP(c)

	requestStart := time.Now()

	// 从认证中间件读取 Key 与行为主体。
	apiKey, ok := h.backend.Access(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := authctx.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := RequestLogger(
		c,
		"handler.openai_gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.backend.Dependencies(c, reqLog) {
		return
	}

	// 在认证及依赖校验后读取请求体。
	body, err := ReadLenientJSONRequestBodyWithPrealloc(c.Request, h.options.MaxBodyBytes)
	if err != nil {
		if maxErr, ok := openAITextMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", BodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.backend.ReadFailure(reqLog, c.Request, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	h.backend.ObserveRequest(c, "", false)
	body, ok = h.normalizeOpenAIResponsesCompactRequest(c, reqLog, body)
	if !ok {
		return
	}
	legacyCompact, nativeCompactionV2 := IsOpenAIResponsesCompactPath(c), IsBareOpenAIResponsesPath(c) && IsOpenAIRemoteCompactionV2Request(body)
	// body-signal Compact 等待上游期间发送 SSE 注释心跳，维持反向代理连接（#3887）。
	// 首拍延迟一个心跳间隔，此前失败返回 JSON 和 HTTP 状态。客户端未标记流式或间隔为 0 时跳过心跳。
	stopCompactKeepalive := h.backend.StartCompact(c, h.options.CompactKeepaliveInterval)
	defer stopCompactKeepalive()

	// 校验请求体 JSON 合法性
	if !gjson.ValidBytes(body) {
		LogRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	// compact 规范化后替换用户提示词，再解析模型。
	body = h.prompt.ApplyUserPromptReplacementToBody(c.Request.Context(), body, "openai_responses")
	sessionHashBody := body

	// 使用 gjson 提取校验需要的字段。
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	apiKey, err = resolveClientGroupForRequest(c, h.backend, apiKey, protocol.ProtocolOpenAIResponses)
	if err != nil {
		writeClientGroupFallbackError(c, err, h.errorResponse)
		return
	}
	if cappedBody, changed, policyErr := h.backend.Reasoning(c, apiKey, body); policyErr != nil {
		h.backend.PolicyDenied(c)
		h.errorResponse(c, http.StatusForbidden, "permission_error", policyErr.Error())
		return
	} else if changed {
		body = cappedBody
	}
	if normalizedBody, changed := h.backend.NormalizeBootstrap(body, false); changed {
		body = normalizedBody
		reqLog.Info("openai.codex_automation_bootstrap_normalized",
			zap.String("normalization", "call_output_to_user_message"),
		)
	}
	if normalizedBody, changed := h.backend.NormalizeBootstrap(body, true); changed {
		body = normalizedBody
		reqLog.Info("openai.codex_delegation_bootstrap_normalized",
			zap.String("normalization", "call_output_to_user_message"),
		)
	}

	reqStream, ok := ParseOpenAICompatibleStream(body)
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", InvalidStreamFieldTypeMessage)
		return
	}
	if err := h.backend.ValidateTier(body); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))
	previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
	if previousResponseID != "" {
		previousResponseIDKind := h.backend.PreviousKind(previousResponseID)
		reqLog = reqLog.With(
			zap.Bool("has_previous_response_id", true),
			zap.String("previous_response_id_kind", previousResponseIDKind),
			zap.Int("previous_response_id_len", len(previousResponseID)),
		)
		if previousResponseIDKind == "message_id" {
			reqLog.Warn("openai.request_validation_failed",
				zap.String("reason", "previous_response_id_looks_like_message_id"),
			)
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id must be a response.id (resp_*), not a message id")
			return
		}
		groupID := int64(0)
		if apiKey.GroupID != nil {
			groupID = *apiKey.GroupID
		}
		owned, ownershipErr := h.backend.ValidateOwner(
			c.Request.Context(),
			groupID,
			previousResponseID,
			subject.UserID,
			apiKey.ID,
		)
		if ownershipErr != nil {
			reqLog.Warn("openai.previous_response_owner_lookup_failed", zap.Error(ownershipErr))
		}
		if !owned {
			reqLog.Warn("openai.request_validation_failed", zap.String("reason", "previous_response_owner_mismatch"))
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id is not available for this user")
			return
		}
	}
	h.backend.SetOwner(c, subject.UserID, apiKey.ID)

	h.backend.ObserveRequest(c, reqModel, reqStream)
	h.backend.ObserveEndpoint(c, reqStream)
	h.backend.Snapshot(c, protocol.ProtocolOpenAIResponses, body)

	if decision := h.backend.Moderate(c, reqLog, apiKey, subject, protocol.ProtocolOpenAIResponses, reqModel, body); decision != nil && decision.Blocked {
		h.errorResponse(c, ModerationHTTPStatus(decision), "content_policy_violation", decision.Message)
		return
	}

	// 分组映射模型 G 决定生图并发和端点能力，客户端模型 R 用于日志与会话。
	// 当前分组和映射结果保存在独立计划中。
	groupMappingRoutePlan := h.backend.Plan(c.Request.Context(), apiKey, reqModel)
	groupMapping := groupMappingRoutePlan.Mapping()
	h.backend.BindPlan(c, groupMappingRoutePlan)
	forwardBody, routingModel, forwardImageIntent := h.backend.ImageIntent(reqModel, body, groupMapping, h.backend.Platform(apiKey))
	forwardModel := strings.TrimSpace(routingModel)
	if forwardModel == "" {
		forwardModel = reqModel
	}
	// 客户端声明的生图意图用于权限、并发和提供商能力检查，宽泛意图用于转发时的工具处理与计费。
	imageIntent := h.backend.ExplicitImageIntent("/v1/responses", routingModel, forwardBody)
	selectionCtx := c.Request.Context()
	if imageIntent {
		// 生图家族限流的上下文标记使用分组映射后的客户端生图意图。
		selectionCtx = h.backend.ImageContext(selectionCtx)
	}
	if imageIntent && !h.backend.AllowsImages(apiKey) {
		h.backend.FeatureDenied(c)
		h.errorResponse(c, http.StatusForbidden, "permission_error", h.backend.ImagePermissionMessage())
		return
	}
	var imageReleaseFunc func()
	if imageIntent {
		var imageAcquired bool
		imageReleaseFunc, imageAcquired = h.backend.ImageSlot(c, streamStarted)
		if !imageAcquired {
			return
		}
		if imageReleaseFunc != nil {
			defer imageReleaseFunc()
		}
	}

	h.backend.SeedImageIntent(c, groupMapping.Mapped, forwardImageIntent)

	// 转发前校验 function_call_output 的关联上下文，缺少上下文会触发上游 400。
	if !h.backend.ValidateTools(c, body, reqLog) {
		return
	}

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	h.backend.BindErrors(c)

	// 订阅为空时保持原余额路径。
	subscription, _ := SubscriptionFromContext(c)
	requestPlatform := h.backend.Platform(apiKey)

	h.backend.AuthLatency(c, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.backend.UserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	// 将已取得用户租约加入保留 HTTP passthrough 标记的选择上下文。
	if lease := scheduler.RequestLease(c.Request.Context()); lease != nil {
		selectionCtx = scheduler.WithRequestLease(selectionCtx, lease)
	}
	// 请求取消时释放槽位，长连接中断也会触发释放。
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 等待成功后按原时点复查资金权益。
	if err := h.backend.Eligibility(c.Request.Context(), apiKey, subscription); err != nil {
		reqLog.Info("openai.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := BillingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// 会话标识仍优先读取 Header，再回退 prompt_cache_key。
	explicitSessionHash := h.backend.SessionHash(c, OpenAIExplicitSession, sessionHashBody)
	sessionHash := h.backend.SessionHash(c, OpenAISelectionSession, sessionHashBody)
	if h.backend.RejectCyber(c, apiKey, sessionHashBody, reqModel, protocol.ProtocolOpenAIResponses) {
		return
	}
	if explicitSessionHash != "" {
		c.Request = c.Request.WithContext(requeststate.WithSessionIsolation(c.Request.Context(), session.SessionIsolationSourceOpenAI, explicitSessionHash))
		if err := h.backend.Isolate(c.Request.Context(), apiKey, subject.UserID, session.SessionIsolationSourceOpenAI, explicitSessionHash); h.handleOpenAISessionIsolationError(c, err, streamStarted) {
			return
		}
	}
	c.Request = c.Request.WithContext(h.backend.GuardianContext(
		c.Request.Context(), c, sessionHashBody, reqModel,
	))
	requireCompact := legacyCompact

	// OpenAI 生图请求选择支持 Responses API 的提供商，Chat Completions 直转无法生图（#4417）。
	// Grok 由 forwardGrokResponses 单独处理。复用准入阶段根据分组模型 G 和 forwardBody 确认的客户端生图意图，省去 tools 重复扫描。
	// Codex 被动 image_gen namespace 保持 Chat-only 提供商的可选资格（#4476）。
	requiredCapability := textflow.RequiredResponsesCapability(
		imageIntent,
		nativeCompactionV2,
		legacyCompact,
		requestPlatform,
	)

	call := OpenAITextCall{
		Route:    groupMappingRoutePlan,
		Protocol: protocol.ProtocolOpenAIResponses, Key: apiKey, Subject: subject, Subscription: subscription,
		Body: body, ForwardBody: forwardBody, SessionHashBody: sessionHashBody,
		Model: reqModel, ForwardModel: forwardModel, SessionHash: sessionHash, PreviousResponseID: previousResponseID,
		Platform: requestPlatform, Stream: reqStream, NativeCompactionV2: nativeCompactionV2, LegacyCompact: legacyCompact,
		RequireCompact: requireCompact, StreamStarted: &streamStarted, SelectionContext: selectionCtx,
		Mapping: groupMapping, RoutingStart: routingStart, RequiredCapability: requiredCapability, Log: reqLog,
	}
	h.executeText(c, call, execution.TextOpenAIResponses)
}
