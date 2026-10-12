package pricingcontract

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewaycapture "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

func TestCalculateWebSearchCostDefaultAndOverride(t *testing.T) {
	t.Parallel()
	s := alphaSearchCalculator()

	// 默认价：官方 $10/1000 次 = 0.01/次
	cost := s.CalculateWebSearchCost(1, nil, 1.0)
	require.InDelta(t, 0.01, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.01, cost.ActualCost, 1e-12)
	require.Equal(t, string(routing.BillingModePerRequest), cost.BillingMode)

	// 分组覆盖价 + 倍率
	cost = s.CalculateWebSearchCost(1, testPtrFloat64(0.02), 2.5)
	require.InDelta(t, 0.02, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.05, cost.ActualCost, 1e-12)

	// 0 = 免费（区别于 nil = 默认价）
	cost = s.CalculateWebSearchCost(1, testPtrFloat64(0), 3.0)
	require.Zero(t, cost.TotalCost)
	require.Zero(t, cost.ActualCost)

	// 负数倍率按 0 处理，避免按 1x 误扣
	cost = s.CalculateWebSearchCost(1, nil, -1)
	require.InDelta(t, 0.01, cost.TotalCost, 1e-12)
	require.Zero(t, cost.ActualCost)

	// 次数 <= 0 不产生费用
	cost = s.CalculateWebSearchCost(0, testPtrFloat64(0.02), 1.0)
	require.Zero(t, cost.TotalCost)
	require.Empty(t, cost.BillingMode)
}

func TestCalculateOpenAIRecordUsageCostWebSearchPerCall(t *testing.T) {
	t.Parallel()
	svc := completion.NewRecorder(completion.Dependencies{Calculator: alphaSearchCalculator()}, completion.RecorderOptions{DefaultMultiplier: 1})

	groupID := int64(11)

	// 分组未配置单价：默认 0.01。按次搜索使用不含高峰因子的基础倍率（第 4 个倍率参数 2.0），
	// 即使 token 倍率（含高峰，3.0）更高也不采用。
	apiKey := &completion.KeySnapshot{ID: 1, GroupID: &groupID, Group: &completion.GroupSnapshot{ID: groupID}}
	result := &forwardcore.OpenAIResult{Model: "gpt-5.6-sol", UpstreamModel: "gpt-5.6-sol", WebSearchCalls: 1}
	cost, err := svc.CalculateOpenAIRecordUsageCostAt(context.Background(), gatewaycapture.ProjectOpenAICompletionResult(result, nil), apiKey, []string{"gpt-5.6-sol"}, 3.0, 1.0, 1.0, 2.0, pricing.UsageTokens{}, "", time.Time{})
	require.NoError(t, err)
	require.Equal(t, string(routing.BillingModePerRequest), cost.BillingMode)
	require.InDelta(t, 0.01, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.02, cost.ActualCost, 1e-12)

	// 分组配置单价 0.005
	apiKey.Group.WebSearchPricePerCall = testPtrFloat64(0.005)
	cost, err = svc.CalculateOpenAIRecordUsageCostAt(context.Background(), gatewaycapture.ProjectOpenAICompletionResult(result, nil), apiKey, []string{"gpt-5.6-sol"}, 1.0, 1.0, 1.0, 1.0, pricing.UsageTokens{}, "", time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 0.005, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.005, cost.ActualCost, 1e-12)

	// WebSearchCalls = 0 时不得走按次分支（无定价数据会返回 pricing 错误，
	// 确认使用 token 计费分支）。
	result.WebSearchCalls = 0
	_, err = svc.CalculateOpenAIRecordUsageCostAt(context.Background(), gatewaycapture.ProjectOpenAICompletionResult(result, nil), apiKey, []string{"gpt-5.6-sol"}, 1.0, 1.0, 1.0, 1.0, pricing.UsageTokens{InputTokens: 10}, "", time.Time{})
	require.Error(t, err)
}

// alphaSearchCalculator 显式提供操作目录价，普通 token 查询仍为空。
func alphaSearchCalculator() *billing.Calculator {
	catalog := provider.NewServiceFromSnapshot(provider.Options{}, nil, provider.Snapshot{BillingDefaults: pricing.OperationPrices{WebSearchPricePerCall: testPtrFloat64(0.01)}})
	return billing.NewCalculator(catalog, billing.CalculatorOptions{})
}
