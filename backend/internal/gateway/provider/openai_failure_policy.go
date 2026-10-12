package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

const (
	openAIRequestBodyTooLargeReason              = forwardcore.GatewayFailureReason("openai_request_body_too_large")
	openAIUpstreamAccessUnavailableClientMessage = "Upstream access is temporarily unavailable, please retry later"
	openAIOAuth429RetryDelay                     = 500 * time.Millisecond
	openAIOAuth429MaxRetryDelay                  = 8 * time.Second
)

// OpenAIFailoverPolicy 计算当前提供商的恢复资格与截止时间。
type OpenAIFailoverPolicy struct {
	Health *provideradapter.OpenAIResponseHealth
}

// ClassifyOpenAIAPIKeyHealthFailure 区分请求取消、平台故障和提供商故障。
func ClassifyOpenAIAPIKeyHealthFailure(err error) (int, []byte, bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0, nil, false
	}

	if failoverErr, ok := errors.AsType[*forwardcore.UpstreamFailoverError](err); ok {
		// 已有独立恢复、同提供商重试或非提供商归因的错误，不进入健康计数。
		if failoverErr.IsCredentialFailure() ||
			failoverErr.RequestScopedTransient ||
			failoverErr.RetryableOnSameProvider ||
			failoverErr.Scope == forwardcore.GatewayFailureScopeRequest ||
			failoverErr.Scope == forwardcore.GatewayFailureScopeShared {
			return failoverErr.StatusCode, failoverErr.ResponseBody, false
		}
		if failoverErr.StatusCode == http.StatusTooManyRequests || failoverErr.StatusCode >= http.StatusInternalServerError {
			return failoverErr.StatusCode, failoverErr.ResponseBody, true
		}
		return failoverErr.StatusCode, failoverErr.ResponseBody, false
	}

	if imageErr, ok := errors.AsType[*openai.OpenAIImagesUpstreamError](err); ok {
		if imageErr.StatusCode == http.StatusTooManyRequests || imageErr.StatusCode >= http.StatusInternalServerError {
			return imageErr.StatusCode, []byte(strings.TrimSpace(imageErr.Message)), true
		}
	}
	return 0, nil, false
}

// IsOpenAIRequestBodyTooLarge 判断当前失败是否仍可通过更换提供商发送相同报文。
func IsOpenAIRequestBodyTooLarge(e *forwardcore.UpstreamFailoverError) bool {
	return e != nil && e.Reason == "openai_request_body_too_large"
}

// IsOpenAICapacityShed 保留请求级瞬态标记与平台报文识别的共同条件。
func IsOpenAICapacityShed(e *forwardcore.UpstreamFailoverError) bool {
	return e != nil && e.RequestScopedTransient && openai.IsOpenAIRequestScopedCapacityShed("", e.ResponseBody)
}

func OpenAI429RetryDelay(headers http.Header, deadline time.Time) time.Duration {
	delay := openAIOAuth429RetryDelay
	now := time.Now()
	if resetAt := openai.ParseRetryAfterResetTime(headers, now); resetAt != nil && resetAt.After(now) {
		delay = resetAt.Sub(now)
	}
	if delay > openAIOAuth429MaxRetryDelay {
		delay = openAIOAuth429MaxRetryDelay
	}
	if remaining := time.Until(deadline); !deadline.IsZero() && delay > remaining {
		delay = remaining
	}
	if delay < 0 {
		return 0
	}
	return delay
}

