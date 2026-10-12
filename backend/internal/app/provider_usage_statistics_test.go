package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// windowPairSource 为装配测试同时提供双窗口和多提供商批量查询。
type windowPairSource struct {
	usage.UsageLogRepository
	pairs int
}

// GetProviderWindowStatsPair 用不同费用字段检查映射顺序。
func (s *windowPairSource) GetProviderWindowStatsPair(context.Context, int64, time.Time, time.Time) (*usage.ProviderStats, *usage.ProviderStats, error) {
	s.pairs++
	return &usage.ProviderStats{Requests: 1, Tokens: 2, Cost: 3, StandardCost: 4, UserCost: 5}, &usage.ProviderStats{Requests: 6}, nil
}

// GetProviderWindowStatsBatch 提供可探测的批量能力。
func (s *windowPairSource) GetProviderWindowStatsBatch(context.Context, []int64, time.Time) (map[int64]*usage.ProviderStats, error) {
	return map[int64]*usage.ProviderStats{}, nil
}

// TestProviderLocalStatsKeepsBothBatchCapabilities 双窗口优化同时保留今日统计的批量能力。
func TestProviderLocalStatsKeepsBothBatchCapabilities(t *testing.T) {
	source := &windowPairSource{}
	reader := newProviderLocalUsageStats(source)
	_, ok := reader.(provider.LocalUsageStatsBatch)
	require.True(t, ok)
	pair, ok := reader.(provider.LocalUsageStatsPair)
	require.True(t, ok)
	a, b, err := pair.GetProviderWindowStatsPair(context.Background(), 1, time.Now(), time.Now())
	require.NoError(t, err)
	require.Equal(t, &provider.WindowStats{Requests: 1, Tokens: 2, Cost: 3, StandardCost: 4, UserCost: 5}, a)
	require.EqualValues(t, 6, b.Requests)
	require.Equal(t, 1, source.pairs)
}
