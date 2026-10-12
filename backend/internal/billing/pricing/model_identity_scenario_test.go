package pricing

// 本文件检查 catalog_query.go、model_price.go 和 card.go 使用完整模型名查询价格与匹配价卡。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPricingUsesCompleteIdentity 检查目录查询、模型价格解析和价卡匹配使用完整型号。
func TestPricingUsesCompleteIdentity(t *testing.T) {
	price := &CatalogModelPricing{InputCostPerToken: 5e-6}
	query := &CatalogQuery{Entries: map[string]*CatalogModelPricing{
		"claude-opus-5-5": price, "gemini-3.8-flash": price, "gpt-5.6-sol": price,
	}}
	for _, model := range []string{"claude-opus-5.5", "claude-opus-5-5-20260101", "gemini-3.8-flash-high", "gpt-5.6-sol-max", "vendor/gpt-5.6-sol", "gpt5.6sol"} {
		require.Nil(t, query.GetModelPricing(model), model)
		_, err := ResolveModelPricing(model, nil)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, model)
	}
	require.Same(t, price, query.GetModelPricing("claude-opus-5-5"))
	require.Same(t, price, query.GetModelPricing("models/gemini-3.8-flash"))
	zero := 0.0
	card := ModelPricingEntry{Models: []string{"gemini-3.8-flash-*"}, InputPrice: &zero}
	require.NotNil(t, MatchPriceCard([]ModelPricingEntry{card}, "gemini-3.8-flash-high"))
	query.Entries["gemini-3.8-flash-high"] = &CatalogModelPricing{InputCostPerToken: 7e-6}
	require.Equal(t, 7e-6, query.GetModelPricing("gemini-3.8-flash-high").InputCostPerToken)
}
