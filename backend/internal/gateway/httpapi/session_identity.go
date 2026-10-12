package httpapi

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/gateway/clientmeta"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	gatewaysession "github.com/TokenFlux/TokenRouter/internal/gateway/session"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

const (
	OpenCodeSessionAffinityHeader = "X-Session-Affinity"
	OpenCodeSessionIDHeader       = "X-Session-Id"
	OpenCodeNativeSessionHeader   = "X-OpenCode-Session"
	CodeBuddyConversationHeader   = "X-Conversation-ID"

	GrokConversationIDHeader = "X-Grok-Conv-Id"
	ClaudeCodeSessionHeader  = "X-Claude-Code-Session-Id"
)

var (
	explicitOpenAIHeaderSessionNames = []string{
		"session-id",
		"session_id",
		"conversation_id",
		OpenCodeSessionAffinityHeader,
		OpenCodeSessionIDHeader,
		OpenCodeNativeSessionHeader,
		CodeBuddyConversationHeader,
	}

	clientSessionIDHeaders = append(append([]string(nil), explicitOpenAIHeaderSessionNames...), ClaudeCodeSessionHeader)
)

// WithOpenAIGuardianParentAffinity 从 HTTP 头和报文提取父会话，再通过 scheduler 生成亲和性散列。
func WithOpenAIGuardianParentAffinity(ctx context.Context, c *gin.Context, body []byte, model string) context.Context {
	if ctx == nil || c == nil || !clientmeta.IsCodexReviewModel(model) {
		return ctx
	}
	parent := clientmeta.CodexReviewParent(clientmeta.CodexReviewInput{Model: model, Body: body, Subagent: c.GetHeader(clientmeta.OpenAISubagentHeader), ParentThreadID: c.GetHeader(clientmeta.CodexParentThreadIDHeader), TurnMetadata: c.GetHeader(clientmeta.CodexTurnMetadataHeader)})
	if parent == "" {
		return ctx
	}
	current, legacy := scheduler.DeriveSessionHashes(parent)
	if current == "" {
		return ctx
	}
	return requeststate.WithGuardianParentAffinity(ctx, requeststate.GuardianParentAffinity{CurrentSessionHash: current, LegacySessionHash: legacy})
}

// ExplicitOpenAIHeaderSessionID 提取 OpenAI 兼容客户端发送的稳定会话标识。
// 这里只接收会话级字段；每轮变化的请求或消息 ID 会破坏粘性路由和上游提示缓存。
func ExplicitOpenAIHeaderSessionID(c *gin.Context) string {
	if c == nil {
		return ""
	}

	for _, header := range explicitOpenAIHeaderSessionNames {
		if sessionID := strings.TrimSpace(c.GetHeader(header)); sessionID != "" {
			return sessionID
		}
	}
	return ""
}

func ExplicitOpenAISessionID(c *gin.Context, body []byte) string {
	if c == nil {
		return ""
	}

	sessionID := ExplicitOpenAIHeaderSessionID(c)
	if sessionID == "" && len(body) > 0 {
		// WS response.create 将会话字段包在 response 内，先解包再读取统一信号。
		sessionID = strings.TrimSpace(protocolopenai.RequestPayloadView(body).Get("prompt_cache_key").String())
	}
	return sessionID
}

// ExplicitOpenAIRequestSessionID 从客户端会话字段读取身份，选定提供商后应用平台约束。
func ExplicitOpenAIRequestSessionID(c *gin.Context, body []byte) string {
	if c == nil {
		return ""
	}

	sessionID := ExplicitOpenAIHeaderSessionID(c)
	if sessionID == "" && IsGrokRequestContext(c) {
		sessionID = strings.TrimSpace(c.GetHeader(GrokConversationIDHeader))
	}
	if sessionID == "" && len(body) > 0 {
		sessionID = strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	}
	if sessionID == "" && IsGrokRequestContext(c) && len(body) > 0 {
		sessionID = gatewaysession.GrokPreviousResponseSeed(body)
	}
	return sessionID
}

// GenerateExplicitOpenAISessionHash 根据客户端会话字段生成 hash，字段缺失时返回空值。
func GenerateExplicitOpenAISessionHash(c *gin.Context, body []byte) string {
	sessionID := ExplicitOpenAIRequestSessionID(c, body)
	if sessionID == "" {
		return ""
	}

	currentHash, legacyHash := scheduler.DeriveSessionHashes(sessionID)
	AttachOpenAILegacySessionHash(c, legacyHash)
	return currentHash
}

// GenerateOpenAISessionHash 为 OpenAI 请求生成粘性会话哈希。
// 优先级依次为：session-id/session_id、conversation_id、OpenCode 会话头、
// CodeBuddy 会话头、Grok 原生会话头、prompt_cache_key，最后才使用内容回退。
func GenerateOpenAISessionHash(c *gin.Context, body []byte) string {
	if c == nil {
		return ""
	}

	sessionID := ExplicitOpenAIRequestSessionID(c, body)
	if sessionID == "" && len(body) > 0 {
		sessionID = gatewaysession.OpenAIContentSeed(body)
	}
	if sessionID == "" {
		return ""
	}

	if IsGrokRequestContext(c) {
		sessionID = gatewaysession.GrokStickyAffinitySeed(sessionID, body)
	}

	currentHash, legacyHash := scheduler.DeriveSessionHashes(sessionID)
	AttachOpenAILegacySessionHash(c, legacyHash)
	return currentHash
}

// GenerateOpenAISessionHashWithFallback 先按常规信号生成会话哈希；
// 当未携带 session_id/conversation_id/prompt_cache_key 时，使用 fallbackSeed 生成稳定哈希。
// 该方法为 WS ingress 补充会话标识，使后续请求继续使用绑定的提供商。
func GenerateOpenAISessionHashWithFallback(c *gin.Context, body []byte, fallbackSeed string) string {
	sessionHash := GenerateOpenAISessionHash(c, body)
	if sessionHash != "" {
		return sessionHash
	}

	seed := strings.TrimSpace(fallbackSeed)
	if seed == "" {
		return ""
	}

	currentHash, legacyHash := scheduler.DeriveSessionHashes(seed)
	AttachOpenAILegacySessionHash(c, legacyHash)
	return currentHash
}

// ClaudeCodeSessionIDFromHeader 解析 Claude Code 会话头，用于 Messages 粘性路由。
// ExtractClientSessionID 用于用量日志。
func ClaudeCodeSessionIDFromHeader(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return gatewaysession.SanitizeClientSessionID(c.GetHeader(ClaudeCodeSessionHeader))
}

func ExtractClientSessionID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return gatewaysession.ExtractClientSessionID(c.GetHeader, clientSessionIDHeaders, IsGrokRequestContext(c))
}

// IsGrokRequestContext 根据已选提供商、强制路由或 Grok 会话头判断请求平台。
func IsGrokRequestContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if platform := selectedOpsPlatform(c); platform != "" && platform != "unknown" {
		return platform == capability.PlatformGrok
	}
	if c.Request != nil {
		if platform, ok := apikey.ForcePlatformFromContext(c.Request.Context()); ok && strings.TrimSpace(platform) != "" {
			return platform == capability.PlatformGrok
		}
	}
	return strings.TrimSpace(c.GetHeader(GrokConversationIDHeader)) != ""
}

func AttachOpenAILegacySessionHash(c *gin.Context, legacyHash string) {
	if c == nil || c.Request == nil {
		return
	}
	c.Request = c.Request.WithContext(requeststate.WithOpenAILegacySessionHash(c.Request.Context(), legacyHash))
}
