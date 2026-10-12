package provider

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

func GrokContentPolicyClientMessage(responseBody []byte) string {
	return grok.GrokContentPolicyClientMessage(logredact.SanitizeUpstreamQueries(strings.TrimSpace(upstream.ExtractErrorMessage(responseBody))))
}

// ShouldFailoverGrokResponse 在状态码之外结合响应体判断是否故障转移。
// Grok 内容拒绝由当前提供商返回调用方。
func ShouldFailoverGrokResponse(statusCode int, responseBody []byte) bool {
	if grok.IsGrokContentPolicyRejection(statusCode, responseBody) {
		return false
	}
	// ModelInput 解码返回 422 时，请求可切换到兼容的提供商。
	if grok.IsGrokDecoderCompatibilityError(statusCode, responseBody) {
		return true
	}
	// xAI 某些兼容端点用 405 表示当前提供商不支持该接口；切换提供商后仍可能
	// 命中另一种能力配置，因此不能沿用 OpenAI 通用状态码集合将其留在原提供商。
	if statusCode == http.StatusMethodNotAllowed {
		return true
	}
	decision := grok.ClassifyGrokUpstreamFailure(statusCode, responseBody, "")
	switch decision.Class {
	case grok.GrokFailureFreeUsage, grok.GrokFailureEmptyUpstream, grok.GrokFailureBilling, grok.GrokFailureModelCapacity, grok.GrokFailureCompatibility:
		return decision.ShouldFailover
	}
	return ShouldFailoverUpstreamStatus(statusCode)
}

// GrokRetryableOnSameProvider 标记共享 failover 循环可在同提供商重试的瞬态错误。
// 模型容量压力允许有限重试；免费额度和计费耗尽属于提供商状态，应立即切换提供商。
func GrokRetryableOnSameProvider(provider *ExecutionProvider, statusCode int, responseBody []byte) bool {
	if provider == nil || !provider.View().IsGrok() {
		return false
	}
	// 配置的错误码策略优先于池模式默认状态列表，命中后交给外层故障转移处理。
	if provider.View().IsCustomErrorCodesEnabled() && provider.View().ShouldHandleErrorCode(statusCode) {
		return false
	}
	decision := grok.ClassifyGrokUpstreamFailure(statusCode, responseBody, "")
	switch decision.Class {
	case grok.GrokFailureFreeUsage, grok.GrokFailureBilling, grok.GrokFailureCompatibility:
		// 额度和权益耗尽不能靠同提供商重放恢复。
		return false
	case grok.GrokFailureModelCapacity:
		if statusCode == http.StatusTooManyRequests {
			return true
		}
	}
	return provider.View().IsPoolMode() && provider.View().IsPoolModeRetryableStatus(statusCode)
}

func GrokSameProviderRetryMetadata(provider *ExecutionProvider, statusCode int, responseBody []byte) (bool, time.Duration, time.Time, int) {
	if !GrokRetryableOnSameProvider(provider, statusCode, responseBody) {
		return false, 0, time.Time{}, 0
	}
	decision := grok.ClassifyGrokUpstreamFailure(statusCode, responseBody, "")
	if decision.Class != grok.GrokFailureModelCapacity {
		return true, 0, time.Time{}, 0
	}
	// 每次上游尝试都会重新构造错误，因此错误上的截止时间不能覆盖整个请求；
	// 容量错误最多重放一次，首次尝试超过 30 秒时也适用。
	return true, 500 * time.Millisecond, time.Now().Add(30 * time.Second), 1
}

// GrokStreamIdleFailure 构造响应提交前可见的切换错误，使挂起 Grok 流能更换 OAuth 提供商。
func GrokStreamIdleFailure(provider *ExecutionProvider, idle time.Duration) *forwardcore.UpstreamFailoverError {
	msg := fmt.Sprintf("Grok stream idle timeout after %s with no upstream data", idle.Round(time.Second))
	return &forwardcore.UpstreamFailoverError{
		StatusCode:               502,
		ResponseBody:             []byte(`{"error":{"code":"empty_upstream","message":"` + strings.ReplaceAll(msg, `"`, `'`) + `"}}`),
		SafeToFailoverAfterWrite: true,
		// 空闲上游流属于瞬时故障，先使用同提供商重试预算再切换凭据；
		// handler 仍负责执行请求级重试上限。
		RetryableOnSameProvider: provider != nil && provider.Record.Platform == capability.PlatformGrok,
		RequestScopedTransient:  true,
		// 空闲失败后最多重放一次，截止时间从失败时刻计算。
		// 超时后切换提供商。
		SameProviderRetryMax:      1,
		SameProviderRetryDeadline: time.Now().Add(idle),
	}
}
