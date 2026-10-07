package provider

import (
	"maps"
)

// ModelMappingDefaults 按需读取平台默认模型目录。
type ModelMappingDefaults struct {
	// Models 按提供商平台和认证类型提供当前默认模型目录。
	Models      func(*Record) []string
	Antigravity func() map[string]string
}

// ResolveModelMapping 读取不可变配置并返回独立映射，不在共享提供商内写入派生缓存。
func ResolveModelMapping(a *Record, defaults ModelMappingDefaults) map[string]string {
	rawMapping, _ := a.Credentials["model_mapping"].(map[string]any)
	if a.Credentials == nil {
		// Antigravity 平台使用默认映射
		if a.Platform == PlatformAntigravity {
			return maps.Clone(defaults.Antigravity())
		}
		// Bedrock 默认映射由 forwardBedrock 统一处理（需配合 region prefix 调整）
		return nil
	}
	if len(rawMapping) == 0 {
		if a.IsGeminiGoogleOne() {
			return modelDirectoryMapping(a, defaults)
		}
		// Antigravity 平台使用默认映射
		if a.Platform == PlatformAntigravity {
			return maps.Clone(defaults.Antigravity())
		}
		return nil
	}

	result := make(map[string]string)
	for k, v := range rawMapping {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	if len(result) > 0 {
		return result
	}

	if a.IsGeminiGoogleOne() {
		return modelDirectoryMapping(a, defaults)
	}
	if a.Platform == PlatformAntigravity {
		return maps.Clone(defaults.Antigravity())
	}
	return nil
}

// modelDirectoryMapping 将默认目录转换成独立的恒等白名单。
func modelDirectoryMapping(a *Record, defaults ModelMappingDefaults) map[string]string {
	if defaults.Models == nil {
		return nil
	}
	models := defaults.Models(a)
	mapping := make(map[string]string, len(models))
	for _, model := range models {
		mapping[model] = model
	}
	return mapping
}
