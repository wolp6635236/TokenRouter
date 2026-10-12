package anthropic

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
)

const (
	// Claude Code 客户端相关常量。

	// Beta 值来自唯一 wire 定义，默认 Header 组合仍由平台拥有。
	BetaOAuth                    = anthropic.BetaOAuth
	BetaClaudeCode               = anthropic.BetaClaudeCode
	BetaInterleavedThinking      = anthropic.BetaInterleavedThinking
	BetaFineGrainedToolStreaming = anthropic.BetaFineGrainedToolStreaming
	BetaTokenCounting            = anthropic.BetaTokenCounting
	BetaContext1M                = anthropic.BetaContext1M
	BetaFastMode                 = anthropic.BetaFastMode
	BetaPromptCachingScope       = anthropic.BetaPromptCachingScope
	BetaEffort                   = anthropic.BetaEffort
	BetaRedactThinking           = anthropic.BetaRedactThinking
	BetaContextManagement        = anthropic.BetaContextManagement
	BetaExtendedCacheTTL         = anthropic.BetaExtendedCacheTTL
	BetaServerSideFallback       = anthropic.BetaServerSideFallback
	BetaFallbackCredit           = anthropic.BetaFallbackCredit
	BetaFallbackCreditLegacy     = anthropic.BetaFallbackCreditLegacy

	// DefaultBetaHeader Claude Code 客户端默认的 anthropic-beta header。
	DefaultBetaHeader = BetaClaudeCode + "," + BetaOAuth + "," + BetaInterleavedThinking + "," + BetaFineGrainedToolStreaming

	// CountTokensBetaHeader count_tokens 请求使用的 anthropic-beta header。
	CountTokensBetaHeader = BetaClaudeCode + "," + BetaOAuth + "," + BetaInterleavedThinking + "," + BetaTokenCounting

	// HaikuBetaHeader Haiku 模型在 OAuth 真实客户端透传路径上的默认 anthropic-beta header。
	// OAuth mimic 路径统一使用 FullClaudeCodeMimicryBetas。
	HaikuBetaHeader = BetaOAuth + "," + BetaInterleavedThinking

	// APIKeyBetaHeader API-key 提供商建议使用的 anthropic-beta header（不包含 oauth）。
	APIKeyBetaHeader = BetaClaudeCode + "," + BetaInterleavedThinking + "," + BetaFineGrainedToolStreaming

	// APIKeyHaikuBetaHeader Haiku 模型在 API-key 提供商下使用的 anthropic-beta header（不包含 oauth / claude-code）。
	APIKeyHaikuBetaHeader = BetaInterleavedThinking

	// DefaultCacheControlTTL 是网关代理为自己生成的 cache_control 块默认使用的 ttl。
	// 真实 Claude Code CLI 当前使用 "1h"，但本仓策略是"客户端透传 ttl 优先；
	// 客户端缺省时统一使用 5m"，这样既不浪费 1h 缓存额度，也保留客户端自定义能力。
	DefaultCacheControlTTL = "5m"

	// CLICurrentVersion 是内置的 Claude Code CLI 伪装版本号基线（三段 semver）。
	// 用于 billing attribution block 中的 cc_version=X.Y.Z.{fp} 前缀以及 fingerprint 计算。
	// 必须与 DefaultHeaders["User-Agent"] 中的版本号严格一致；不一致会被 Anthropic 判第三方。
	//
	// ⚠️ 读取实际生效的版本号请用 CLIVersion()，它会叠加 TOKENROUTER_CLAUDE_CLI_VERSION 覆盖。
	// 直接引用本常量只在"表达内置基线"时才正确（例如覆盖值的下限校验）。
	CLICurrentVersion = "2.1.220"

	// DefaultTestModel 测试时使用的默认模型。
	DefaultTestModel = "claude-sonnet-4-5-20250929"

	ClaudeAPIURL                    = "https://api.anthropic.com/v1/messages?beta=true"
	ClaudeAPICountTokensURL         = "https://api.anthropic.com/v1/messages/count_tokens?beta=true"
	ClaudeCodeSystemPrompt          = "You are Claude Code, Anthropic's official CLI for Claude."
	ClaudeCodeSystemPromptExpansion = `You are an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.
IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming. You may use URLs provided by the user in their messages or local files.

# Tone and style
 - Only use emojis if the user explicitly requests it. Avoid using emojis in all communication unless asked.
 - Your responses should be short and concise.
 - When referencing specific functions or pieces of code include the pattern file_path:line_number to allow the user to easily navigate to the source code location.
 - When referencing GitHub issues or pull requests, use the owner/repo#123 format (e.g. anthropics/claude-code#100) so they render as clickable links.
 - Do not use a colon before tool calls. Your tool calls may not be shown directly in the output, so text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`

	MaxCacheControlBlocks         = 4
	CacheTTLTarget1h              = "1h"
	ClaudeCodeBillingHeaderPrefix = anthropic.ClaudeCodeBillingHeaderPrefix

	ClaudeCodeEntrypointMarker = anthropic.ClaudeCodeEntrypointMarker
)

