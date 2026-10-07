package scheduler

import (
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestFilterByMinPriority(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		result := FilterByMinPriority(nil)
		require.Empty(t, result)
	})

	t.Run("single provider", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, Priority: 5}, Load: &ProviderLoadInfo{}},
		}
		result := FilterByMinPriority(providers)
		require.Len(t, result, 1)
		require.Equal(t, int64(1), result[0].Provider.ID)
	})

	t.Run("multiple providers same priority", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, Priority: 3}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, Priority: 3}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 3, Priority: 3}, Load: &ProviderLoadInfo{}},
		}
		result := FilterByMinPriority(providers)
		require.Len(t, result, 3)
	})

	t.Run("filters to min priority only", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, Priority: 5}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, Priority: 1}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 3, Priority: 3}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 4, Priority: 1}, Load: &ProviderLoadInfo{}},
		}
		result := FilterByMinPriority(providers)
		require.Len(t, result, 2)
		require.Equal(t, int64(2), result[0].Provider.ID)
		require.Equal(t, int64(4), result[1].Provider.ID)
	})
}

func TestFilterByMinLoadRate(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		result := FilterByMinLoadRate(nil)
		require.Empty(t, result)
	})

	t.Run("single provider", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1}, Load: &ProviderLoadInfo{LoadRate: 50}},
		}
		result := FilterByMinLoadRate(providers)
		require.Len(t, result, 1)
		require.Equal(t, int64(1), result[0].Provider.ID)
	})

	t.Run("multiple providers same load rate", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1}, Load: &ProviderLoadInfo{LoadRate: 20}},
			{Provider: &BasicProvider{ID: 2}, Load: &ProviderLoadInfo{LoadRate: 20}},
			{Provider: &BasicProvider{ID: 3}, Load: &ProviderLoadInfo{LoadRate: 20}},
		}
		result := FilterByMinLoadRate(providers)
		require.Len(t, result, 3)
	})

	t.Run("filters to min load rate only", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1}, Load: &ProviderLoadInfo{LoadRate: 80}},
			{Provider: &BasicProvider{ID: 2}, Load: &ProviderLoadInfo{LoadRate: 10}},
			{Provider: &BasicProvider{ID: 3}, Load: &ProviderLoadInfo{LoadRate: 50}},
			{Provider: &BasicProvider{ID: 4}, Load: &ProviderLoadInfo{LoadRate: 10}},
		}
		result := FilterByMinLoadRate(providers)
		require.Len(t, result, 2)
		require.Equal(t, int64(2), result[0].Provider.ID)
		require.Equal(t, int64(4), result[1].Provider.ID)
	})

	t.Run("zero load rate", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1}, Load: &ProviderLoadInfo{LoadRate: 0}},
			{Provider: &BasicProvider{ID: 2}, Load: &ProviderLoadInfo{LoadRate: 50}},
			{Provider: &BasicProvider{ID: 3}, Load: &ProviderLoadInfo{LoadRate: 0}},
		}
		result := FilterByMinLoadRate(providers)
		require.Len(t, result, 2)
		require.Equal(t, int64(1), result[0].Provider.ID)
		require.Equal(t, int64(3), result[1].Provider.ID)
	})
}

