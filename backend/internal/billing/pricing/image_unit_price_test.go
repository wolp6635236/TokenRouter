package pricing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestImageModeKeepsExplicitDefaultPrice 图片模式的固定默认价不受无标签区间字段影响。
func TestImageModeKeepsExplicitDefaultPrice(t *testing.T) {
	base, tier := 0.2, 0.1
	resolved := ResolvePriceCards(&ModelPricingEntry{BillingMode: BillingModeImage, PerRequestPrice: &base, Intervals: []PricingInterval{{MinTokens: 100, PerRequestPrice: &tier}}}, nil, PricingSourceUnpriced, true)
	price, found := ConfiguredImageUnitPrice(resolved, "2K")
	require.True(t, found)
	require.Equal(t, base, price)
}
