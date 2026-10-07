package provider

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
