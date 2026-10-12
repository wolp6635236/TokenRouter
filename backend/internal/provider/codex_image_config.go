package provider

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	CodexImageGenerationBridgeKey = "codex_image_generation_bridge"

	CodexImageGenerationExplicitToolPolicyKey = "codex_image_generation_explicit_tool_policy"

	CodexImagePolicyAllow = "allow"
	CodexImagePolicyStrip = "strip"
)

func boolOverrideFromMap(values map[string]any, keys ...string) *bool {
	if values == nil {
		return nil
	}
	for _, key := range keys {
		if v, ok := values[key].(bool); ok {
			copy := v
			return &copy
		}
	}
	return nil
}

// stringOverrideFromMap 按候选键顺序读取首个字符串覆盖值。
func stringOverrideFromMap(values map[string]any, keys ...string) (string, bool) {
	if values == nil {
		return "", false
	}
	for _, key := range keys {
		if v, ok := values[key].(string); ok {
			return v, true
		}
	}
	return "", false
}

// normalizeCodexImageGenerationExplicitToolPolicy 兼容旧的 remove/drop 写法，未知值按放行处理。
func normalizeCodexImageGenerationExplicitToolPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CodexImagePolicyStrip, "remove", "drop":
		return CodexImagePolicyStrip
	default:
		return CodexImagePolicyAllow
	}
}

// CodexImageGenerationBridgeOverride 返回提供商级 Codex 图片桥接覆盖配置。
// nil 表示使用全局默认值，分组配置的协议设置优先。
func (r *Record) CodexImageGenerationBridgeOverride() *bool {
	if r == nil || r.Platform != capability.PlatformOpenAI || r.Extra == nil {
		return nil
	}
	if override := boolOverrideFromMap(r.Extra, CodexImageGenerationBridgeKey, "codex_image_generation_bridge_enabled"); override != nil {
		return override
	}
	openaiConfig, _ := r.Extra[capability.PlatformOpenAI].(map[string]any)
	return boolOverrideFromMap(openaiConfig, CodexImageGenerationBridgeKey, "codex_image_generation_bridge_enabled")
}

// CodexImageGenerationExplicitToolPolicy 返回提供商级 Codex /responses 图片工具策略。
// 未设置或未知值默认放行，以保持已有行为。
func (r *Record) CodexImageGenerationExplicitToolPolicy() string {
	if r == nil || r.Platform != capability.PlatformOpenAI || r.Extra == nil {
		return CodexImagePolicyAllow
	}
	if policy, ok := stringOverrideFromMap(r.Extra, CodexImageGenerationExplicitToolPolicyKey); ok {
		return normalizeCodexImageGenerationExplicitToolPolicy(policy)
	}
	openaiConfig, _ := r.Extra[capability.PlatformOpenAI].(map[string]any)
	if policy, ok := stringOverrideFromMap(openaiConfig, CodexImageGenerationExplicitToolPolicyKey); ok {
		return normalizeCodexImageGenerationExplicitToolPolicy(policy)
	}
	return CodexImagePolicyAllow
}
