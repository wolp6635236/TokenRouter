package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/clientmeta"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaysession "github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

const (
	grokClientToolCacheOptInHeader       = "X-TokenRouter-Grok-Client-Tool-Cache"
	legacyGrokClientToolCacheOptInHeader = "X-Sub2API-Grok-Client-Tool-Cache"
)

// ExtractClaudeCodeSessionID 从请求头或 Anthropic/OpenAI 兼容载荷元数据中提取
// Claude Code 会话标识。
func ExtractClaudeCodeSessionID(c *gin.Context, body []byte) string {
	if c != nil {
		if seed := strings.TrimSpace(c.GetHeader(ClaudeCodeSessionHeader)); seed != "" {
			return seed
		}
	}
	return grok.ExtractClaudeCodeSessionIDFromPayload(body)
}

func ResolveGrokCacheIdentity(c *gin.Context, body []byte, explicitKey, upstreamModel string) string {
	return grok.ResolveCacheIdentity(grokCacheInput(c, explicitKey, upstreamModel), body)
}

// ApplyGrokFreeRequestToolCacheRoute 还接受请求级开关。该兼容协议头仅在本地消费，
// buildGrokResponsesRequest 只向上游转发明确支持的 OpenAI-Beta 头。
func ApplyGrokFreeRequestToolCacheRoute(c *gin.Context, body, intentSourceBody []byte, provider *gatewayprovider.ExecutionProvider, cacheIdentity string) ([]byte, error) {
	allowPureClientTools, providerPolicyExplicit := gatewayprovider.GrokClientToolCacheProviderPolicy(provider)
	requestOptOut := false
	if c != nil {
		// 新请求头的关闭值优先，缺失新头时读取兼容请求头。
		value := c.GetHeader(grokClientToolCacheOptInHeader)
		if _, present := c.Request.Header[http.CanonicalHeaderKey(grokClientToolCacheOptInHeader)]; !present {
			value = c.GetHeader(legacyGrokClientToolCacheOptInHeader)
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on", "prefer-cache":
			allowPureClientTools = true
		case "0", "false", "no", "off":
			allowPureClientTools = false
			requestOptOut = true
		}
	}
	if !allowPureClientTools && !providerPolicyExplicit && !requestOptOut && isGrokClaudeDesktopResponsesCacheRequest(c) {
		allowPureClientTools = true
	}
	// 名为 web_search/x_search 的函数仍是客户端函数。已知 Free OAuth 提供商默认使用
	// 缓存路由；请求级启用可覆盖提供商关闭，而请求级关闭始终优先。旧 Claude 指纹仅在
	// 尚无提供商策略时作为兼容回退（#4486）。
	return gatewayprovider.ApplyGrokFreeToolCacheRoute(body, intentSourceBody, provider, cacheIdentity, allowPureClientTools, allowPureClientTools)
}

// isGrokClaudeDesktopResponsesCacheRequest 识别 Claude Desktop 本地代理经 CC Switch 转换的 Responses 请求。
// 全部指纹信号匹配时启用平台和客户端混合工具路由。
func isGrokClaudeDesktopResponsesCacheRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil || IsOpenAIResponsesCompactPath(c) {
		return false
	}
	path := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	if !strings.HasSuffix(path, "/responses") {
		return false
	}

	if !clientmeta.NewClaudeCodeValidator().ValidateUserAgent(strings.TrimSpace(c.GetHeader("User-Agent"))) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(c.GetHeader("X-App"))) {
	case "cli", "cli-bg":
	default:
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(c.GetHeader("anthropic-client-platform")), "desktop_app") {
		return false
	}
	return strings.TrimSpace(c.GetHeader("X-Claude-Code-Session-Id")) != ""
}

// grokCacheInput 请求读取留在适配器；原种子 helper 只在平台实际选择该分支时调用。
func grokCacheInput(c *gin.Context, explicitKey, model string) grok.CacheIdentityInput {
	input := grok.CacheIdentityInput{APIKeyID: APIKeyIDFromContext(c), Compact: IsOpenAIResponsesCompactPath(c), Model: model, ExplicitKey: explicitKey, StablePrefixSeed: gatewaysession.OpenAIStablePrefixSeed, AnchoredSeed: gatewaysession.OpenAIAnchoredContentSeed, PreviousResponseSeed: gatewaysession.GrokPreviousResponseSeed}
	if c != nil {
		input.ClaudeSession = c.GetHeader(ClaudeCodeSessionHeader)
		input.HeaderSession = ExplicitOpenAIHeaderSessionID(c)
		input.ConversationID = c.GetHeader(GrokConversationIDHeader)
	}
	return input
}
