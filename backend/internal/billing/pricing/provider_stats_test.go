package pricing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchProviderStatsRule_BothEmpty_NoMatch(t *testing.T) {
	rule := &ProviderStatsPricingRule{}
	require.False(t, MatchProviderStatsRule(rule, 1, 10))
}

func TestMatchProviderStatsRule_ProviderIDMatch(t *testing.T) {
	rule := &ProviderStatsPricingRule{ProviderIDs: []int64{1, 2, 3}}
	require.True(t, MatchProviderStatsRule(rule, 2, 999))
}

func TestMatchProviderStatsRule_GroupIDMatch(t *testing.T) {
	rule := &ProviderStatsPricingRule{GroupIDs: []int64{10, 20}}
	require.True(t, MatchProviderStatsRule(rule, 999, 20))
}

func TestMatchProviderStatsRule_BothConfigured_ProviderMatch(t *testing.T) {
	rule := &ProviderStatsPricingRule{
		ProviderIDs: []int64{1, 2},
		GroupIDs:    []int64{10, 20},
	}
	require.True(t, MatchProviderStatsRule(rule, 2, 999))
}

func TestMatchProviderStatsRule_BothConfigured_GroupMatch(t *testing.T) {
	rule := &ProviderStatsPricingRule{
		ProviderIDs: []int64{1, 2},
		GroupIDs:    []int64{10, 20},
	}
	require.True(t, MatchProviderStatsRule(rule, 999, 10))
}

func TestMatchProviderStatsRule_BothConfigured_NeitherMatch(t *testing.T) {
	rule := &ProviderStatsPricingRule{
		ProviderIDs: []int64{1, 2},
		GroupIDs:    []int64{10, 20},
	}
	require.False(t, MatchProviderStatsRule(rule, 999, 999))
}

