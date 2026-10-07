package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDeepseekDefaultCatalogUsesNativeEntries 验证默认目录原厂报价；中继的历史型号仍可独立存在。
func TestDeepseekDefaultCatalogUsesNativeEntries(t *testing.T) {
	pricingService := newOfflinePricingFixture(t)
	pricingData := pricingService.Snapshot().Data

	for _, tc := range []struct {
		model                 string
		input, output, cached float64
	}{
		{"deepseek-v4-flash", 0.15e-6, 0.6e-6, 0.003e-6},
		{"deepseek-v4-flash-vision-exp", 0.15e-6, 0.6e-6, 0.003e-6},
		{"deepseek-v4-pro", 0.435e-6, 0.87e-6, 0.003625e-6},
	} {
		entry, exists := pricingData["deepseek/"+tc.model]
		require.True(t, exists, "%s 必须存在于原厂价格目录", tc.model)
		require.NotNil(t, entry)
		require.Equal(t, "models.dev", entry.Source)
		require.Equal(t, "deepseek", entry.Provider)
		require.InDelta(t, tc.input, entry.InputCostPerToken, 1e-15)
		require.InDelta(t, tc.output, entry.OutputCostPerToken, 1e-15)
		require.InDelta(t, tc.cached, entry.CacheReadInputTokenCost, 1e-15)
	}
}
