package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
)

// ModelDefaults 提供平台默认模型目录的按需读取函数。
func ModelDefaults() provider.ModelMappingDefaults {
	return provider.ModelMappingDefaults{
		Models:      DefaultProviderModels,
		Antigravity: func() map[string]string { return antigravity.DefaultAntigravityModelMapping },
	}
}

// ModelRules 在检查平台资格时读取站点信息。
func ModelRules(value *provider.Record) provider.ModelPlatformRules {
	return provider.ModelPlatformRules{
		NormalizeQoder:      qoder.NormalizeModelForWhitelist,
		OpenAIOAuthServable: provider.IsOpenAIOAuthServableModel,
		QoderCompatible: func(model string) bool {
			if value == nil {
				return false
			}
			site, err := qoder.ParseSite(value.GetCredential("site"))
			return err == nil && qoder.ModelCompatibleWithSite(site, model)
		},
	}
}

// SupportsOpenAIEndpoint 在端点能力检查实际需要时提供平台媒体资格，不提前读取资格。
func SupportsOpenAIEndpoint(value *provider.Record, capability provider.OpenAIEndpointCapability) bool {
	return value.SupportsOpenAIEndpointCapability(capability, func() (bool, string) {
		return provider.GrokMediaGenerationEligibility(value, GrokTierRules())
	})
}