func TestFindPricingForModel(t *testing.T) {
	exactPricing := ModelPricingEntry{
		ID:     1,
		Models: []string{"claude-opus-4"},
	}
	wildcardPricing := ModelPricingEntry{
		ID:     2,
		Models: []string{"claude-*"},
	}
	additionalPricing := ModelPricingEntry{
		ID: 3,

		Models: []string{"gpt-4o"},
	}
	geminiPricing := ModelPricingEntry{
		ID:     4,
		Models: []string{"gemini-2.5-pro"},
	}

	tests := []struct {
		name    string
		list    []ModelPricingEntry
		model   string
		wantID  int64
		wantNil bool
	}{
		{
			name:   "exact match",
			list:   []ModelPricingEntry{exactPricing},
			model:  "claude-opus-4",
			wantID: 1,
		},
		{
			name:   "exact match case insensitive",
			list:   []ModelPricingEntry{{ID: 5, Models: []string{"Claude-Opus-4"}}},
			model:  "claude-opus-4",
			wantID: 5,
		},
		{
			name:   "wildcard match",
			list:   []ModelPricingEntry{wildcardPricing},
			model:  "claude-opus-4",
			wantID: 2,
		},
		{
			name:   "exact match takes priority over wildcard",
			list:   []ModelPricingEntry{wildcardPricing, exactPricing},
			model:  "claude-opus-4",
			wantID: 1,
		},
		{
			name:   "same card includes different model vendors",
			list:   []ModelPricingEntry{exactPricing, additionalPricing, geminiPricing},
			model:  "gpt-4o",
			wantID: 3,
		},
		{
			name:   "Gemini model shares the same card",
			list:   []ModelPricingEntry{exactPricing, additionalPricing, geminiPricing},
			model:  "gemini-2.5-pro",
			wantID: 4,
		},
		{
			name:    "no match at all",
			list:    []ModelPricingEntry{exactPricing, wildcardPricing},
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
			list: []ModelPricingEntry{
				{ID: 10, Models: []string{"claude-*"}},
				{ID: 11, Models: []string{"claude-opus-*"}},
			},
			model:  "claude-opus-4",
			wantID: 10, // claude-* 排在前面且匹配，采用该规则。
		},
		{
			name: "shorter wildcard used when longer does not match",
			list: []ModelPricingEntry{
				{ID: 10, Models: []string{"claude-*"}},
				{ID: 11, Models: []string{"claude-opus-*"}},
			},
			model:  "claude-sonnet-4",
			wantID: 10, // 匹配 claude-*。
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindPricingForModelByPredicate(tt.list, tt.model, nil)
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
	result := CalculateStatsCost(nil, UsageTokens{}, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_TokenBilling(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModeToken,
		InputPrice:  contractFloat(0.001),
		OutputPrice: contractFloat(0.002),
	}
	tokens := UsageTokens{
		InputTokens:  100,
		OutputTokens: 50,
	}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 100*0.001 + 50*0.002 = 0.1 + 0.1 = 0.2
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_WithCache(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode:     BillingModeToken,
		InputPrice:      contractFloat(0.001),
		OutputPrice:     contractFloat(0.002),
		CacheWritePrice: contractFloat(0.003),
		CacheReadPrice:  contractFloat(0.0005),
	}
	tokens := UsageTokens{
		InputTokens:         100,
		OutputTokens:        50,
		CacheCreationTokens: 200,
		CacheReadTokens:     300,
	}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 100*0.001 + 50*0.002 + 200*0.003 + 300*0.0005
	// = 0.1 + 0.1 + 0.6 + 0.15 = 0.95
	require.InDelta(t, 0.95, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_WithCacheTTLPricing(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode:       BillingModeToken,
		CacheWritePrice:   contractFloat(0.003),
		CacheWrite1hPrice: contractFloat(0.006),
	}
	tokens := UsageTokens{
		CacheCreationTokens:   100,
		CacheCreation5mTokens: 40,
		CacheCreation1hTokens: 60,
	}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	require.InDelta(t, 0.48, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_WithImageOutput(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode:      BillingModeToken,
		InputPrice:       contractFloat(0.001),
		OutputPrice:      contractFloat(0.002),
		ImageOutputPrice: contractFloat(0.01),
	}
	tokens := UsageTokens{
		InputTokens:       100,
		OutputTokens:      50,
		ImageOutputTokens: 10,
	}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 统计费用分别累计 output 和 image output。
	// 100*0.001 + 50*0.002 + 10*0.01 = 0.1 + 0.1 + 0.1 = 0.3
	require.InDelta(t, 0.3, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_PartialPricesNil(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModeToken,
		InputPrice:  contractFloat(0.001),
		// 未填写的输出和缓存价格按 0 计算。
	}
	tokens := UsageTokens{
		InputTokens:         100,
		OutputTokens:        50,
		CacheCreationTokens: 200,
	}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	// 输入费用为 100*0.001 = 0.1。
	require.InDelta(t, 0.1, *result, 1e-12)
}

func TestCalculateStatsCost_TokenBilling_AllTokensZero(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModeToken,
		InputPrice:  contractFloat(0.001),
		OutputPrice: contractFloat(0.002),
	}
	tokens := UsageTokens{} // 用量均为零。
	result := CalculateStatsCost(pricing, tokens, 1)
	// 用量为零时返回 nil，由调用方使用默认计价公式。
	require.Nil(t, result)
}

func TestCalculateStatsCost_TokenBilling_ExplicitZeroPriceOverrides(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModeToken,
		InputPrice:  contractFloat(0),
	}
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	require.Zero(t, *result)
}

func TestCalculateStatsCost_TokenBilling_BlankPricingDoesNotOverride(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModeToken,
	}
	tokens := UsageTokens{InputTokens: 100}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_PerRequestBilling(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: contractFloat(0.05),
	}
	tokens := UsageTokens{InputTokens: 999, OutputTokens: 999}
	result := CalculateStatsCost(pricing, tokens, 3)
	require.NotNil(t, result)
	// 0.05 * 3 = 0.15
	require.InDelta(t, 0.15, *result, 1e-12)
}

func TestCalculateStatsCost_PerRequestBilling_PriceNil(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModePerRequest,
		// 按次价格未配置。
	}
	result := CalculateStatsCost(pricing, UsageTokens{}, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_PerRequestBilling_ExplicitZeroPrice(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: contractFloat(0),
	}
	result := CalculateStatsCost(pricing, UsageTokens{}, 1)
	require.NotNil(t, result)
	require.Zero(t, *result)
}

func TestCalculateStatsCost_ImageBilling(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode:     BillingModeImage,
		PerRequestPrice: contractFloat(0.10),
	}
	result := CalculateStatsCost(pricing, UsageTokens{}, 2)
	require.NotNil(t, result)
	// 0.10 * 2 = 0.20
	require.InDelta(t, 0.20, *result, 1e-12)
}

func TestCalculateStatsCost_ImageBilling_PriceNil(t *testing.T) {
	pricing := &ModelPricingEntry{
		BillingMode: BillingModeImage,
		// 按次价格未配置。
	}
	result := CalculateStatsCost(pricing, UsageTokens{}, 1)
	require.Nil(t, result)
}

func TestCalculateStatsCost_AppliesPriceMultiplier(t *testing.T) {
	t.Run("token 定价", func(t *testing.T) {
		pricing := &ModelPricingEntry{
			BillingMode:     BillingModeToken,
			PriceMultiplier: contractFloat(1.5),
			InputPrice:      contractFloat(0.001),
		}
		result := CalculateStatsCost(pricing, UsageTokens{InputTokens: 100}, 1)
		require.NotNil(t, result)
		require.InDelta(t, 0.15, *result, 1e-12)
	})

	t.Run("按次定价", func(t *testing.T) {
		pricing := &ModelPricingEntry{
			BillingMode:     BillingModePerRequest,
			PriceMultiplier: contractFloat(0),
			PerRequestPrice: contractFloat(0.25),
		}
		result := CalculateStatsCost(pricing, UsageTokens{}, 2)
		require.NotNil(t, result)
		require.Zero(t, *result)
	})
}

func TestCalculateStatsCost_DefaultBillingMode_FallsToToken(t *testing.T) {
	// BillingMode 为空时按 token 计费。
	pricing := &ModelPricingEntry{
		InputPrice:  contractFloat(0.001),
		OutputPrice: contractFloat(0.002),
	}
	tokens := UsageTokens{
		InputTokens:  100,
		OutputTokens: 50,
	}
	result := CalculateStatsCost(pricing, tokens, 1)
	require.NotNil(t, result)
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestTryCustomRules_FirstMatchWins(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []ProviderStatsPricingRule{
			{
				GroupIDs: []int64{1},
				Pricing: []ModelPricingEntry{
					{ID: 100, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.01), OutputPrice: contractFloat(0.02)},
				},
			},
			{
				GroupIDs: []int64{1},
				Pricing: []ModelPricingEntry{
					{ID: 200, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.99), OutputPrice: contractFloat(0.99)},
				},
			},
		},
	}
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}
	result := TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 1, "claude-opus-4", tokens, 1)
	require.NotNil(t, result)
	// 应使用第一条规则的价格：100*0.01 + 50*0.02 = 2.0
	require.InDelta(t, 2.0, *result, 1e-12)
}

