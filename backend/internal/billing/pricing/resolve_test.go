package pricing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRequestPriceSeparatesLabelsAndContext 检查标签和上下文分别匹配对应的按次价。
func TestRequestPriceSeparatesLabelsAndContext(t *testing.T) {
	zero, low, high := 0.0, 0.03, 0.1
	maxTokens := 128000
	resolved := &ResolvedPricing{Mode: BillingModePerRequest, RequestTiers: []PricingInterval{
		{TierLabel: "1K", PerRequestPrice: &zero},
		{MinTokens: 0, MaxTokens: &maxTokens, PerRequestPrice: &low},
		{MinTokens: maxTokens, PerRequestPrice: &high},
	}}
	_, ok := GetRequestTierPriceValue(resolved, "")
	require.False(t, ok)
	count := 200000
	value, ok := ResolveRequestUnitPrice(resolved, "2K", &count)
	require.True(t, ok)
	require.Equal(t, high, value)
	value, ok = ResolveRequestUnitPrice(resolved, "1K", nil)
	require.True(t, ok)
	require.Zero(t, value)
	_, ok = ResolveRequestUnitPrice(resolved, "2K", nil)
	require.False(t, ok)
}

// TestRequestMissingDefaultIsNotFree 检查零价、零倍率和缺少默认按次价的区别。
func TestRequestMissingDefaultIsNotFree(t *testing.T) {
	count := 0
	_, ok := ResolveRequestUnitPrice(&ResolvedPricing{Mode: BillingModePerRequest}, "", &count)
	require.False(t, ok)
	price, ok := ResolveRequestUnitPrice(&ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPricePresent: true}, "", &count)
	require.True(t, ok)
	require.Zero(t, price)
	unit, multiplier := 0.2, 0.0
	resolved := ResolvePriceCards(&ModelPricingEntry{BillingMode: BillingModePerRequest, PerRequestPrice: &unit, PriceMultiplier: &multiplier}, nil, PricingSourceUnpriced, true)
	price, ok = ResolveRequestUnitPrice(resolved, "", &count)
	require.True(t, ok)
	require.Zero(t, price)
}

// TestConstantRequestIntervalsKeepExplicitFreeQuote 检查区间价格相同时可返回固定报价，免费价卡返回零价。
func TestConstantRequestIntervalsKeepExplicitFreeQuote(t *testing.T) {
	base, tier, multiplier := 0.2, 0.4, 0.0
	resolved := ResolvePriceCards(&ModelPricingEntry{BillingMode: BillingModePerRequest, PerRequestPrice: &base, PriceMultiplier: &multiplier, Intervals: []PricingInterval{{MinTokens: 0, PerRequestPrice: &tier}}}, nil, PricingSourceUnpriced, true)
	price, ok := ResolveRequestUnitPrice(resolved, "2K", nil)
	require.True(t, ok)
	require.Zero(t, price)
}
