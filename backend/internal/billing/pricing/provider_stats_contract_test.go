package pricing_test

import (
	"testing"

	purepricing "github.com/TokenFlux/TokenRouter/internal/billing/pricing"

	"github.com/stretchr/testify/require"
)

func TestMatchProviderStatsRule_BothEmpty_NoMatch(t *testing.T) {
	rule := &purepricing.ProviderStatsPricingRule{}
	require.False(t, purepricing.MatchProviderStatsRule(rule, 1, 10))
}

func TestMatchProviderStatsRule_ProviderIDMatch(t *testing.T) {
	rule := &purepricing.ProviderStatsPricingRule{ProviderIDs: []int64{1, 2, 3}}
	require.True(t, purepricing.MatchProviderStatsRule(rule, 2, 999))
}

func TestMatchProviderStatsRule_GroupIDMatch(t *testing.T) {
	rule := &purepricing.ProviderStatsPricingRule{GroupIDs: []int64{10, 20}}
	require.True(t, purepricing.MatchProviderStatsRule(rule, 999, 20))
}

func TestMatchProviderStatsRule_BothConfigured_ProviderMatch(t *testing.T) {
	rule := &purepricing.ProviderStatsPricingRule{
		ProviderIDs: []int64{1, 2},
		GroupIDs:    []int64{10, 20},
	}
	require.True(t, purepricing.MatchProviderStatsRule(rule, 2, 999))
}

func TestMatchProviderStatsRule_BothConfigured_GroupMatch(t *testing.T) {
	rule := &purepricing.ProviderStatsPricingRule{
		ProviderIDs: []int64{1, 2},
		GroupIDs:    []int64{10, 20},
	}
	require.True(t, purepricing.MatchProviderStatsRule(rule, 999, 10))
}

func TestMatchProviderStatsRule_BothConfigured_NeitherMatch(t *testing.T) {
	rule := &purepricing.ProviderStatsPricingRule{
		ProviderIDs: []int64{1, 2},
		GroupIDs:    []int64{10, 20},
	}
	require.False(t, purepricing.MatchProviderStatsRule(rule, 999, 999))
}

func TestFindPricingForModel(t *testing.T) {
	exactPricing := purepricing.ModelPricingEntry{
		ID:     1,
		Models: []string{"claude-opus-4"},
	}
	wildcardPricing := purepricing.ModelPricingEntry{
		ID:     2,
		Models: []string{"claude-*"},
	}
	additionalPricing := purepricing.ModelPricingEntry{
		ID: 3,

		Models: []string{"gpt-4o"},
	}
	geminiPricing := purepricing.ModelPricingEntry{
		ID:     4,
		Models: []string{"gemini-2.5-pro"},
	}

	tests := []struct {
		name    string
		list    []purepricing.ModelPricingEntry
		model   string
		wantID  int64
		wantNil bool
	}{
		{
			name:   "exact match",
			list:   []purepricing.ModelPricingEntry{exactPricing},
			model:  "claude-opus-4",
			wantID: 1,
		},
		{
			name:   "exact match case insensitive",
			list:   []purepricing.ModelPricingEntry{{ID: 5, Models: []string{"Claude-Opus-4"}}},
			model:  "claude-opus-4",
			wantID: 5,
		},
		{
			name:   "wildcard match",
			list:   []purepricing.ModelPricingEntry{wildcardPricing},
			model:  "claude-opus-4",
			wantID: 2,
		},
		{
			name:   "exact match takes priority over wildcard",
			list:   []purepricing.ModelPricingEntry{wildcardPricing, exactPricing},
			model:  "claude-opus-4",
			wantID: 1,
		},
		{
			name:   "same card includes different model vendors",
			list:   []purepricing.ModelPricingEntry{exactPricing, additionalPricing, geminiPricing},
			model:  "gpt-4o",
			wantID: 3,
		},
		{
			name:   "Gemini model shares the same card",
			list:   []purepricing.ModelPricingEntry{exactPricing, additionalPricing, geminiPricing},
			model:  "gemini-2.5-pro",
			wantID: 4,
		},
		{
			name:    "no match at all",
			list:    []purepricing.ModelPricingEntry{exactPricing, wildcardPricing},
			model:   "gpt-4o",
			wantNil: true,
		},
		{
			name:    "empty list returns nil",
			list:    nil,
			model:   "claude-opus-4",
			wantNil: true,
		},
		{
			name: "wildcard matches by config order (first match wins)",
			list: []purepricing.ModelPricingEntry{
				{ID: 10, Models: []string{"claude-*"}},
				{ID: 11, Models: []string{"claude-opus-*"}},
			},
			model:  "claude-opus-4",
			wantID: 10, // config order: "claude-*" is first and matches, so it wins
		},
		{
			name: "shorter wildcard used when longer does not match",
			list: []purepricing.ModelPricingEntry{
				{ID: 10, Models: []string{"claude-*"}},
				{ID: 11, Models: []string{"claude-opus-*"}},
			},
			model:  "claude-sonnet-4",
			wantID: 10, // only "claude-*" matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := purepricing.FindPricingForModelByPredicate(tt.list, tt.model, nil)
			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.NotNil(t, result)
			require.Equal(t, tt.wantID, result.ID)
		})
	}
}

