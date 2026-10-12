package provider

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	upstreamopenai "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// IsContentPolicyRejection 识别只由当前请求内容触发的 OpenAI 安全拒绝。
// 这类错误即使通过流内事件被推断为 502，也不能命中提供商级自定义错误策略。
func IsContentPolicyRejection(responseBody []byte) bool {
	if len(responseBody) == 0 {
		return false
	}
	for _, path := range []string{
		"error.code",
		"error.type",
		"response.error.code",
		"response.error.type",
	} {
		marker := strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, path).String()))
		if strings.Contains(marker, "content_policy") ||
			strings.Contains(marker, "content_filter") ||
			strings.Contains(marker, "safety") ||
			strings.Contains(marker, "moderation") {
			return true
		}
	}
	for _, path := range []string{"error.message", "response.error.message", "message"} {
		message := strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, path).String()))
		if strings.Contains(message, "content policy") ||
			strings.Contains(message, "blocked by policy") ||
			strings.Contains(message, "safety system") ||
			strings.Contains(message, "violates our policies") {
			return true
		}
	}
	return false
}

// IsRequestScopedProviderFailure 识别应按单次请求处理的提供商错误。
// 413 请求体限制可能是提供商上游代理的独有限制，仍允许切换提供商，因此不在这里统一排除。
func IsRequestScopedProviderFailure(provider *ExecutionProvider, statusCode int, responseBody []byte) bool {
	upstreamMsg := strings.TrimSpace(upstream.ExtractErrorMessage(responseBody))
	if hit, _, _ := upstreamopenai.DetectOpenAICyberPolicy(responseBody); hit {
		return true
	}
	if IsOpenAICyberWarningPayload(responseBody, upstreamMsg) ||
		IsContentPolicyRejection(responseBody) ||
		upstreamopenai.IsOpenAIClientInvalidRequestError(statusCode, upstreamMsg, responseBody) ||
		upstreamopenai.IsOpenAIContextWindowError(upstreamMsg, responseBody) {
		return true
	}
	return provider != nil && provider.Record.Platform == capability.PlatformGrok && grok.IsGrokContentPolicyRejection(statusCode, responseBody)
}

// IsTransientProviderFailure 保留状态码与请求处理错误的短期恢复分类。
func IsTransientProviderFailure(statusCode int, responseBody []byte) bool {
	switch statusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 520, 521, 522, 523, 524:
		return true
	case http.StatusBadRequest:
		return upstreamopenai.IsOpenAITransientProcessingError(statusCode, "", responseBody)
	default:
		return false
	}
}

// OpenAIWSHTTPBridgeRequestScopedError 识别只与当前请求有关的错误。
// 这类错误既不修改提供商状态，也不能因为池模式配置而回放当前 turn。
func OpenAIWSHTTPBridgeRequestScopedError(provider *ExecutionProvider, statusCode int, message string, body []byte) bool {
	if hit, _, _ := upstreamopenai.DetectOpenAICyberPolicy(body); hit {
		return true
	}
	if IsOpenAICyberWarningPayload(body, message) ||
		upstreamopenai.IsOpenAIClientInvalidRequestError(statusCode, message, body) ||
		upstreamopenai.IsOpenAIContextWindowError(message, body) {
		return true
	}
	return provider != nil && provider.Record.Platform == capability.PlatformGrok && grok.IsGrokContentPolicyRejection(statusCode, body)
}
