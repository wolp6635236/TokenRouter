package openaiattempt

import (
	"net/http"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/billing"

	"github.com/TokenFlux/TokenRouter/internal/gateway/errorpolicy"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/moderationflow"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaysession "github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/server/clientip"
	openaierrors "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// OpenAIProviderScheduleModel 保留实际输出模型、请求观测和提供商规则的原优先级。
func OpenAIProviderScheduleModel(c *gin.Context, provider *gatewayprovider.ExecutionProvider, forwardModel string, requireCompact bool, result *forwardcore.OpenAIResult) string {
	if result != nil {
		if actual := strings.TrimSpace(result.UpstreamModel); actual != "" {
			return actual
		}
	}
	if c != nil {
		if value, ok := c.Get(gatewayhttp.OpsUpstreamModelKey); ok {
			if actual, ok := value.(string); ok && strings.TrimSpace(actual) != "" {
				return strings.TrimSpace(actual)
			}
		}
	}
	return gatewayprovider.ExecutionModelPolicy(provider).OpenAIUpstream(forwardModel, requireCompact)
}

// AppendOpenAIProviderProxyLogFields 追加可公开的代理定位字段。
func AppendOpenAIProviderProxyLogFields(fields []zap.Field, provider *gatewayprovider.ExecutionProvider) []zap.Field {
	if provider == nil {
		return fields
	}
	if provider.Record.Proxy != nil {
		return append(fields,
			zap.Int64("proxy_id", provider.Record.Proxy.ID),
			zap.String("proxy_name", provider.Record.Proxy.Name),
			zap.String("proxy_host", provider.Record.Proxy.Host),
			zap.Int("proxy_port", provider.Record.Proxy.Port),
		)
	}
	if provider.Record.ProxyID != nil {
		return append(fields, zap.Int64p("proxy_id", provider.Record.ProxyID))
	}
	return fields
}

// HandleOpenAISelectionBusinessError 将选组业务错误写为 OpenAI 响应。
func (h *Support) HandleOpenAISelectionBusinessError(c *gin.Context, err error, streamStarted bool) bool {
	return handleGroupSelectionBusinessError(c, err, streamStarted, func(status int, errType string, message string, streamStarted bool) {
		gatewayhttp.DefaultOpenAIErrorOutput().StreamError(c, status, errType, message, streamStarted)
	})
}

// HandleAnthropicFailoverExhausted 将上游切号错误转换为 Anthropic 格式。
func (h *Support) HandleAnthropicFailoverExhausted(c *gin.Context, failoverErr *forwardcore.UpstreamFailoverError, streamStarted bool) {
	if failoverErr != nil && gatewayprovider.IsOpenAIRequestBodyTooLarge(failoverErr) {
		gatewayhttp.SetOpsUpstreamError(c, http.StatusRequestEntityTooLarge, forwardcore.OpenAIRequestBodyTooLargeClientMessage, "")
		gatewayhttp.DefaultOpenAIErrorOutput().WriteAnthropicStreamingError(
			c,
			http.StatusRequestEntityTooLarge,
			"invalid_request_error",
			forwardcore.OpenAIRequestBodyTooLargeClientMessage,
			streamStarted,
		)
		return
	}
	if failoverErr != nil {
		gatewayhttp.CopyFailoverRetryAfter(c, failoverErr.ResponseHeaders)
	}
	if failoverErr != nil && failoverErr.IsCredentialFailure() {
		status, message := gatewayhttp.CredentialFailoverClientResponse(failoverErr)
		gatewayhttp.DefaultOpenAIErrorOutput().WriteAnthropicStreamingError(c, status, "api_error", message, streamStarted)
		return
	}
	if failoverErr != nil && gatewayprovider.IsOpenAICapacityShed(failoverErr) && strings.TrimSpace(failoverErr.ClientMessage) != "" {
		status := failoverErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		gatewayhttp.DefaultOpenAIErrorOutput().WriteAnthropicStreamingError(c, status, "api_error", failoverErr.ClientMessage, streamStarted)
		return
	}
	status, errType, errMsg := gatewayhttp.MapOpenAIUpstreamError(failoverErr.StatusCode)
	gatewayhttp.DefaultOpenAIErrorOutput().WriteAnthropicStreamingError(c, status, errType, errMsg, streamStarted)
}

// EnsureAnthropicErrorResponse 只在尚未写出响应时补充原 Anthropic 错误。
func (h *Support) EnsureAnthropicErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return false
	}
	gatewayhttp.DefaultOpenAIErrorOutput().WriteAnthropicStreamingError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
	return true
}

