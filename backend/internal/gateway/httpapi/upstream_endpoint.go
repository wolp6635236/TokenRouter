package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	protocolcore "github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const openAIUpstreamEndpointContextKey = "openai_actual_upstream_endpoint"

// resolveOpenAITextProtocolForAttempt 解析当前提供商的文本协议，并在转发前写入当前尝试的端点元数据。
func resolveOpenAITextProtocolForAttempt(
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	preferred providercore.TextProtocol,
) providercore.TextProtocol {
	// OAuth 等专用提供商始终保留既有 Responses 桥；只有 API Key 提供商参与
	// “客户端首选协议 + 路由模式 + 探测状态”的普通文本协议解析。
	protocol := providercore.TextProtocolResponses
	if provider != nil && provider.Record.Type == capability.ProviderTypeAPIKey {
		protocol = providercore.ResolveUpstreamTextProtocol(provider.Record.Extra, preferred)
	}

	if provider != nil && provider.Route.Protocol() == protocolcore.ProtocolOpenAIChatCompletions {
		protocol = providercore.TextProtocolChatCompletions
	}
	if provider != nil && provider.Route.Protocol() == protocolcore.ProtocolOpenAIResponses {
		protocol = providercore.TextProtocolResponses
	}
	endpoint := "/v1/responses"
	if protocol == providercore.TextProtocolChatCompletions {
		endpoint = "/v1/chat/completions"
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	return protocol
}

// SetActualOpenAIUpstreamEndpoint 记录当前转发尝试选择的端点，供无法取得
// OpenAIForwardResult 的错误路径记录用量和运维日志。
func SetActualOpenAIUpstreamEndpoint(c *gin.Context, endpoint string) {
	if c == nil {
		return
	}
	if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
		c.Set(openAIUpstreamEndpointContextKey, endpoint)
	}
}

// ClearActualOpenAIUpstreamEndpoint 清除当前转发尝试记录的端点。
// Gin context 在提供商尝试间复用，每次转发前清理上一次的端点。
func ClearActualOpenAIUpstreamEndpoint(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(openAIUpstreamEndpointContextKey, "")
}

// GetActualOpenAIUpstreamEndpoint 返回该请求最近一次转发尝试记录的端点。
func GetActualOpenAIUpstreamEndpoint(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, exists := c.Get(openAIUpstreamEndpointContextKey)
	if !exists {
		return ""
	}
	endpoint, _ := value.(string)
	return strings.TrimSpace(endpoint)
}