func TestTryCustomRules_SkipsNonMatchingRules(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []ProviderStatsPricingRule{
			{
				ProviderIDs: []int64{888}, // 不匹配
				Pricing: []ModelPricingEntry{
					{ID: 100, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.99)},
				},
			},
			{
				GroupIDs: []int64{1}, // 匹配
				Pricing: []ModelPricingEntry{
					{ID: 200, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.05)},
				},
			},
		},
	}
	tokens := UsageTokens{InputTokens: 100}
	result := TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 1, "claude-opus-4", tokens, 1)
	require.NotNil(t, result)
	// 跳过规则 1（提供商不匹配），使用规则 2：100*0.05 = 5.0
	require.InDelta(t, 5.0, *result, 1e-12)
}

func TestTryCustomRules_NoMatch_ReturnsNil(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []ProviderStatsPricingRule{
			{
				ProviderIDs: []int64{888},
				Pricing: []ModelPricingEntry{
					{ID: 100, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.01)},
				},
			},
		},
	}
	tokens := UsageTokens{InputTokens: 100}
	result := TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 2, "claude-opus-4", tokens, 1)
	require.Nil(t, result) // 提供商和分组都不匹配
}

func TestTryCustomRules_RuleMatchesButModelNot_ContinuesToNext(t *testing.T) {
	configPricing := &struct {
		ProviderStatsPricingRules []ProviderStatsPricingRule
	}{
		ProviderStatsPricingRules: []ProviderStatsPricingRule{
			{
				GroupIDs: []int64{1},
				Pricing: []ModelPricingEntry{
					{ID: 100, Models: []string{"gpt-4o"}, InputPrice: contractFloat(0.01)}, // 模型不匹配
				},
			},
			{
				GroupIDs: []int64{1},
				Pricing: []ModelPricingEntry{
					{ID: 200, Models: []string{"claude-opus-4"}, InputPrice: contractFloat(0.05)}, // 模型匹配
				},
			},
		},
	}
	tokens := UsageTokens{InputTokens: 100}
	result := TryCustomRules(configPricing.ProviderStatsPricingRules, 999, 1, "claude-opus-4", tokens, 1)
	require.NotNil(t, result)
	require.InDelta(t, 5.0, *result, 1e-12) // 使用规则2
}

// contractFloat 返回测试价卡中的可空价格。
func contractFloat(v float64) *float64 { return &v }
