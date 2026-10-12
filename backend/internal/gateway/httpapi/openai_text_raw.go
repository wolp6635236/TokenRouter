package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	openaiexecution "github.com/TokenFlux/TokenRouter/internal/gateway/provider/openaiforward"
)

// RawChat 将 Chat Completions 请求直转至 {base_url}/v1/chat/completions。
// 模型 ID 改写后，SSE chunk 和非流式 JSON 原样透传，并按需提取 usage。
// 路由适用于已确认缺少 Responses 端点的 OpenAI API Key 提供商，以及配置固定 chat_completions 协议的 CN 供应商。
// 该路径使用 API Key 请求头和正文处理，OAuth 的请求变换与 prompt_cache_key 注入由 OAuth 路径处理。
func (s *OpenAITextExecutor) RawChat(
	ctx context.Context,
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	body []byte,
	defaultMappedModel string,
	tlsRouterMatch ...egress.TLSFingerprintRouterMatchResult,
) (*forwardcore.OpenAIResult, error) {
	adapter := &openAIRawChatAdapter{openAIRawFallbackAdapter: &openAIRawFallbackAdapter{openAIMessagesExecutionAdapter: &openAIMessagesExecutionAdapter{s: s, c: c, provider: provider, tls: tlsRouterMatch}, kind: openaiexecution.NativeChat}}
	result, err := openaiexecution.RunRawChat(ctx, body, defaultMappedModel, adapter)
	out := openaiexecution.ToForwardResult(result)
	captureResponseModel(c, out)
	return out, err
}

// MessagesViaRawChat 将 /v1/messages 请求转换为 Chat Completions，响应转换为 Anthropic 事件或正文。
// 它使用单个流式状态机，与服务 /v1/responses 的 ResponsesViaRawChat 分别处理各自客户端协议。
func (s *OpenAITextExecutor) MessagesViaRawChat(
	ctx context.Context,
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	body []byte,
	defaultMappedModel string,
	tlsRouterMatch ...egress.TLSFingerprintRouterMatchResult,
) (*forwardcore.OpenAIResult, error) {
	adapter := &openAIRawFallbackAdapter{openAIMessagesExecutionAdapter: &openAIMessagesExecutionAdapter{s: s, c: c, provider: provider, tls: tlsRouterMatch}, kind: openaiexecution.NativeMessages}
	result, err := openaiexecution.MessagesViaRawChat(ctx, body, defaultMappedModel, adapter)
	out := openaiexecution.ToForwardResult(result)
	captureResponseModel(c, out)
	return out, err
}

// ResponsesViaRawChat 将 `/v1/responses` 入站请求桥接到
// 只支持 `/v1/chat/completions` 的上游。
func (s *OpenAITextExecutor) ResponsesViaRawChat(
	ctx context.Context,
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	body []byte,
	tlsRouterMatch ...egress.TLSFingerprintRouterMatchResult,
) (*forwardcore.OpenAIResult, error) {
	adapter := &openAIRawFallbackAdapter{openAIMessagesExecutionAdapter: &openAIMessagesExecutionAdapter{s: s, c: c, provider: provider, tls: tlsRouterMatch}, kind: openaiexecution.NativeResponses}
	result, err := openaiexecution.ResponsesViaRawChat(ctx, body, adapter)
	out := openaiexecution.ToForwardResult(result)
	captureResponseModel(c, out)
	return out, err
}