func TestSelectByLRU(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	muchEarlier := now.Add(-2 * time.Hour)

	t.Run("empty slice", func(t *testing.T) {
		result := SelectByLRU(nil, false)
		require.Nil(t, result)
	})

	t.Run("single provider", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{}},
		}
		result := SelectByLRU(providers, false)
		require.NotNil(t, result)
		require.Equal(t, int64(1), result.Provider.ID)
	})

	t.Run("selects least recently used", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: &muchEarlier}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 3, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{}},
		}
		result := SelectByLRU(providers, false)
		require.NotNil(t, result)
		require.Equal(t, int64(2), result.Provider.ID)
	})

	t.Run("nil LastUsedAt preferred over non-nil", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: nil}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 3, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{}},
		}
		result := SelectByLRU(providers, false)
		require.NotNil(t, result)
		require.Equal(t, int64(2), result.Provider.ID)
	})

	t.Run("multiple nil LastUsedAt random selection", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: nil, Type: "session"}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: nil, Type: "session"}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 3, LastUsedAt: nil, Type: "session"}, Load: &ProviderLoadInfo{}},
		}
		// 多次调用应该随机选择，验证结果都在候选范围内
		validIDs := map[int64]bool{1: true, 2: true, 3: true}
		for i := 0; i < 10; i++ {
			result := SelectByLRU(providers, false)
			require.NotNil(t, result)
			require.True(t, validIDs[result.Provider.ID], "selected ID should be one of the candidates")
		}
	})

	t.Run("multiple same LastUsedAt random selection", func(t *testing.T) {
		sameTime := now
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: &sameTime}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: &sameTime}, Load: &ProviderLoadInfo{}},
		}
		// 多次调用应该随机选择
		validIDs := map[int64]bool{1: true, 2: true}
		for i := 0; i < 10; i++ {
			result := SelectByLRU(providers, false)
			require.NotNil(t, result)
			require.True(t, validIDs[result.Provider.ID], "selected ID should be one of the candidates")
		}
	})

	t.Run("preferOAuth selects from OAuth providers when multiple nil", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: nil, Type: "session"}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: nil, Type: capability.ProviderTypeOAuth}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 3, LastUsedAt: nil, Type: capability.ProviderTypeOAuth}, Load: &ProviderLoadInfo{}},
		}
		// preferOAuth 时，应该从 OAuth 类型中选择
		oauthIDs := map[int64]bool{2: true, 3: true}
		for i := 0; i < 10; i++ {
			result := SelectByLRU(providers, true)
			require.NotNil(t, result)
			require.True(t, oauthIDs[result.Provider.ID], "should select from OAuth providers")
		}
	})

	t.Run("preferOAuth falls back to all when no OAuth", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: nil, Type: "session"}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: nil, Type: "session"}, Load: &ProviderLoadInfo{}},
		}
		// 没有 OAuth 时，从所有候选中选择
		validIDs := map[int64]bool{1: true, 2: true}
		for i := 0; i < 10; i++ {
			result := SelectByLRU(providers, true)
			require.NotNil(t, result)
			require.True(t, validIDs[result.Provider.ID])
		}
	})

	t.Run("preferOAuth only affects same LastUsedAt providers", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, LastUsedAt: &earlier, Type: "session"}, Load: &ProviderLoadInfo{}},
			{Provider: &BasicProvider{ID: 2, LastUsedAt: &now, Type: capability.ProviderTypeOAuth}, Load: &ProviderLoadInfo{}},
		}
		result := SelectByLRU(providers, true)
		require.NotNil(t, result)
		// LastUsedAt 不同时，选择时间最早的提供商。
		require.Equal(t, int64(1), result.Provider.ID)
	})
}

func TestLayeredFilterIntegration(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	muchEarlier := now.Add(-2 * time.Hour)

	t.Run("full layered selection", func(t *testing.T) {
		// 候选提供商具有不同的优先级、负载率和最后使用时间。
		providers := []BasicCandidate{
			// 优先级 1，负载 50%
			{Provider: &BasicProvider{ID: 1, Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 50}},
			// 优先级 1，负载 20%（最低）
			{Provider: &BasicProvider{ID: 2, Priority: 1, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{LoadRate: 20}},
			// 优先级 1，负载 20%（最低），更早使用
			{Provider: &BasicProvider{ID: 3, Priority: 1, LastUsedAt: &muchEarlier}, Load: &ProviderLoadInfo{LoadRate: 20}},
			// 优先级 2（较低优先）
			{Provider: &BasicProvider{ID: 4, Priority: 2, LastUsedAt: &muchEarlier}, Load: &ProviderLoadInfo{LoadRate: 0}},
		}

		// 1. 取优先级最小的集合 → ID: 1, 2, 3
		step1 := FilterByMinPriority(providers)
		require.Len(t, step1, 3)

		// 2. 取负载率最低的集合 → ID: 2, 3
		step2 := FilterByMinLoadRate(step1)
		require.Len(t, step2, 2)

		// 3. LRU 选择 → ID: 3（muchEarlier 最早）
		selected := SelectByLRU(step2, false)
		require.NotNil(t, selected)
		require.Equal(t, int64(3), selected.Provider.ID)
	})

	t.Run("all same priority and load rate", func(t *testing.T) {
		providers := []BasicCandidate{
			{Provider: &BasicProvider{ID: 1, Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 50}},
			{Provider: &BasicProvider{ID: 2, Priority: 1, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{LoadRate: 50}},
			{Provider: &BasicProvider{ID: 3, Priority: 1, LastUsedAt: &muchEarlier}, Load: &ProviderLoadInfo{LoadRate: 50}},
		}

		step1 := FilterByMinPriority(providers)
		require.Len(t, step1, 3)

		step2 := FilterByMinLoadRate(step1)
		require.Len(t, step2, 3)

		// LRU 选择最早的
		selected := SelectByLRU(step2, false)
		require.NotNil(t, selected)
		require.Equal(t, int64(3), selected.Provider.ID)
	})
}