func ShouldFailoverUpstreamStatus(statusCode int) bool {
	switch statusCode {
	case 401, 402, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

func ShouldFailoverOpenAIResponse(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	// cyber_policy 即使被中间层包成 5xx，仍属于请求拒绝，不触发提供商切换。
	if IsOpenAICyberWarningPayload(upstreamBody, upstreamMsg) {
		return false
	}
	if openai.IsOpenAIContextWindowError(upstreamMsg, upstreamBody) {
		return false
	}
	if IsOpenAIHTTPUpstreamAccessStateError(statusCode, upstreamMsg, upstreamBody) {
		return true
	}
	if IsOpenAIRequestBodyTooLargeError(statusCode, upstreamMsg, upstreamBody) {
		return true
	}
	if ShouldFailoverUpstreamStatus(statusCode) {
		return true
	}
	return openai.IsOpenAITransientProcessingError(statusCode, upstreamMsg, upstreamBody)
}

func IsOpenAIRequestBodyTooLargeError(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	return statusCode == http.StatusRequestEntityTooLarge && !openai.IsOpenAIContextWindowError(upstreamMsg, upstreamBody)
}

func NewOpenAIUpstreamFailure(
	statusCode int,
	responseHeaders http.Header,
	responseBody []byte,
	upstreamMsg string,
	retryableOnSameProvider bool,
) *forwardcore.UpstreamFailoverError {
	requestScopedCapacity := openai.IsOpenAIRequestScopedCapacityShed(upstreamMsg, responseBody)
	failoverErr := &forwardcore.UpstreamFailoverError{
		StatusCode:              statusCode,
		ResponseBody:            responseBody,
		ResponseHeaders:         responseHeaders.Clone(),
		RetryableOnSameProvider: retryableOnSameProvider || requestScopedCapacity,
		RequestScopedTransient:  requestScopedCapacity,
	}
	if IsOpenAIRequestBodyTooLargeError(statusCode, upstreamMsg, responseBody) {
		failoverErr.RetryableOnSameProvider = false
		failoverErr.RequestScopedTransient = false
		failoverErr.Scope = forwardcore.GatewayFailureScopeProvider
		failoverErr.Reason = openAIRequestBodyTooLargeReason
		failoverErr.NextProviderAction = forwardcore.NextProviderRetry
		failoverErr.ClientStatusCode = http.StatusRequestEntityTooLarge
		failoverErr.ClientMessage = forwardcore.OpenAIRequestBodyTooLargeClientMessage
	}
	if IsOpenAIHTTPUpstreamAccessStateError(statusCode, upstreamMsg, responseBody) {
		failoverErr.RetryableOnSameProvider = false
		failoverErr.RequestScopedTransient = false
		failoverErr.Stage = forwardcore.GatewayFailureStageProviderAuth
		failoverErr.Scope = forwardcore.GatewayFailureScopeProvider
		failoverErr.Reason = forwardcore.OpenAIUpstreamAccessStateReason
		failoverErr.NextProviderAction = forwardcore.NextProviderRetry
		failoverErr.ClientStatusCode = http.StatusBadGateway
		failoverErr.ClientMessage = openAIUpstreamAccessUnavailableClientMessage
	} else if requestScopedCapacity {
		// 重试耗尽后保留供应商的过载说明，并按可重试服务端错误返回。
		failoverErr.ClientStatusCode = http.StatusServiceUnavailable
		failoverErr.ClientMessage = OpenAICapacityShedClientMessage(upstreamMsg, responseBody)
	}
	return failoverErr
}

func (p OpenAIFailoverPolicy) NewProviderFailure(
	provider *ExecutionProvider,
	statusCode int,
	responseHeaders http.Header,
	responseBody []byte,
	upstreamMsg string,
	shouldDisable bool,
	retryableOnSameProvider bool,
) *forwardcore.UpstreamFailoverError {
	return p.NewProviderFailureWithClassificationHeaders(provider, statusCode, responseHeaders, responseHeaders, responseBody, upstreamMsg, shouldDisable, retryableOnSameProvider)
}

func (p OpenAIFailoverPolicy) NewProviderFailureWithClassificationHeaders(
	provider *ExecutionProvider,
	statusCode int,
	responseHeaders http.Header,
	classificationHeaders http.Header,
	responseBody []byte,
	upstreamMsg string,
	shouldDisable bool,
	retryableOnSameProvider bool,
) *forwardcore.UpstreamFailoverError {
	oauth429Retry := p.Health.RetryOAuth429(ExecutionRecord(provider), statusCode, shouldDisable, classificationHeaders, responseBody)
	failoverErr := NewOpenAIUpstreamFailure(
		statusCode,
		responseHeaders,
		responseBody,
		upstreamMsg,
		retryableOnSameProvider || oauth429Retry,
	)
	if oauth429Retry {
		failoverErr.SameProviderRetryDeadline = p.Health.RetryDeadline(ExecutionRecord(provider))
		failoverErr.SameProviderRetryDelay = OpenAI429RetryDelay(responseHeaders, failoverErr.SameProviderRetryDeadline)
	}
	return failoverErr
}

func IsOpenAIUpstreamAccessStateError(_ string, body []byte) bool {
	return openai.IsOpenAIUpstreamAccessStateError("", body)
}

func IsOpenAIHTTPUpstreamAccessStateError(_ int, _ string, body []byte) bool {
	return openai.IsOpenAIHTTPUpstreamAccessStateError(0, "", body)
}

func OpenAICapacityShedClientMessage(upstreamMsg string, body []byte) string {
	for _, candidate := range []string{
		upstreamMsg,
		gjson.GetBytes(body, "error.message").String(),
		gjson.GetBytes(body, "response.error.message").String(),
		gjson.GetBytes(body, "message").String(),
	} {
		candidate = logredact.SanitizeUpstreamQueries(strings.TrimSpace(candidate))
		if candidate != "" && openai.IsOpenAICapacityShedMessage(candidate) {
			return candidate
		}
	}
	return "Upstream service is temporarily overloaded, please retry later"
}

// OpenAISemantic429Headers 为 Spark OAuth 请求的流内限流处理提供响应头。
func OpenAISemantic429Headers(target *ExecutionProvider, model string, headers http.Header) http.Header {
	if IsCodexSparkModel(model) && target != nil && target.View().IsOpenAIOAuthLike() {
		return headers
	}
	return nil
}

// OpenAIStreamFailureRetryable 保留容量降载与提供商池模式的同提供商重试资格。
func OpenAIStreamFailureRetryable(provider *ExecutionProvider, payload []byte, message string) bool {
	if provider == nil {
		return false
	}
	// 容量降载由客户端身份或模型容量触发，与当前提供商健康无关；非池提供商也应先
	// 在同一提供商上按重试预算恢复请求。
	if openai.IsOpenAIUpstreamCapacityShedEvent(payload) {
		return true
	}
	if !provider.View().IsPoolMode() {
		return false
	}
	semanticStatus := openai.OpenAIStreamFailedEventSemanticStatus(payload, message)
	return provider.View().IsPoolModeRetryableStatus(semanticStatus) ||
		openai.IsOpenAITransientProcessingError(http.StatusBadRequest, message, payload)
}
