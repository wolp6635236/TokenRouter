package billing

import (
	"encoding/json"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/stretchr/testify/require"
)

type defaultCatalogStub struct {
	entries map[string]*pricing.CatalogModelPricing
}

func (s defaultCatalogStub) GetModelPricing(model string) *pricing.CatalogModelPricing {
	return s.entries[model]
}

func (s defaultCatalogStub) ForceUpdate() error {
	panic("default price queries must not update the catalog")
}

func TestDefaultPriceUsesCatalogAndPreservesZero(t *testing.T) {
	catalog := defaultCatalogStub{entries: map[string]*pricing.CatalogModelPricing{
		"custom": {CatalogRules: pricing.CatalogRules{FlexMultiplier: newFloatForCatalogTest(0.5)}, InputCostPerToken: 0, OutputCostPerToken: 0.000004, SupportsServiceTier: true, InputCostPerTokenPriority: 0, OutputCostPerTokenPriority: 0.000008, LongContextInputTokenThreshold: 100000, LongContextInputCostMultiplier: 2, LongContextOutputCostMultiplier: 1.5},
	}}
	calculator := NewCalculator(catalog, CalculatorOptions{})
	row := calculator.DefaultModelPrice("custom", "openai", "token")
	require.Equal(t, "priced", row.PriceStatus)
	require.Equal(t, 100000, row.LongContextThreshold)
	values := make(map[string]float64)
	for _, price := range row.Prices {
		if price.Value != nil {
			values[price.Key] = *price.Value
		}
	}
	require.Contains(t, values, "input")
	require.Zero(t, values["input"])
	require.Equal(t, 4.0, values["output"])
	require.Equal(t, 8.0, values["fast_output"])
	require.Equal(t, 2.0, values["flex_output"])
	// 长上下文返回应用倍率后的单价：output 4x1.5，fast 8x1.5，flex 2x1.5。
	require.Zero(t, values["long_input"])
	require.Equal(t, 6.0, values["long_output"])
	require.Equal(t, 12.0, values["long_fast_output"])
	require.Equal(t, 3.0, values["long_flex_output"])
	require.NotContains(t, values, "long_context_input")
	require.NotContains(t, values, "long_context_output")
	require.Equal(t, "unpriced", calculator.DefaultModelPrice("unknown-model", "openai", "token").PriceStatus)
	require.Equal(t, "unpriced", calculator.DefaultModelPrice("claude-sonnet-4", "anthropic", "token").PriceStatus)
}

func TestDefaultPriceContextIntervalsUseInclusiveBoundary(t *testing.T) {
	catalog := defaultCatalogStub{entries: map[string]*pricing.CatalogModelPricing{
		"grok-test": {
			Source:             "models.dev",
			Provider:           "xai",
			InputCostPerToken:  2e-6,
			OutputCostPerToken: 6e-6,
			ContextPrices: []pricing.CatalogContextPrice{
				{Threshold: 200000, Pricing: &pricing.CatalogModelPricing{InputCostPerToken: 4e-6, OutputCostPerToken: 12e-6}},
			},
		},
	}}
	calculator := NewCalculator(catalog, CalculatorOptions{})
	row := calculator.DefaultModelPrice("grok-test", "xai", "token")
	require.Len(t, row.ContextIntervals, 2)
	require.Equal(t, 199999, *row.ContextIntervals[0].MaxTokens)
	require.Equal(t, 199999, row.ContextIntervals[1].MinTokens)
	// 转换不修改目录原有阈值，后续查询不能再次减一。
	require.Equal(t, 200000, catalog.entries["grok-test"].ContextPrices[0].Threshold)
	require.Equal(t, row, calculator.DefaultModelPrice("grok-test", "xai", "token"))
}

func TestDefaultMediaPriceRequiresKnownUnits(t *testing.T) {
	entries, _, err := pricing.ParsePricingEntries(map[string]json.RawMessage{
		"free-image":   json.RawMessage(`{"source":"local_override","mode":"image_generation","output_cost_per_image":0}`),
		"custom-image": json.RawMessage(`{"source":"models.dev","mode":"chat","output_cost_per_image":0.1,"price_sources":{"image":"local_override"}}`),
		"vision-chat":  json.RawMessage(`{"source":"models.dev","mode":"chat","input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"output_cost_per_image":0.1}`),
	})
	require.NoError(t, err)
	calculator := NewCalculator(defaultCatalogStub{entries: entries}, CalculatorOptions{})
	for _, model := range []string{"free-image", "custom-image"} {
		row := calculator.DefaultModelPrice(model, "other", entries[model].Mode)
		require.Equal(t, "image", row.BillingMode)
		require.Equal(t, "priced", row.PriceStatus)
		require.Len(t, row.Prices, 3)
		for _, price := range row.Prices {
			require.NotNil(t, price.Value)
			require.Equal(t, "USD/image", price.Unit)
			require.Equal(t, "local_override", row.PriceSources[price.Key])
			unit, err := calculator.DefaultImagePrice(model, price.Key)
			require.NoError(t, err)
			require.Equal(t, unit, *price.Value)
			if model == "free-image" {
				require.Zero(t, *price.Value)
			}
		}
	}
	for _, mode := range []string{"video", "image"} {
		row := calculator.DefaultModelPrice("unknown-media", "other", mode)
		require.Equal(t, mode, row.BillingMode)
		require.Equal(t, "unpriced", row.PriceStatus)
		require.Empty(t, row.Prices)
	}
	row := calculator.DefaultModelPrice("vision-chat", "other", "chat")
	require.Equal(t, "token", row.BillingMode)
	for _, price := range row.Prices {
		require.NotEqual(t, "USD/image", price.Unit)
	}
}

func newFloatForCatalogTest(value float64) *float64 { return &value }
