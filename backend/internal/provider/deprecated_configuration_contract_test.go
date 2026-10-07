package provider_test

import (
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/stretchr/testify/require"
)

// TestLegacyOpenAIConfigurationInputBoundary 检查保存时删除旧探测值，其余配置保持不变。
func TestLegacyOpenAIConfigurationInputBoundary(t *testing.T) {
	for _, value := range []any{nil, false, true, "unsupported", []any{1}, map[string]any{"invalid": true}} {
		extra := map[string]any{}
		for _, key := range providercore.DeprecatedOpenAIProviderExtraKeys {
			extra[key] = value
		}
		normalized, replace := providercore.NormalizeDeprecatedProviderExtraUpdate(extra)
		require.False(t, replace)
		require.Nil(t, normalized)
		extra["keep"] = map[string]any{"enabled": false}
		extra["openai_compact_mode"] = " AUTO "
		extra[providercore.OpenAINativeCompactionV2ModeExtraKey] = "force_off"
		normalized, replace = providercore.NormalizeDeprecatedProviderExtraUpdate(extra)
		require.True(t, replace)
		require.Equal(t, map[string]any{"keep": map[string]any{"enabled": false}, "openai_compact_mode": "force_on", providercore.OpenAINativeCompactionV2ModeExtraKey: "force_off"}, normalized)
		require.Equal(t, " AUTO ", extra["openai_compact_mode"], "边界处理不得修改调用方对象")
	}
	normalized, replace := providercore.NormalizeDeprecatedProviderExtraUpdate(map[string]any{})
	require.True(t, replace, "显式空对象仍表示清空")
	require.Empty(t, normalized)
}
