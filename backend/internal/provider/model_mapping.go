package provider

import (
	"maps"
	"strings"
)

// ModelMappingDefaults 提供专用路由和独立型号配额的读取函数。
type ModelMappingDefaults struct {
	// Models 枚举专用路由别名，Spark 返回独立配额对应的型号。
	Models      func(*Record) []string
	Antigravity func() map[string]string
}

// ResolveCompactForwardModel 应用 Compact 附加映射，空值时使用回退模型。
func ResolveCompactForwardModel(value *Record, model string) string {
	model = strings.TrimSpace(model)
	if model == "" || value == nil {
		return model
	}
	mapped, matched := value.ResolveCompactMappedModel(model)
	if matched && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	return model
}

// ResolveForwardMappedModel 保留提供商映射的一跳与空映射回退，调用方仍决定平台规范化。
func ResolveForwardMappedModel(value *Record, requested string, defaults ModelMappingDefaults) string {
	if value == nil {
		return ""
	}
	mapped, matched := ResolveMappedModel(ResolveModelMapping(value, defaults), requested)
	if !matched || strings.TrimSpace(mapped) == "" {
		return requested
	}
	return strings.TrimSpace(mapped)
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

	if a.Platform == PlatformAntigravity {
		return maps.Clone(defaults.Antigravity())
	}
	return nil
}