func TestCalculateStatsCost_NilPricing(t *testing.T) {
	result := purepricing.CalculateStatsCost(nil, purepricing.UsageTokens{}, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_TokenBilling(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModeToken,
		InputPrice:  contractFloat(0.001),
		OutputPrice: contractFloat(0.002),
	}
	tokens := purepricing.UsageTokens{
		InputTokens:  100,
		OutputTokens: 50,
	}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 100*0.001 + 50*0.002 = 0.1 + 0.1 = 0.2
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_WithCache(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode:     purepricing.BillingModeToken,
		InputPrice:      contractFloat(0.001),
		OutputPrice:     contractFloat(0.002),
		CacheWritePrice: contractFloat(0.003),
		CacheReadPrice:  contractFloat(0.0005),
	}
	tokens := purepricing.UsageTokens{
		InputTokens:         100,
		OutputTokens:        50,
		CacheCreationTokens: 200,
		CacheReadTokens:     300,
	}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 100*0.001 + 50*0.002 + 200*0.003 + 300*0.0005
	// = 0.1 + 0.1 + 0.6 + 0.15 = 0.95
	require.InDelta(t, 0.95, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_WithCacheTTLPricing(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode:       purepricing.BillingModeToken,
		CacheWritePrice:   contractFloat(0.003),
		CacheWrite1hPrice: contractFloat(0.006),
	}
	tokens := purepricing.UsageTokens{
		CacheCreationTokens:   100,
		CacheCreation5mTokens: 40,
		CacheCreation1hTokens: 60,
	}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	require.InDelta(t, 0.48, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_WithImageOutput(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode:      purepricing.BillingModeToken,
		InputPrice:       contractFloat(0.001),
		OutputPrice:      contractFloat(0.002),
		ImageOutputPrice: contractFloat(0.01),
	}
	tokens := purepricing.UsageTokens{
		InputTokens:       100,
		OutputTokens:      50,
		ImageOutputTokens: 10,
	}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 统计侧的历史口径保留 output 与 image output 两个独立桶。
	// 100*0.001 + 50*0.002 + 10*0.01 = 0.1 + 0.1 + 0.1 = 0.3
	require.InDelta(t, 0.3, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_PartialPricesNil(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModeToken,
		InputPrice:  contractFloat(0.001),
		// OutputPrice, CacheWritePrice, etc. are all nil → treated as 0
	}
	tokens := purepricing.UsageTokens{
		InputTokens:         100,
		OutputTokens:        50,
		CacheCreationTokens: 200,
	}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// Only input contributes: 100*0.001 = 0.1
	require.InDelta(t, 0.1, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_AllTokensZero(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModeToken,
		InputPrice:  contractFloat(0.001),
		OutputPrice: contractFloat(0.002),
	}
	tokens := purepricing.UsageTokens{} // all zeros
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	// totalCost == 0 → returns nil (does not override, falls back to default formula)
	require.Nil(t, result)
}

func TestCalculateStatsCost_TokenBilling_ExplicitZeroPriceOverrides(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModeToken,
		InputPrice:  contractFloat(0),
	}
	tokens := purepricing.UsageTokens{InputTokens: 100, OutputTokens: 50}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	require.Zero(t, *result)
}

func TestCalculateStatsCost_TokenBilling_BlankPricingDoesNotOverride(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModeToken,
	}
	tokens := purepricing.UsageTokens{InputTokens: 100}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_PerRequestBilling(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode:     purepricing.BillingModePerRequest,
		PerRequestPrice: contractFloat(0.05),
	}
	tokens := purepricing.UsageTokens{InputTokens: 999, OutputTokens: 999}
	result := purepricing.CalculateStatsCost(pricing, tokens, 3)
	require.NotNil(t, result)
	// 0.05 * 3 = 0.15
	require.InDelta(t, 0.15, *result, 1e-12)
}

func TestCalculateStatsCost_PerRequestBilling_PriceNil(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModePerRequest,
		// PerRequestPrice is nil
	}
	result := purepricing.CalculateStatsCost(pricing, purepricing.UsageTokens{}, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_PerRequestBilling_ExplicitZeroPrice(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode:     purepricing.BillingModePerRequest,
		PerRequestPrice: contractFloat(0),
	}
	result := purepricing.CalculateStatsCost(pricing, purepricing.UsageTokens{}, 1)
	require.NotNil(t, result)
	require.Zero(t, *result)
}

func TestCalculateStatsCost_ImageBilling(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode:     purepricing.BillingModeImage,
		PerRequestPrice: contractFloat(0.10),
	}
	result := purepricing.CalculateStatsCost(pricing, purepricing.UsageTokens{}, 2)
	require.NotNil(t, result)
	// 0.10 * 2 = 0.20
	require.InDelta(t, 0.20, *result, 1e-12)
}

func TestCalculateStatsCost_ImageBilling_PriceNil(t *testing.T) {
	pricing := &purepricing.ModelPricingEntry{
		BillingMode: purepricing.BillingModeImage,
		// PerRequestPrice is nil
	}
	result := purepricing.CalculateStatsCost(pricing, purepricing.UsageTokens{}, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_AppliesPriceMultiplier(t *testing.T) {
	t.Run("token 定价", func(t *testing.T) {
		pricing := &purepricing.ModelPricingEntry{
			BillingMode:     purepricing.BillingModeToken,
			PriceMultiplier: contractFloat(1.5),
			InputPrice:      contractFloat(0.001),
		}
		result := purepricing.CalculateStatsCost(pricing, purepricing.UsageTokens{InputTokens: 100}, 1)
		require.NotNil(t, result)
		require.InDelta(t, 0.15, *result, 1e-12)
	})

	t.Run("按次定价", func(t *testing.T) {
		pricing := &purepricing.ModelPricingEntry{
			BillingMode:     purepricing.BillingModePerRequest,
			PriceMultiplier: contractFloat(0),
			PerRequestPrice: contractFloat(0.25),
		}
		result := purepricing.CalculateStatsCost(pricing, purepricing.UsageTokens{}, 2)
		require.NotNil(t, result)
		require.Zero(t, *result)
	})
}

func TestCalculateStatsCost_DefaultBillingMode_FallsToToken(t *testing.T) {
	// BillingMode is empty string (default) → falls into token billing
	pricing := &purepricing.ModelPricingEntry{
		InputPrice:  contractFloat(0.001),
		OutputPrice: contractFloat(0.002),
	}
	tokens := purepricing.UsageTokens{
		InputTokens:  100,
		OutputTokens: 50,
	}
	result := purepricing.CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestTryCustomRules_FirstMatchWins(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []purepricing.ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []purepricing.ProviderStatsPricingRule{
			{
				GroupIDs: []int64{1},
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 100, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.01), OutputPrice: contractFloat(0.02)},
				},
			},
			{
				GroupIDs: []int64{1},
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 200, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.99), OutputPrice: contractFloat(0.99)},
				},
			},
		},
	}
	tokens := purepricing.UsageTokens{InputTokens: 100, OutputTokens: 50}
	result := purepricing.TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 1, "claude-opus-4", tokens, 1)
	require.NotNil(t, result)
	// 应使用第一条规则的价格：100*0.01 + 50*0.02 = 2.0
	require.InDelta(t, 2.0, *result, 1e-12)
}

