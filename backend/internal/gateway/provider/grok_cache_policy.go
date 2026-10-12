package provider

import (
	"strings"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

const grokClientToolCacheOptInExtraKey = "grok_client_tool_cache_enabled"

// ApplyGrokFreeMessagesFunctionToolCacheRoute 只为已知 Free 提供商启用 xAI 可缓存的
// 混合工具路由。纯客户端工具默认启用，运维人员可在原生搜索工具会改变预期行为时
// 按提供商明确关闭（#4486）。
func ApplyGrokFreeMessagesFunctionToolCacheRoute(body, intentSourceBody []byte, provider *ExecutionProvider, cacheIdentity string) ([]byte, error) {
	allowPureClientTools, _ := GrokClientToolCacheProviderPolicy(provider)
	return ApplyGrokFreeToolCacheRoute(body, intentSourceBody, provider, cacheIdentity, allowPureClientTools, true)
}

// GrokClientToolCacheProviderPolicy 严格要求配置值为 JSON 布尔值。缺少键时仅对已确认的
// Grok Free OAuth 提供商默认启用；付费、API Key 和未知提供商保持关闭。
func GrokClientToolCacheProviderPolicy(provider *ExecutionProvider) (enabled, explicit bool) {
	if !IsKnownGrokFreeProvider(provider) {
		return false, false
	}
	if provider.Record.Extra == nil {
		return true, false
	}
	value, exists := provider.Record.Extra[grokClientToolCacheOptInExtraKey]
	if !exists {
		return true, false
	}
	enabled, valid := value.(bool)
	if !valid {
		return false, true
	}
	return enabled, true
}

func ApplyGrokFreeToolCacheRoute(body, intentSourceBody []byte, provider *ExecutionProvider, cacheIdentity string, allowPureClientTools, allowFunctionSearch bool) ([]byte, error) {
	if strings.TrimSpace(cacheIdentity) == "" {
		return body, nil
	}
	return grok.ApplyGrokFreeToolCacheRoute(body, intentSourceBody, IsKnownGrokFreeProvider(provider), cacheIdentity, allowPureClientTools, allowFunctionSearch)
}

// IsKnownGrokFreeProvider 识别免费层 Grok 提供商，用于免费缓存路由与媒体 free_tier 阻断，
// 其覆盖范围比软性门禁更广；软性门禁使用 isExplicitGrokFreeOAuthProvider，且只匹配明确的 free。
func IsKnownGrokFreeProvider(provider *ExecutionProvider) bool {
	return providercore.KnownGrokFreeProvider(ExecutionRecord(provider), provideradapter.GrokTierRules())
}
