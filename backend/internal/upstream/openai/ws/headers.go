package ws

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

const WSTurnStateHeader = "x-codex-turn-state"

type WSHeaderOptions struct {
	openai.ResponsesRequestOptions
	AgentIdentity           bool
	Token                   string `json:"-"`
	TurnState, TurnMetadata string
	ResolveSession          func() (string, string)
	InboundHeaders          func() http.Header
	UserAgent               func() string
	ApplyWSUserAgent        func(http.Header)
}

func (WSHeaderOptions) String() string     { return "openai websocket header options" }
func (v WSHeaderOptions) GoString() string { return v.String() }

// BuildWSHeaders 根据 WSHeaderOptions 构造握手请求头。
func BuildWSHeaders(ctx context.Context, options WSHeaderOptions) (http.Header, error) {
	headers := make(http.Header)
	if !options.AgentIdentity {
		headers.Set("authorization", "Bearer "+options.Token)
	}

	sessionID, conversationID := options.ResolveSession()
	if options.InboundHeaders() != nil {
		if v := strings.TrimSpace(options.InboundHeaders().Get("accept-language")); v != "" {
			headers.Set("accept-language", v)
		}
		// Codex beta feature 会参与上游 WS 握手，也用于连接池兼容性隔离。
		for _, value := range options.InboundHeaders().Values("x-codex-beta-features") {
			if value = strings.TrimSpace(value); value != "" {
				headers.Add("x-codex-beta-features", value)
			}
		}
		// 按下方清单透传 Codex 的窗口与安装身份请求头。
		for _, name := range [...]string{
			"x-codex-window-id",
			"x-codex-installation-id",
			"session-id",
			"thread-id",
			"x-client-request-id",
		} {
			if value := strings.TrimSpace(options.InboundHeaders().Get(name)); value != "" {
				headers.Set(name, value)
			}
		}
	}
	// OAuth 提供商：将 apiKeyID 混入 session 标识符，防止跨用户会话碰撞。
	if options.UsesCodex() {
		apiKeyID := options.APIKeyID()
		if sessionID != "" {
			headers.Set("session_id", options.IsolateSession(apiKeyID, sessionID))
		}
		if conversationID != "" {
			headers.Set("conversation_id", options.IsolateSession(apiKeyID, conversationID))
		}
	} else {
		if sessionID != "" {
			headers.Set("session_id", sessionID)
		}
		if conversationID != "" {
			headers.Set("conversation_id", conversationID)
		}
	}
	if state := strings.TrimSpace(options.TurnState); state != "" {
		headers.Set(WSTurnStateHeader, state)
	}
	if metadata := strings.TrimSpace(options.TurnMetadata); metadata != "" {
		headers.Set(openai.WSTurnMetadataHeader, metadata)
	}
	options.ApplyProviderIdentity(headers)
	options.ApplyFingerprint(headers)

	if options.UsesCodex() {
		if err := options.ProviderHeaders(ctx, headers); err != nil {
			return nil, fmt.Errorf("resolve chatgpt provider headers: %w", err)
		}
		headers.Set("originator", options.Originator())
	}

	headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")

	if ua := strings.TrimSpace(options.UserAgent()); ua != "" {
		headers.Set("user-agent", ua)
	}
	options.ApplyWSUserAgent(headers)

	// originator 与最终 user-agent 首段使用配套的官方身份，非官方 UA 使用默认 Codex TUI 身份。
	// 两个字段错配会导致上游 404，详见 issue #3901。
	if options.UsesCodex() {
		openai.EnforceCodexIdentityHeadersWithUA(headers, "")
	}

	// 提供商级请求头覆写（仅 openai api_key 提供商启用时生效；OAuth 路径 no-op）。
	// 覆盖所有 WS 模式（ctx_pool/dedicated/passthrough）的握手头。
	options.OverrideHeaders(headers)
	// HTTP 与 WebSocket 共用同一份 Codex 会话级能力协商，连接池也会据此
	// 隔离不兼容握手。
	options.BetaFeatures(headers)
	options.RoutingHint(headers, nil)
	options.Diagnostics(headers, nil)

	return headers, nil
}