func TestTryCustomRules_SkipsNonMatchingRules(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []purepricing.ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []purepricing.ProviderStatsPricingRule{
			{
				ProviderIDs: []int64{888}, // 不匹配
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 100, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.99)},
				},
			},
			{
				GroupIDs: []int64{1}, // 匹配
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 200, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.05)},
				},
			},
		},
	}
	tokens := purepricing.UsageTokens{InputTokens: 100}
	result := purepricing.TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 1, "claude-opus-4", tokens, 1)
	require.NotNil(t, result)
	// 跳过规则1（提供商不匹配），使用规则2：100*0.05 = 5.0
	require.InDelta(t, 5.0, *result, 1e-12)
}

func TestTryCustomRules_NoMatch_ReturnsNil(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []purepricing.ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []purepricing.ProviderStatsPricingRule{
			{
				ProviderIDs: []int64{888},
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 100, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.01)},
				},
			},
		},
	}
	tokens := purepricing.UsageTokens{InputTokens: 100}
	result := purepricing.TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 2, "claude-opus-4", tokens, 1)
	require.Nil(t, result) // 提供商和分组都不匹配
}

func TestTryCustomRules_RuleMatchesButModelNot_ContinuesToNext(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []purepricing.ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []purepricing.ProviderStatsPricingRule{
			{
				GroupIDs: []int64{1},
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 100, Models: []string{"gpt-4o"}, InputPrice: contractFloat(0.01)}, // 模型不匹配
				},
			},
			{
				GroupIDs: []int64{1},
				Pricing: []purepricing.ModelPricingEntry{
					{ID: 200, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.05)}, // 模型匹配
				},
			},
		},
	}
	tokens := purepricing.UsageTokens{InputTokens: 100}
	result := purepricing.TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 1, "claude-opus-4", tokens, 1)
	require.NotNil(t, result)
	require.InDelta(t, 5.0, *result, 1e-12) // 使用规则2
}

func contractFloat(v float64) *float64 { return &v }