func (h *Support) AcquireResponsesProviderSlot(
	c *gin.Context,
	groupID *int64,
	sessionHash string,
	selection *gatewayprovider.SelectionResult,
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (func(), bool) {
	release, result := h.AcquireOpenAIProviderSlot(c, groupID, sessionHash, selection, reqStream, streamStarted, reqLog, nil)
	return release, result
}

// AcquireOpenAIProviderSlot centralizes scheduler selection admission. The
// optional error writer lets non-Responses endpoints retain their wire format
// while sharing the same WaitPlan, cancellation, and release semantics.
func (h *Support) AcquireOpenAIProviderSlot(
	c *gin.Context,
	groupID *int64,
	sessionHash string,
	selection *gatewayprovider.SelectionResult,
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
	writeError func(int, string, string, string),
) (func(), bool) {
	if writeError == nil {
		writeError = func(status int, errType, code, message string) {
			gatewayhttp.DefaultOpenAIErrorOutput().WriteStreamingErrorWithCode(c, status, errType, code, message, *streamStarted, false)
		}
	}
	var projected *gatewayhttp.SelectedProviderSlot
	if selection != nil && selection.Provider != nil {
		projected = &gatewayhttp.SelectedProviderSlot{CompleteBeforeRelease: reqStream && selection.Provider.Record.Platform == capability.PlatformQoder, ProviderID: selection.Provider.Record.ID, Acquired: selection.Acquired, ReleaseFunc: selection.ReleaseFunc, WaitPlan: selection.WaitPlan}
	}
	release, ok := gatewayhttp.AcquireSelectedProviderSlot(c, groupID, sessionHash, projected, reqStream, streamStarted, reqLog, writeError, h.Concurrency, h.Sticky, gatewayhttp.ProviderSlotHooks{Acquired: gatewayhttp.MarkOpsProviderSlotAcquired, CapacityLimited: gatewayhttp.MarkOpsRoutingCapacityLimited})
	if !ok {
		return release, false
	}
	return release, true
}

// GetContextInt64 保留历史 HTTP 观测数值类型的兼容读取。
func GetContextInt64(c *gin.Context, key string) (int64, bool) {
	if c == nil || key == "" {
		return 0, false
	}
	v, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	default:
		return 0, false
	}
}

func (h *Support) HandleFailoverExhausted(c *gin.Context, failoverErr *forwardcore.UpstreamFailoverError, streamStarted bool) {
	var rules gatewayhttp.ErrorRuleMatcher
	if h.Rules != nil {
		rules = h.Rules
	}
	gatewayhttp.DefaultOpenAIErrorOutput().WriteFailoverExhausted(c, gatewayhttp.ProjectOpenAIFailoverError(failoverErr), streamStarted, rules, gatewayhttp.FailoverErrorHooks{Upstream: func(c *gin.Context, status int, message string) {
		gatewayhttp.SetOpsUpstreamError(c, status, message, "")
	}, SkipMonitoring: func(c *gin.Context) { c.Set(gatewayhttp.OpsSkipPassthroughKey, true) }})
}

// HandleFailoverExhaustedSimple 简化版本，用于没有响应体的情况
func (h *Support) HandleFailoverExhaustedSimple(c *gin.Context, statusCode int, streamStarted bool) {
	status, errType, errMsg := gatewayhttp.MapOpenAIUpstreamError(statusCode)
	gatewayhttp.SetOpsUpstreamError(c, statusCode, errMsg, "")
	gatewayhttp.DefaultOpenAIErrorOutput().StreamError(c, status, errType, errMsg, streamStarted)
}

func (h *Support) EnsureOpenAIStreamReadErrorResponse(c *gin.Context, err error, streamStarted bool) bool {
	code, message, ok := openaierrors.OpenAIUpstreamStreamReadErrorDetails(err)
	if !ok || c == nil || c.Writer == nil || gatewayhttp.IsResponseCommitted(c) {
		return false
	}
	if c.Writer.Written() {
		streamStarted = true
	}
	gatewayhttp.DefaultOpenAIErrorOutput().WriteStreamingErrorWithCode(
		c, http.StatusBadGateway, "upstream_error", code, message, streamStarted, true,
	)
	return true
}

