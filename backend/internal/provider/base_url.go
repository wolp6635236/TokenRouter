package provider

import (
	"strings"
)

// OpenAIBaseURL 解析 OpenAI 协议族提供商的上游 base_url。
// 适用 openai 与国产 OpenAI 兼容供应商（kimi/zhipu/deepseek）；grok 走 GetGrokBaseURL，
// Grok 的默认地址由调用方解析，此处返回空字符串。
func (r *Record) OpenAIBaseURL(adaptive bool) string {
	if !r.IsOpenAI() && !r.IsCNProvider() {
		return ""
	}
	if _, unified := r.Credentials[UpstreamProtocolsKey]; r.IsCNProvider() && (unified || adaptive) {
		if baseURLs, ok := r.Credentials["api_base_urls"].(map[string]any); ok {
			if baseURL, ok := baseURLs[APIProtocolChatCompletions].(string); ok && strings.TrimSpace(baseURL) != "" {
				return strings.TrimSpace(baseURL)
			}
		}
	}
	if r.Type == ProviderTypeAPIKey || r.Type == ProviderTypeUpstream {
		if baseURL := strings.TrimSpace(r.GetCredential("base_url")); baseURL != "" {
			return baseURL
		}
	}
	// 平台默认 base_url：CN 供应商按 provider_mode 选择 payg / coding 默认值。
	switch r.Platform {
	case PlatformKimi:
		if r.GetProviderMode() == ProviderModeCoding {
			return DefaultKimiCodingBaseURL
		}
		return DefaultKimiPayGBaseURL
	case PlatformZhipu:
		if r.GetProviderMode() == ProviderModeCoding {
			return DefaultZhipuCodingBaseURL
		}
		return DefaultZhipuPayGBaseURL
	case PlatformDeepseek:
		return DefaultDeepseekBaseURL
	default:
		return "https://api.openai.com"
	}
}
