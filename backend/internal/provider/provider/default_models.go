package provider

import (
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/bedrock"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
)

// DefaultProviderModels 枚举执行路由中的别名，Spark 返回其独立配额对应的型号。
func DefaultProviderModels(value *provider.Record) []string {
	if value == nil {
		return nil
	}
	if value.IsShadow() && value.Platform == provider.PlatformOpenAI {
		return sparkModelVariants()
	}
	if value.IsBedrock() {
		var ids []string
		for alias, target := range bedrock.DefaultBedrockModelMapping {
			ids = append(ids, alias, target)
		}
		slices.Sort(ids)
		return slices.Compact(ids)
	}
	if value.Platform == provider.PlatformQoder {
		site, err := qoder.ParseSite(value.GetCredential("site"))
		if err != nil {
			return nil
		}
		return qoder.DefaultRequestModelIDsForSite(site)
	}
	return nil
}

// sparkModelVariants 返回 Spark 影子提供商支持的型号。
func sparkModelVariants() []string {
	return []string{"gpt-5.3-codex-spark"}
}

// DefaultSparkShadowModels 返回用于限制 Spark 型号的独立白名单映射。
func DefaultSparkShadowModels() map[string]any {
	variants := sparkModelVariants()
	mapping := make(map[string]any, len(variants))
	for _, m := range variants {
		mapping[m] = m
	}
	return mapping
}
