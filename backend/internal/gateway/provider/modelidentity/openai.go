package modelidentity

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// IsGPT56 只识别已登记的 GPT-5.6 完整型号。
func IsGPT56(model string) bool {
	normalized := capability.CanonicalizeOpenAIModelAliasSpelling(model)
	for _, prefix := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		if normalized == prefix {
			return true
		}
	}
	return false
}

// UsageCandidates 只使用已选定的计费模型；仅缺少模型元数据时补取首个明确值。
func UsageCandidates(primary string, alternates ...string) []string {
	if strings.TrimSpace(primary) != "" {
		return []string{strings.TrimSpace(primary)}
	}
	for _, model := range alternates {
		if strings.TrimSpace(model) != "" {
			return []string{strings.TrimSpace(model)}
		}
	}
	return nil
}
