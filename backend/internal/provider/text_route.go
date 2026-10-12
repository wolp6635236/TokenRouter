package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

const (
	// TextRouteModePreserveClientProtocol 优先保留客户端协议。
	TextRouteModePreserveClientProtocol TextRouteMode = "preserve_client_protocol"
	// TextRouteModeForceResponses 强制使用 Responses 协议。
	TextRouteModeForceResponses TextRouteMode = "force_responses"
	// TextRouteModeForceChatCompletions 强制使用 Chat Completions 协议。
	TextRouteModeForceChatCompletions TextRouteMode = "force_chat_completions"

	TextProtocolChatCompletions = openai.TextProtocolChatCompletions
	TextProtocolResponses       = openai.TextProtocolResponses

	// ExtraKeyTextRouteMode 是管理员控制的文本协议路由配置。
	ExtraKeyTextRouteMode = "openai_text_route_mode"
	// ExtraKeyResponsesContinuationSupported 是管理员控制的 HTTP continuation 能力开关。
	ExtraKeyResponsesContinuationSupported = "openai_responses_continuation_supported"
)

// TextRouteMode 描述普通文本请求的上游协议路由策略。
type TextRouteMode string

// TextProtocol 复用 protocol/openai 定义的文本协议标识。
type TextProtocol = openai.TextProtocol

// NormalizeTextRouteMode 将缺失或非法模式归一化为保留客户端协议。
func NormalizeTextRouteMode(mode string) TextRouteMode {
	switch TextRouteMode(mode) {
	case TextRouteModeForceResponses:
		return TextRouteModeForceResponses
	case TextRouteModeForceChatCompletions:
		return TextRouteModeForceChatCompletions
	default:
		return TextRouteModePreserveClientProtocol
	}
}

// ResolveTextRouteMode 从提供商 extra 中读取管理员配置的文本协议路由模式。
func ResolveTextRouteMode(extra map[string]any) TextRouteMode {
	if extra == nil {
		return TextRouteModePreserveClientProtocol
	}
	mode, _ := extra[ExtraKeyTextRouteMode].(string)
	return NormalizeTextRouteMode(mode)
}

// ResolveResponsesContinuationSupported 从提供商 extra 中读取 HTTP continuation 能力开关。
// 字段缺失或类型不符时按不支持处理。
func ResolveResponsesContinuationSupported(extra map[string]any) bool {
	if extra == nil {
		return false
	}
	supported, _ := extra[ExtraKeyResponsesContinuationSupported].(bool)
	return supported
}

// ResolveUpstreamTextProtocol 仅根据客户端首选协议和管理员路由模式，
// 返回普通文本请求实际应使用的上游协议；探测状态不参与路由决策。
func ResolveUpstreamTextProtocol(extra map[string]any, preferred TextProtocol) TextProtocol {
	switch ResolveTextRouteMode(extra) {
	case TextRouteModeForceResponses:
		return TextProtocolResponses
	case TextRouteModeForceChatCompletions:
		return TextProtocolChatCompletions
	}

	if preferred == TextProtocolChatCompletions {
		return TextProtocolChatCompletions
	}
	return TextProtocolResponses
}