func (h *Support) RecordCyberPolicyIfMarked(c *gin.Context, apiKey *apikey.APIKey, provider *gatewayprovider.ExecutionProvider, subscription *billing.UserSubscription, model string, forwardErrored bool, cyberBlockArg any, pricingFields routing.PricingUsageFields, requestPayloadHash string, nativeCompaction ...bool) bool {
	mark := gatewayhttp.GetOpsCyberPolicy(c)
	if mark == nil || c == nil {
		return false
	}
	call := gatewayhttp.CyberPolicyCall{
		Key:            apikey.CopyAPIKey(apiKey),
		Provider:       moderationProviderView(provider),
		Model:          model,
		ForwardErrored: forwardErrored,
	}
	switch value := cyberBlockArg.(type) {
	case string:
		call.BlockKey = value
	case []byte:
		if apiKey != nil {
			plan := buildCyberSessionBlockWritePlan(apiKey.ID, c, value)
			call.Plan = moderationflow.BlockPlan{
				ScopeKey: plan.scopeKey,
				Keys:     plan.keys,
			}
			call.HasPlan = true
		}
	}
	// 所有旧实体在提交前转为独立完成快照；后台闭包不持有 Gin 或后续可变的 turn 数据。
	compaction := gatewayhttp.IsOpenAINativeCompactionV2(c)
	if len(nativeCompaction) > 0 {
		compaction = nativeCompaction[0]
	}
	platform := capability.PlatformOpenAI
	if provider != nil && strings.TrimSpace(provider.Record.Platform) != "" {
		platform = provider.Record.Platform
	}
	call.Usage = gatewayprovider.CaptureCyber(c.Request.Context(), gatewayprovider.CyberCapture{
		APIKey:             apiKey,
		Provider:           gatewayprovider.ExecutionCompletionRecord(provider),
		Subscription:       subscription,
		RequestID:          c.Writer.Header().Get("X-Request-Id"),
		Model:              model,
		Stream:             requestIsStream(c),
		InputTokens:        mark.UpstreamInTok,
		OutputTokens:       mark.UpstreamOutTok,
		InboundEndpoint:    gatewayhttp.GetInboundEndpoint(c),
		UpstreamEndpoint:   gatewayhttp.GetUpstreamEndpoint(c, platform),
		UserAgent:          c.GetHeader("User-Agent"),
		IPAddress:          clientip.GetClientIP(c),
		ClientSessionID:    gatewayhttp.ExtractClientSessionID(c),
		RequestPayloadHash: requestPayloadHash,
		APIKeyService:      h.Quota,

		NativeCompactionV2: compaction,
		PricingUsageFields: pricingFields,
	})
	return h.Cyber.RecordPolicy(c, call)
}

func requestIsStream(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if v, ok := c.Get(gatewayhttp.OpsStreamKey); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func OpenAIForwardMayFailover(c *gin.Context, writerSizeBeforeForward int, failoverErr *forwardcore.UpstreamFailoverError) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	if gatewayhttp.OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return true
	}
	return failoverErr != nil && failoverErr.SafeToFailoverAfterWrite
}

func OpenAIRequestAllowsFailoverReplay(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return !gatewayhttp.FailoverClientGone(c)
}

func EnsureOpenAIPoolModeSessionHash(sessionHash string, provider *gatewayprovider.ExecutionProvider) string {
	if sessionHash != "" || provider == nil || !provider.View().IsPoolMode() {
		return sessionHash
	}
	// 为当前请求生成粘性会话键，同提供商重试复用该提供商。
	return "openai-pool-retry-" + uuid.NewString()
}

type cyberSessionBlockWritePlan struct {
	scopeKey string
	keys     []string
}

func buildCyberSessionBlockWritePlan(apiKeyID int64, c *gin.Context, body []byte) cyberSessionBlockWritePlan {
	explicit := gatewayhttp.CyberSessionExplicitBlockKey(apiKeyID, c, body)
	transcript := gatewaysession.CyberSessionTranscriptBlockKeys(apiKeyID, body)
	scope := ""
	if len(transcript) > 0 {
		scope = cyberSessionScopeKey(apiKeyID, c)
	}
	plan := moderationflow.BuildBlockPlan(explicit, transcript, scope)
	return cyberSessionBlockWritePlan{scopeKey: plan.ScopeKey, keys: plan.Keys}
}

func cyberSessionScopeKey(apiKeyID int64, c *gin.Context) string {
	if c == nil {
		return ""
	}
	return gatewaysession.CyberSessionScopeKey(apiKeyID, strings.TrimSpace(clientip.GetClientIP(c)), c.GetHeader("User-Agent"))
}

// handleGroupSelectionBusinessError 读取 Key 并使用平台目录生成业务错误响应。
func handleGroupSelectionBusinessError(c *gin.Context, err error, started bool, write func(int, string, string, bool)) bool {
	return gatewayhttp.WriteGroupSelectionBusinessError(c, err, started, keyhttp.GetAPIKeyFromContext, gatewayprovider.ModelDisplayCatalogue{}, write)
}

// Support 为文本、WS 和媒体适配提供共享的请求资源和用例接口。
type Support struct {
	Concurrency *gatewayhttp.ConcurrencyHelper
	Sticky      gatewayhttp.SlotStickyBinder
	Rules       *errorpolicy.ErrorPassthroughService
	Cyber       *gatewayhttp.CyberHandler
	Quota       gatewayprovider.QuotaUpdater
	Moderation  gatewayhttp.ModerationPort
	Submission  gatewayhttp.CompletionSubmission
}
