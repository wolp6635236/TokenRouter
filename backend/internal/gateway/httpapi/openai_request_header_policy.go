package httpapi

import "github.com/TokenFlux/TokenRouter/internal/gateway/media"

// 普通 Responses、passthrough 与 Raw Chat 保留各自请求头白名单。
const (
	ChatgptCodexURL      = "https://chatgpt.com/backend-api/codex/responses"
	OpenaiPlatformAPIURL = "https://api.openai.com/v1/responses"
)

var (
	// OpenAI allowed headers whitelist (for non-passthrough).
	openaiAllowedHeaders = map[string]bool{
		"accept-language": true,
		"content-type":    true,
		"conversation_id": true,
		"user-agent":      true,
		"originator":      true,
		"session_id":      true,
		// 请求构造器保留 Codex 设备和会话标识，用于提供商 namespace 隔离。
		"installation_id":            true,
		"x-codex-installation-id":    true,
		"session-id":                 true,
		"thread_id":                  true,
		"thread-id":                  true,
		"turn_id":                    true,
		"turn-id":                    true,
		"window_id":                  true,
		"window-id":                  true,
		"x-codex-window-id":          true,
		"x-client-request-id":        true,
		"x-codex-beta-features":      true,
		"x-codex-turn-state":         true,
		"x-codex-turn-metadata":      true,
		media.ResponsesLiteHeaderKey: true,
	}

	// OpenAI passthrough allowed headers whitelist.
	// 透传模式放行以下低风险请求头，其他请求头可能触发上游风控。
	openaiPassthroughAllowedHeaders = map[string]bool{
		"accept":                     true,
		"accept-language":            true,
		"content-type":               true,
		"conversation_id":            true,
		"openai-beta":                true,
		"user-agent":                 true,
		"originator":                 true,
		"session_id":                 true,
		"installation_id":            true,
		"x-codex-installation-id":    true,
		"session-id":                 true,
		"thread_id":                  true,
		"thread-id":                  true,
		"turn_id":                    true,
		"turn-id":                    true,
		"window_id":                  true,
		"window-id":                  true,
		"x-codex-window-id":          true,
		"x-client-request-id":        true,
		"x-codex-beta-features":      true,
		"x-codex-turn-state":         true,
		"x-codex-turn-metadata":      true,
		media.ResponsesLiteHeaderKey: true,
	}

	// openaiCCRawAllowedHeaders 是 Chat Completions 直转的客户端请求头白名单。
	// Codex 专用头 originator、session_id、x-codex-turn-state、x-codex-turn-metadata、conversation_id 用于 ChatGPT OAuth。
	// 将这些头发给 DeepSeek、Kimi、GLM 等兼容上游，可能被忽略或返回 400 unknown parameter。
	// 此白名单透传通用 HTTP 头，content-type、authorization 和 accept 由请求上下文设置。
	// 参见 pensieve/short-term/maxims/dont-reuse-shared-headers-whitelist-across-different-upstream-trust-domains。
	openaiCCRawAllowedHeaders = map[string]bool{
		"accept-language": true,
		"user-agent":      true,
	}
)

// AllowOpenAIRawChatHeader 供媒体和原生 Chat 入口复用同一通用白名单。
func AllowOpenAIRawChatHeader(name string) bool { return openaiCCRawAllowedHeaders[name] }

// AllowOpenAIPassthroughHeader 保留图片入口不携带客户端超时头的范围。
func AllowOpenAIPassthroughHeader(name string) bool { return openaiPassthroughAllowedHeaders[name] }
