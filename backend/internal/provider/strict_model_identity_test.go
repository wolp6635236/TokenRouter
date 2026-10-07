package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExplicitAntigravityMappingIsNotAugmented 验证读取不补默认项，也不改写已保存的目标。
func TestExplicitAntigravityMappingIsNotAugmented(t *testing.T) {
	raw := map[string]any{"gemini-pro-agent": "custom", "gemini-3.1-pro-high": "gemini-3.1-pro-high"}
	value := &Record{Platform: PlatformAntigravity, Credentials: map[string]any{"model_mapping": raw}}
	defaults := ModelMappingDefaults{}
	got := ResolveModelMapping(value, defaults)
	require.Equal(t, map[string]string{"gemini-pro-agent": "custom", "gemini-3.1-pro-high": "gemini-3.1-pro-high"}, got)
	got["gemini-pro-agent"] = "changed"
	require.Equal(t, "custom", raw["gemini-pro-agent"])
	model, matched := ResolveMappedModel(nil, "gemini-3.1-pro-preview-customtools")
	require.False(t, matched)
	require.Equal(t, "gemini-3.1-pro-preview-customtools", model)
}
