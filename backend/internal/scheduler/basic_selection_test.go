package scheduler

import (
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func testTimePtr(t time.Time) *time.Time { return &t }

func makeAccWithLoad(id int64, priority int, loadRate int, lastUsed *time.Time, accType string) BasicCandidate {
	return BasicCandidate{
		Provider: &BasicProvider{
			ID:         id,
			Priority:   priority,
			LastUsedAt: lastUsed,
			Type:       accType,
		},
		Load: &ProviderLoadInfo{
			ProviderID:         id,
			CurrentConcurrency: 0,
			LoadRate:           loadRate,
		},
	}
}

func TestSortProvidersByPriorityAndLastUsed_ByPriority(t *testing.T) {
	now := time.Now()
	providers := []*BasicProvider{
		{ID: 1, Priority: 5, LastUsedAt: testTimePtr(now)},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 3, Priority: 3, LastUsedAt: testTimePtr(now)},
	}
	SortProvidersByPriorityAndLastUsed(providers, false)
	require.Equal(t, int64(2), providers[0].ID, "优先级最低的排第一")
	require.Equal(t, int64(3), providers[1].ID)
	require.Equal(t, int64(1), providers[2].ID)
}

func TestSortProvidersByPriorityAndLastUsed_SamePriorityByLastUsed(t *testing.T) {
	now := time.Now()
	providers := []*BasicProvider{
		{ID: 1, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now.Add(-1 * time.Hour))},
		{ID: 3, Priority: 1, LastUsedAt: nil},
	}
	SortProvidersByPriorityAndLastUsed(providers, false)
	require.Equal(t, int64(3), providers[0].ID, "nil LastUsedAt 排最前")
	require.Equal(t, int64(2), providers[1].ID, "更早使用的排前面")
	require.Equal(t, int64(1), providers[2].ID)
}

func TestSortProvidersByPriorityAndLastUsed_PreferOAuth(t *testing.T) {
	providers := []*BasicProvider{
		{ID: 1, Priority: 1, LastUsedAt: nil, Type: capability.ProviderTypeAPIKey},
		{ID: 2, Priority: 1, LastUsedAt: nil, Type: capability.ProviderTypeOAuth},
	}
	SortProvidersByPriorityAndLastUsed(providers, true)
	require.Equal(t, int64(2), providers[0].ID, "preferOAuth 时 OAuth 提供商排前面")
}

func TestSortProvidersByPriorityAndLastUsed_StableSort(t *testing.T) {
	providers := []*BasicProvider{
		{ID: 1, Priority: 1, LastUsedAt: nil, Type: capability.ProviderTypeAPIKey},
		{ID: 2, Priority: 1, LastUsedAt: nil, Type: capability.ProviderTypeAPIKey},
		{ID: 3, Priority: 1, LastUsedAt: nil, Type: capability.ProviderTypeAPIKey},
	}

	// sortProvidersByPriorityAndLastUsed 随机打散同一 Priority 和 LastUsedAt 的候选。
	// 多次运行后元素集合应相同，并出现不同顺序。
	seenFirst := map[int64]bool{}
	for i := 0; i < 100; i++ {
		cpy := make([]*BasicProvider, len(providers))
		copy(cpy, providers)
		SortProvidersByPriorityAndLastUsed(cpy, false)
		seenFirst[cpy[0].ID] = true

		ids := map[int64]bool{}
		for _, a := range cpy {
			ids[a.ID] = true
		}
		require.True(t, ids[1] && ids[2] && ids[3])
	}
	require.GreaterOrEqual(t, len(seenFirst), 2, "同组提供商应能被随机打散")
}

func TestSortProvidersByPriorityAndLastUsed_MixedPriorityAndTime(t *testing.T) {
	now := time.Now()
	providers := []*BasicProvider{
		{ID: 1, Priority: 2, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 3, Priority: 1, LastUsedAt: testTimePtr(now.Add(-1 * time.Hour))},
		{ID: 4, Priority: 2, LastUsedAt: testTimePtr(now.Add(-2 * time.Hour))},
	}
	SortProvidersByPriorityAndLastUsed(providers, false)
	// 优先级1排前：nil < earlier
	require.Equal(t, int64(3), providers[0].ID, "优先级1 + 更早")
	require.Equal(t, int64(2), providers[1].ID, "优先级1 + 现在")
	// 优先级2排后：nil < time
	require.Equal(t, int64(1), providers[2].ID, "优先级2 + nil")
	require.Equal(t, int64(4), providers[3].ID, "优先级2 + 有时间")
}

