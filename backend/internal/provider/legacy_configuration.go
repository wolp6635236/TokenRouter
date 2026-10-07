package provider

import (
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
