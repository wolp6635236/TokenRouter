package provider

import (
	"maps"
	"strings"
)

// 接收输入时删除废弃探测键，路由和调度使用协议配置。
var DeprecatedOpenAIProviderExtraKeys = [...]string{
	"openai_responses_probe_status", "openai_responses_supported",
	"openai_compact_supported", "openai_compact_checked_at",
	"openai_compact_last_status", "openai_compact_last_error",
	"openai_native_compaction_v2_supported", "openai_native_compaction_v2_checked_at",
	"openai_native_compaction_v2_last_status", "openai_native_compaction_v2_last_error",
}

// DiscardDeprecatedProviderExtra 静默移除旧客户端可能继续提交的废弃提供商扩展键。
func DiscardDeprecatedProviderExtra(extra map[string]any) {
	NormalizeLegacyOpenAIProviderExtra(extra)
	DiscardDeprecatedExtra(extra)
}

// NormalizeDeprecatedProviderExtraUpdate 规范化用于整份替换的提供商 Extra。
// 返回的布尔值表示是否执行替换：空对象清空 Extra，仅含废弃键时跳过更新。
func NormalizeDeprecatedProviderExtraUpdate(extra map[string]any) (map[string]any, bool) {
	if extra == nil {
		return nil, false
	}
	normalized := maps.Clone(extra)
	DiscardDeprecatedProviderExtra(normalized)
	if len(extra) > 0 && len(normalized) == 0 {
		return nil, false
	}
	return normalized, true
}

// DiscardDeprecatedExtra 清理写入数据中的废弃扩展键。
func DiscardDeprecatedExtra(extra map[string]any) {
	delete(extra, "upstream_billing_probe")
	delete(extra, "upstream_billing_probe_enabled")
	delete(extra, "openai_long_context_billing_enabled")
}

// NormalizeLegacyOpenAIProviderExtra 清理提供商历史探测字段并规范化压缩开关。
// 规范化请求携带的开关，省略项保持缺省，auto 和其他兼容值按开启处理。
// @project-doc docs/interfaces/openai_upstream.md#openai_account_configuration
func NormalizeLegacyOpenAIProviderExtra(extra map[string]any) {
	for _, key := range DeprecatedOpenAIProviderExtraKeys {
		delete(extra, key)
	}
	for _, key := range []string{"openai_compact_mode", OpenAINativeCompactionV2ModeExtraKey} {
		if raw, exists := extra[key]; exists {
			mode, _ := raw.(string)
			if strings.EqualFold(strings.TrimSpace(mode), OpenAICompactModeForceOff) {
				extra[key] = OpenAICompactModeForceOff
			} else {
				extra[key] = OpenAICompactModeForceOn
			}
		}
	}
}