func TestFilterByMinPriority_Empty(t *testing.T) {
	result := FilterByMinPriority(nil)
	require.Nil(t, result)
}

func TestFilterByMinPriority_SelectsMinPriority(t *testing.T) {
	providers := []BasicCandidate{
		makeAccWithLoad(1, 5, 10, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(2, 1, 10, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(3, 1, 20, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(4, 2, 10, nil, capability.ProviderTypeAPIKey),
	}
	result := FilterByMinPriority(providers)
	require.Len(t, result, 2)
	require.Equal(t, int64(2), result[0].Provider.ID)
	require.Equal(t, int64(3), result[1].Provider.ID)
}

func TestFilterByMinLoadRate_Empty(t *testing.T) {
	result := FilterByMinLoadRate(nil)
	require.Nil(t, result)
}

func TestFilterByMinLoadRate_SelectsMinLoadRate(t *testing.T) {
	providers := []BasicCandidate{
		makeAccWithLoad(1, 1, 30, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(2, 1, 10, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(3, 1, 10, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(4, 1, 20, nil, capability.ProviderTypeAPIKey),
	}
	result := FilterByMinLoadRate(providers)
	require.Len(t, result, 2)
	require.Equal(t, int64(2), result[0].Provider.ID)
	require.Equal(t, int64(3), result[1].Provider.ID)
}

func TestSelectByLRU_Empty(t *testing.T) {
	result := SelectByLRU(nil, false)
	require.Nil(t, result)
}

func TestSelectByLRU_Single(t *testing.T) {
	providers := []BasicCandidate{makeAccWithLoad(1, 1, 10, nil, capability.ProviderTypeAPIKey)}
	result := SelectByLRU(providers, false)
	require.NotNil(t, result)
	require.Equal(t, int64(1), result.Provider.ID)
}

func TestSelectByLRU_NilLastUsedAtWins(t *testing.T) {
	now := time.Now()
	providers := []BasicCandidate{
		makeAccWithLoad(1, 1, 10, testTimePtr(now), capability.ProviderTypeAPIKey),
		makeAccWithLoad(2, 1, 10, nil, capability.ProviderTypeAPIKey),
		makeAccWithLoad(3, 1, 10, testTimePtr(now.Add(-1*time.Hour)), capability.ProviderTypeAPIKey),
	}
	result := SelectByLRU(providers, false)
	require.NotNil(t, result)
	require.Equal(t, int64(2), result.Provider.ID)
}

func TestSelectByLRU_EarliestTimeWins(t *testing.T) {
	now := time.Now()
	providers := []BasicCandidate{
		makeAccWithLoad(1, 1, 10, testTimePtr(now), capability.ProviderTypeAPIKey),
		makeAccWithLoad(2, 1, 10, testTimePtr(now.Add(-1*time.Hour)), capability.ProviderTypeAPIKey),
		makeAccWithLoad(3, 1, 10, testTimePtr(now.Add(-2*time.Hour)), capability.ProviderTypeAPIKey),
	}
	result := SelectByLRU(providers, false)
	require.NotNil(t, result)
	require.Equal(t, int64(3), result.Provider.ID)
}

func TestSelectByLRU_TiePreferOAuth(t *testing.T) {
	now := time.Now()
	// 提供商 1/2 LastUsedAt 相同，且同为最小值。
	providers := []BasicCandidate{
		makeAccWithLoad(1, 1, 10, testTimePtr(now), capability.ProviderTypeAPIKey),
		makeAccWithLoad(2, 1, 10, testTimePtr(now), capability.ProviderTypeOAuth),
		makeAccWithLoad(3, 1, 10, testTimePtr(now.Add(1*time.Hour)), capability.ProviderTypeAPIKey),
	}
	for i := 0; i < 50; i++ {
		result := SelectByLRU(providers, true)
		require.NotNil(t, result)
		require.Equal(t, capability.ProviderTypeOAuth, result.Provider.Type)
		require.Equal(t, int64(2), result.Provider.ID)
	}
}
