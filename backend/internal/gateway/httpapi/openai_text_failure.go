package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// httpFailover 对 >=400 的上游响应做 failover 判定：命中时
// 记录 ops 事件、执行提供商级错误处置并返回 *UpstreamFailoverError；未命中返回
// nil，调用方继续走各自端点格式的非 failover 错误处理链。
func (s *OpenAITextExecutor) httpFailover(
	ctx context.Context,
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	resp *http.Response,
	respBody []byte,
	upstreamMsg string,
	upstreamModel string,
) *forwardcore.UpstreamFailoverError {
	shouldFailover := gatewayprovider.ShouldFailoverOpenAIResponse(resp.StatusCode, upstreamMsg, respBody)
	if provider != nil && provider.Record.Platform == capability.PlatformGrok {
		shouldFailover = gatewayprovider.ShouldFailoverGrokResponse(resp.StatusCode, respBody)
	}
	// 请求级拒绝不能触发提供商策略或池模式重试。
	if detectHit, _, _ := openai.DetectOpenAICyberPolicy(respBody); detectHit || gatewayprovider.IsOpenAICyberWarningPayload(respBody, upstreamMsg) ||
		openai.IsOpenAIClientInvalidRequestError(resp.StatusCode, upstreamMsg, respBody) ||
		openai.IsOpenAIContextWindowError(upstreamMsg, respBody) ||
		(provider != nil && provider.Record.Platform == capability.PlatformGrok && grok.IsGrokContentPolicyRejection(resp.StatusCode, respBody)) {
		return nil
	}
	// 缺少 Gin 上下文时，按默认故障转移分类决定是否执行提供商策略。
	if c == nil && !shouldFailover && (provider == nil || provider.Record.Platform != capability.PlatformGrok) {
		return nil
	}
	var decision providercore.UpstreamErrorDecision
	if provider != nil && provider.Record.Platform == capability.PlatformGrok {
		decision = gatewayprovider.ApplyGrokExecutionHealth(ctx, s.Grok.Health, provider, resp.StatusCode, resp.Header, respBody, "", upstreamModel)
	} else {
		decision = gatewayprovider.ApplyOpenAIResponseHealth(ctx, s.Output.Health, provider, resp.StatusCode, resp.Header, respBody, false, upstreamModel)
	}
	if decision.ShouldReturnGenericError() || !decision.ShouldFailover(gatewayprovider.ExecutionErrorPolicy(provider), resp.StatusCode, shouldFailover) {
		return nil
	}
	upstreamDetail := ""
	if s.Output.Options.LogUpstreamErrorBody {
		maxBytes := s.Output.Options.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = logredact.TruncateUTF8(string(respBody), maxBytes)
	}
	AppendOpsUpstreamError(c, ops.OpsUpstreamErrorEvent{
		Platform:           provider.Record.Platform,
		ProviderID:         provider.Record.ID,
		ProviderName:       provider.Record.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  resp.Header.Get("x-request-id"),
		Kind:               "failover",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	return gatewayprovider.NewOpenAIUpstreamFailure(
		resp.StatusCode,
		resp.Header,
		respBody,
		upstreamMsg,
		decision.RetryableOnSameProvider(gatewayprovider.ExecutionErrorPolicy(provider), resp.StatusCode),
	)
}
