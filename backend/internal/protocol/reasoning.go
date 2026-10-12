package protocol

import (
	"strings"
)

// 推理档位是 wire 值，管理员映射和排序策略由 routing 拥有。
const (
	ReasoningNone    = "none"
	ReasoningMinimal = "minimal"
	ReasoningLow     = "low"
	ReasoningMedium  = "medium"
	ReasoningHigh    = "high"
	ReasoningXHigh   = "xhigh"
	ReasoningMax     = "max"
)

// NormalizeClaudeOutputEffort 规范化 Claude 的 output_config.effort 值。
// 空值和无法识别的值返回 nil。
func NormalizeClaudeOutputEffort(raw string) *string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return nil
	}
	switch value {
	case "low", "medium", "high", "xhigh", "max":
		return &value
	default:
		return nil
	}
}

// OpenAIReasoningEfforts 返回可比较档位的独立副本。
func OpenAIReasoningEfforts() []string {
	return []string{ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax}
}

// AnthropicReasoningEfforts 列出 Anthropic 支持的推理档位。
func AnthropicReasoningEfforts() []string {
	return []string{ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax}
}

// OpenAIReasoningMappingValues 包含关闭推理的 none，其余值按推理上限排序。
func OpenAIReasoningMappingValues() []string {
	return append([]string{ReasoningNone}, OpenAIReasoningEfforts()...)
}
