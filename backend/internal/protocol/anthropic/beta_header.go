package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Beta header 常量
//
// 这些常量来自 2026-04 观测到的 Claude Code CLI 流量。
// 选型参考：与 Parrot (src/transform/cc_mimicry.py) 的 BETAS 保持一致，
// 原因：Anthropic 上游会基于 anthropic-beta 的完整集合判定请求来源；
// 缺少任何“官方 Claude Code 请求才会带”的 beta，都会被降级到第三方额度，
// 对应报错：`Third-party apps now draw from your extra usage, not your plan limits.`。
const (
	BetaOAuth                    = "oauth-2025-04-20"
	BetaClaudeCode               = "claude-code-20250219"
	BetaInterleavedThinking      = "interleaved-thinking-2025-05-14"
	BetaFineGrainedToolStreaming = "fine-grained-tool-streaming-2025-05-14"
	BetaTokenCounting            = "token-counting-2024-11-01"
	BetaContext1M                = "context-1m-2025-08-07"
	BetaFastMode                 = "fast-mode-2026-02-01"

	// CLI 2.1.9x 起的流量包含以下 beta。
	BetaPromptCachingScope = "prompt-caching-scope-2026-01-05"
	BetaEffort             = "effort-2025-11-24"
	BetaRedactThinking     = "redact-thinking-2026-02-12"
	BetaContextManagement  = "context-management-2025-06-27"
	BetaExtendedCacheTTL   = "extended-cache-ttl-2025-04-11"

	// server-side refusal fallback beta 字段族（beta Messages API 专有）。
	// 客户端（Claude Code / SDK / OpenCode 等）会默认透传 body.fallbacks /
	// body.fallback_credit_token，上游仅在 anthropic-beta 携带对应 token 时接受；
	// 缺 token 时 Pydantic 拒收："fallbacks: Extra inputs are not permitted"。
	// 仅用于 sanitize 的条件判断（strip-or-keep），禁止加入
	// FullClaudeCodeMimicryBetas / DefaultBetaHeader / APIKeyBetaHeader /
	// Bedrock 白名单：server-side fallback 会换模型、改计费，不能默认打开。
	BetaServerSideFallback   = "server-side-fallback-2026-07-01"
	BetaFallbackCredit       = "fallback-credit-2026-07-01"
	BetaFallbackCreditLegacy = "fallback-credit-2026-06-01"
)

// ParseAnthropicBetaHeader 解析 anthropic-beta 头的逗号分隔字符串为 token 列表。
func ParseAnthropicBetaHeader(header string) []string {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if strings.HasPrefix(header, "[") && strings.HasSuffix(header, "]") {
		var parsed []any
		if err := json.Unmarshal([]byte(header), &parsed); err == nil {
			tokens := make([]string, 0, len(parsed))
			for _, item := range parsed {
				token := strings.TrimSpace(fmt.Sprint(item))
				if token != "" {
					tokens = append(tokens, token)
				}
			}
			return tokens
		}
	}
	var tokens []string
	for part := range strings.SplitSeq(header, ",") {
		t := strings.TrimSpace(part)
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
}