var (
	// DroppedBetas 是转发时需要从 anthropic-beta header 中移除的 beta token 列表。
	// 这些 token 是客户端特有的，不应透传给上游 API。
	DroppedBetas = []string{}

	// DefaultHeaders 是 Claude Code 客户端默认请求头。
	DefaultHeaders = map[string]string{
		// Keep these in sync with recent Claude CLI traffic to reduce the chance
		// that Claude Code-scoped OAuth credentials are rejected as "non-CLI" usage.
		// 版本参考：对齐 Parrot (src/transform/cc_mimicry.py:49) 的 CLI_USER_AGENT。
		"User-Agent":                                "claude-cli/" + CLIVersion() + " (external, cli)",
		"X-Stainless-Lang":                          "js",
		"X-Stainless-Package-Version":               "0.94.0",
		"X-Stainless-OS":                            "Linux",
		"X-Stainless-Arch":                          "arm64",
		"X-Stainless-Runtime":                       "node",
		"X-Stainless-Runtime-Version":               "v24.3.0",
		"X-Stainless-Retry-Count":                   "0",
		"X-Stainless-Timeout":                       "600",
		"X-App":                                     "cli",
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
	}

	ClaudeCodePromptPrefixes = []string{
		"You are Claude Code, Anthropic's official CLI for Claude",             // 标准版 & Agent SDK 版（含 running within...）
		"You are a Claude agent, built on Anthropic's Claude Agent SDK",        // Agent SDK 变体
		"You are a file search specialist for Claude Code",                     // Explore Agent 版
		"You are a helpful AI assistant tasked with summarizing conversations", // Compact 版
	}
)

// FullClaudeCodeMimicryBetas 返回最"像"真实 Claude Code CLI 的完整 beta 列表，
// 用于 OAuth 提供商伪装成 Claude Code 时使用。
// 顺序与真实 CLI 抓包一致。
//
// 使用建议：
//   - OAuth mimic：所有模型（包括 Haiku）都使用这整份列表。
//   - OAuth 真实客户端透传：保留客户端 beta；未提供时使用模型对应默认值。
//   - API-key 提供商：不要使用本函数，参见 APIKeyBetaHeader。
//   - 不默认加入 redact-thinking，避免上游抹除 thinking 内容；客户端显式传入时由合并逻辑保留。
func FullClaudeCodeMimicryBetas() []string {
	return []string{
		BetaClaudeCode,
		BetaOAuth,
		BetaInterleavedThinking,
		BetaPromptCachingScope,
		BetaEffort,
		BetaContextManagement,
		BetaExtendedCacheTTL,
	}
}

// IsAnthropicFableModel 判断是否为 Fable 模型家族（claude-fable-5、claude-fable-5[1m] 等变体）。
func IsAnthropicFableModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "fable")
}
