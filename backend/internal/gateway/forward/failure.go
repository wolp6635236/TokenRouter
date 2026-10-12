package forward

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/failover"
)

const (
	NextProviderLegacyRetry NextProviderAction = iota
	NextProviderRetry
	NextProviderStop

	OpenAIRequestBodyTooLargeClientMessage     = "Request payload is too large"
	OpenAIUpstreamAccessStateReason            = GatewayFailureReason("openai_upstream_access_state")
	OpenAIHTTPContinuationUnsupportedReason    = GatewayFailureReason("openai_http_continuation_unsupported")
	AntigravityCredentialRejectedClientMessage = "Antigravity rejected the OAuth credential after refresh; reauthorize the provider and verify project_id"
	AntigravityCredentialRejectedReason        = GatewayFailureReason("antigravity_oauth_credential_rejected")

	openAISilentRefusalErrorCode       = "openai_silent_refusal"
	openAISilentRefusalUpstreamMessage = "OpenAI upstream returned an empty completion stream with finish_reason=stop and no usage"
	openAISilentRefusalClientMessage   = "Upstream returned an empty completion without usage; no fallback provider was available"

	GatewayFailureStageInference    GatewayFailureStage = "inference"
	GatewayFailureStageProviderAuth GatewayFailureStage = "provider_auth"

	GatewayFailureScopeProvider GatewayFailureScope = "provider"
	GatewayFailureScopeShared   GatewayFailureScope = "shared"
	GatewayFailureScopeRequest  GatewayFailureScope = "request"
)

// AgentIdentityTaskRecoveredError 表示身份任务已恢复，调用方可重试请求。
type AgentIdentityTaskRecoveredError struct{}

// GatewayFailureStage 标识请求失败的阶段，零值表示推理阶段。
type GatewayFailureStage string

// GatewayFailureScope 区分单个提供商、共享凭据设施和请求本身，供切换决策使用。
type GatewayFailureScope string

// NextProviderAction 描述提供商切换动作，零值继续尝试，NextProviderStop 终止切换。
type NextProviderAction uint8

type GatewayFailureReason string

// UpstreamFailoverError 表示可能触发提供商切换的上游或凭据错误。
// 切换动作缺省时按错误状态和重试预算决定下一步。
type UpstreamFailoverError struct {
	StatusCode                int
	ResponseBody              []byte              // 上游响应体，用于错误透传规则匹配
	ResponseHeaders           map[string][]string // 上游响应头值，供 HTTP 适配器读取。
	ForceCacheBilling         bool                // Antigravity 粘性会话切换时设为 true
	RetryableOnSameProvider   bool                // 临时性错误（如 Google 间歇性 400、空响应），应在同一提供商上重试 N 次再切换
	SameProviderRetryDelay    time.Duration
	SameProviderRetryDeadline time.Time
	SameProviderRetryMax      int  // 可选的错误级同提供商重试上限，低于 handler 默认预算时优先采用
	RequestScopedTransient    bool // 故障因素与提供商无关（如上游按客户端身份/模型容量降载）：可同提供商重试，但不得据此对提供商做临时封禁
	SafeToFailoverAfterWrite  bool // 已写出的内容仅为 SSE 注释等控制字节时，允许在当前流中切换提供商。
	Stage                     GatewayFailureStage
	Scope                     GatewayFailureScope
	Reason                    GatewayFailureReason
	NextProviderAction        NextProviderAction
	ClientStatusCode          int
	ClientMessage             string
}

func (e *AgentIdentityTaskRecoveredError) Error() string { return "agent identity task recovered" }

// OpenAISilentRefusalErrorBody 返回网关用于静默拒绝的错误码和安全消息。
func OpenAISilentRefusalErrorBody() []byte {
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    "upstream_error",
			"code":    openAISilentRefusalErrorCode,
			"message": openAISilentRefusalUpstreamMessage,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"upstream_error","code":"openai_silent_refusal","message":"OpenAI upstream returned an empty completion stream with finish_reason=stop and no usage"}}`)
	}
	return body
}

// IsOpenAISilentRefusalErrorBody 判断响应体是否由 OpenAI 静默拒绝检测器生成。
func IsOpenAISilentRefusalErrorBody(body []byte) bool {
	return strings.TrimSpace(gjson.GetBytes(body, "error.code").String()) == openAISilentRefusalErrorCode
}

// OpenAISilentRefusalClientMessage 返回静默拒绝且 failover 耗尽时给客户端看的错误文案。
func OpenAISilentRefusalClientMessage() string {
	return openAISilentRefusalClientMessage
}

func (e *UpstreamFailoverError) Error() string {
	if e != nil && e.Stage == GatewayFailureStageProviderAuth {
		return fmt.Sprintf("credential failure: %s (failover)", e.Reason)
	}
	return fmt.Sprintf("upstream error: %d (failover)", e.StatusCode)
}

func (e *UpstreamFailoverError) ShouldRetryNextProvider() bool {
	return e != nil && e.NextProviderAction != NextProviderStop
}

func (e *UpstreamFailoverError) IsCredentialFailure() bool {
	return e != nil && e.Stage == GatewayFailureStageProviderAuth
}

// ShouldReportProviderScheduleFailure 区分凭据失败归属，提供方级和请求级失败由对应范围处理。
// 其他错误和推理失败继续向提供商调度健康报告。
func (e *UpstreamFailoverError) ShouldReportProviderScheduleFailure() bool {
	if e == nil {
		return false
	}
	return !e.IsCredentialFailure() || e.Scope == GatewayFailureScopeProvider
}

func (e *UpstreamFailoverError) RetryFailure() *failover.FailureInfo {
	if e == nil {
		return nil
	}
	return &failover.FailureInfo{StatusCode: e.StatusCode, ForceCacheBilling: e.ForceCacheBilling, RetryableOnSameProvider: e.RetryableOnSameProvider, RequestScopedTransient: e.RequestScopedTransient, SameProviderRetryDelay: e.SameProviderRetryDelay, SameProviderRetryDeadline: e.SameProviderRetryDeadline, SameProviderRetryMax: e.SameProviderRetryMax, RetryNext: e.ShouldRetryNextProvider()}
}
