package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ============ ShuffleWithinSortGroups 测试 ============

func TestShuffleWithinSortGroups_Empty(t *testing.T) {
	ShuffleWithinSortGroups(nil)
	ShuffleWithinSortGroups([]BasicCandidate{})
}

func TestShuffleWithinSortGroups_SingleElement(t *testing.T) {
	providers := []BasicCandidate{
		{Provider: &BasicProvider{ID: 1, Priority: 1}, Load: &ProviderLoadInfo{LoadRate: 10}},
	}
	ShuffleWithinSortGroups(providers)
	require.Equal(t, int64(1), providers[0].Provider.ID)
}

func TestShuffleWithinSortGroups_DifferentGroups_OrderPreserved(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	providers := []BasicCandidate{
		{Provider: &BasicProvider{ID: 1, Priority: 1, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{LoadRate: 10}},
		{Provider: &BasicProvider{ID: 2, Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 20}},
		{Provider: &BasicProvider{ID: 3, Priority: 2, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{LoadRate: 10}},
	}

	// 每个元素都属于不同组（Priority 或 LoadRate 或 LastUsedAt 不同），顺序不变
	for i := 0; i < 20; i++ {
		cpy := make([]BasicCandidate, len(providers))
		copy(cpy, providers)
		ShuffleWithinSortGroups(cpy)
		require.Equal(t, int64(1), cpy[0].Provider.ID)
		require.Equal(t, int64(2), cpy[1].Provider.ID)
		require.Equal(t, int64(3), cpy[2].Provider.ID)
	}
}

func TestShuffleWithinSortGroups_SameGroup_Shuffled(t *testing.T) {
	now := time.Now()
	// 同一秒的时间戳视为同一组
	sameSecond := time.Unix(now.Unix(), 0)
	sameSecond2 := time.Unix(now.Unix(), 500_000_000) // 同一秒但不同纳秒

	providers := []BasicCandidate{
		{Provider: &BasicProvider{ID: 1, Priority: 1, LastUsedAt: &sameSecond}, Load: &ProviderLoadInfo{LoadRate: 10}},
		{Provider: &BasicProvider{ID: 2, Priority: 1, LastUsedAt: &sameSecond2}, Load: &ProviderLoadInfo{LoadRate: 10}},
		{Provider: &BasicProvider{ID: 3, Priority: 1, LastUsedAt: &sameSecond}, Load: &ProviderLoadInfo{LoadRate: 10}},
	}

	// 多次执行，验证所有 ID 都出现在第一个位置（说明确实被打乱了）
	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		cpy := make([]BasicCandidate, len(providers))
		copy(cpy, providers)
		ShuffleWithinSortGroups(cpy)
		seen[cpy[0].Provider.ID] = true
		// 无论怎么打乱，所有 ID 都应在候选中
		ids := map[int64]bool{}
		for _, a := range cpy {
			ids[a.Provider.ID] = true
		}
		require.True(t, ids[1] && ids[2] && ids[3])
	}
	// 至少 2 个不同的 ID 出现在首位（随机性验证）
	require.GreaterOrEqual(t, len(seen), 2, "shuffle should produce different orderings")
}

func TestShuffleWithinSortGroups_NilLastUsedAt_SameGroup(t *testing.T) {
	providers := []BasicCandidate{
		{Provider: &BasicProvider{ID: 1, Priority: 1, LastUsedAt: nil}, Load: &ProviderLoadInfo{LoadRate: 0}},
		{Provider: &BasicProvider{ID: 2, Priority: 1, LastUsedAt: nil}, Load: &ProviderLoadInfo{LoadRate: 0}},
		{Provider: &BasicProvider{ID: 3, Priority: 1, LastUsedAt: nil}, Load: &ProviderLoadInfo{LoadRate: 0}},
	}

	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		cpy := make([]BasicCandidate, len(providers))
		copy(cpy, providers)
		ShuffleWithinSortGroups(cpy)
		seen[cpy[0].Provider.ID] = true
	}
	require.GreaterOrEqual(t, len(seen), 2, "nil LastUsedAt providers should be shuffled")
}

func TestShuffleWithinSortGroups_MixedGroups(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	sameAsNow := time.Unix(now.Unix(), 0)

	// 组1: Priority=1, LoadRate=10, LastUsedAt=earlier (ID 1)，单元素组
	// 组2: Priority=1, LoadRate=20, LastUsedAt=now (ID 2, 3)，双元素组
	// 组3: Priority=2, LoadRate=10, LastUsedAt=earlier (ID 4)，单元素组
	providers := []BasicCandidate{
		{Provider: &BasicProvider{ID: 1, Priority: 1, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{LoadRate: 10}},
		{Provider: &BasicProvider{ID: 2, Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 20}},
		{Provider: &BasicProvider{ID: 3, Priority: 1, LastUsedAt: &sameAsNow}, Load: &ProviderLoadInfo{LoadRate: 20}},
		{Provider: &BasicProvider{ID: 4, Priority: 2, LastUsedAt: &earlier}, Load: &ProviderLoadInfo{LoadRate: 10}},
	}

	for i := 0; i < 20; i++ {
		cpy := make([]BasicCandidate, len(providers))
		copy(cpy, providers)
		ShuffleWithinSortGroups(cpy)

		// 组间顺序不变
		require.Equal(t, int64(1), cpy[0].Provider.ID, "group 1 position fixed")
		require.Equal(t, int64(4), cpy[3].Provider.ID, "group 3 position fixed")

		// 组2 内部可以打乱，但仍在位置 1 和 2
		mid := map[int64]bool{cpy[1].Provider.ID: true, cpy[2].Provider.ID: true}
		require.True(t, mid[2] && mid[3], "group 2 elements should stay in positions 1-2")
	}
}

// ============ ShuffleWithinPriorityAndLastUsed 测试 ============

func TestShuffleWithinPriorityAndLastUsed_Empty(t *testing.T) {
	ShuffleWithinPriorityAndLastUsed(nil, false)
	ShuffleWithinPriorityAndLastUsed([]*BasicProvider{}, false)
}

func TestShuffleWithinPriorityAndLastUsed_SingleElement(t *testing.T) {
	providers := []*BasicProvider{{ID: 1, Priority: 1}}
	ShuffleWithinPriorityAndLastUsed(providers, false)
	require.Equal(t, int64(1), providers[0].ID)
}

func TestShuffleWithinPriorityAndLastUsed_SameGroup_Shuffled(t *testing.T) {
	providers := []*BasicProvider{
		{ID: 1, Priority: 1, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: nil},
		{ID: 3, Priority: 1, LastUsedAt: nil},
	}

	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		cpy := make([]*BasicProvider, len(providers))
		copy(cpy, providers)
		ShuffleWithinPriorityAndLastUsed(cpy, false)
		seen[cpy[0].ID] = true
	}
	require.GreaterOrEqual(t, len(seen), 2, "same group should be shuffled")
}

func TestShuffleWithinPriorityAndLastUsed_DifferentPriority_OrderPreserved(t *testing.T) {
	providers := []*BasicProvider{
		{ID: 1, Priority: 1, LastUsedAt: nil},
		{ID: 2, Priority: 2, LastUsedAt: nil},
		{ID: 3, Priority: 3, LastUsedAt: nil},
	}

	for i := 0; i < 20; i++ {
		cpy := make([]*BasicProvider, len(providers))
		copy(cpy, providers)
		ShuffleWithinPriorityAndLastUsed(cpy, false)
		require.Equal(t, int64(1), cpy[0].ID)
		require.Equal(t, int64(2), cpy[1].ID)
		require.Equal(t, int64(3), cpy[2].ID)
	}
}

func TestShuffleWithinPriorityAndLastUsed_DifferentLastUsedAt_OrderPreserved(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	providers := []*BasicProvider{
		{ID: 1, Priority: 1, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: &earlier},
		{ID: 3, Priority: 1, LastUsedAt: &now},
	}

	for i := 0; i < 20; i++ {
		cpy := make([]*BasicProvider, len(providers))
		copy(cpy, providers)
		ShuffleWithinPriorityAndLastUsed(cpy, false)
		require.Equal(t, int64(1), cpy[0].ID)
		require.Equal(t, int64(2), cpy[1].ID)
		require.Equal(t, int64(3), cpy[2].ID)
	}
}

// ============ SameLastUsedAt 测试 ============

func TestSameLastUsedAt(t *testing.T) {
	now := time.Now()
	sameSecond := time.Unix(now.Unix(), 0)
	sameSecondDiffNano := time.Unix(now.Unix(), 999_999_999)
	differentSecond := now.Add(1 * time.Second)

	t.Run("both nil", func(t *testing.T) {
		require.True(t, SameLastUsedAt(nil, nil))
	})

	t.Run("one nil one not", func(t *testing.T) {
		require.False(t, SameLastUsedAt(nil, &now))
		require.False(t, SameLastUsedAt(&now, nil))
	})

	t.Run("same second different nanoseconds", func(t *testing.T) {
		require.True(t, SameLastUsedAt(&sameSecond, &sameSecondDiffNano))
	})

	t.Run("different seconds", func(t *testing.T) {
		require.False(t, SameLastUsedAt(&now, &differentSecond))
	})

	t.Run("exact same time", func(t *testing.T) {
		require.True(t, SameLastUsedAt(&now, &now))
	})
}

// ============ SameProviderWithLoadGroup 测试 ============

func TestSameProviderWithLoadGroup(t *testing.T) {
	now := time.Now()
	sameSecond := time.Unix(now.Unix(), 0)

	t.Run("same group", func(t *testing.T) {
		a := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 10}}
		b := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &sameSecond}, Load: &ProviderLoadInfo{LoadRate: 10}}
		require.True(t, SameProviderWithLoadGroup(a, b))
	})

	t.Run("different priority", func(t *testing.T) {
		a := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 10}}
		b := BasicCandidate{Provider: &BasicProvider{Priority: 2, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 10}}
		require.False(t, SameProviderWithLoadGroup(a, b))
	})

	t.Run("different load rate", func(t *testing.T) {
		a := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 10}}
		b := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 20}}
		require.False(t, SameProviderWithLoadGroup(a, b))
	})

	t.Run("different last used at", func(t *testing.T) {
		later := now.Add(1 * time.Second)
		a := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &now}, Load: &ProviderLoadInfo{LoadRate: 10}}
		b := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: &later}, Load: &ProviderLoadInfo{LoadRate: 10}}
		require.False(t, SameProviderWithLoadGroup(a, b))
	})

	t.Run("both nil LastUsedAt", func(t *testing.T) {
		a := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: nil}, Load: &ProviderLoadInfo{LoadRate: 0}}
		b := BasicCandidate{Provider: &BasicProvider{Priority: 1, LastUsedAt: nil}, Load: &ProviderLoadInfo{LoadRate: 0}}
		require.True(t, SameProviderWithLoadGroup(a, b))
	})
}

// ============ SameProviderGroup 测试 ============

func TestSameProviderGroup(t *testing.T) {
	now := time.Now()

	t.Run("same group", func(t *testing.T) {
		a := &BasicProvider{Priority: 1, LastUsedAt: nil}
		b := &BasicProvider{Priority: 1, LastUsedAt: nil}
		require.True(t, SameProviderGroup(a, b))
	})

	t.Run("different priority", func(t *testing.T) {
		a := &BasicProvider{Priority: 1, LastUsedAt: nil}
		b := &BasicProvider{Priority: 2, LastUsedAt: nil}
		require.False(t, SameProviderGroup(a, b))
	})

	t.Run("different LastUsedAt", func(t *testing.T) {
		later := now.Add(1 * time.Second)
		a := &BasicProvider{Priority: 1, LastUsedAt: &now}
		b := &BasicProvider{Priority: 1, LastUsedAt: &later}
		require.False(t, SameProviderGroup(a, b))
	})
}

// ============ SortProvidersByPriorityAndLastUsed 集成随机化测试 ============

func TestSortProvidersByPriorityAndLastUsed_WithShuffle(t *testing.T) {
	t.Run("same priority and nil LastUsedAt are shuffled", func(t *testing.T) {
		providers := []*BasicProvider{
			{ID: 1, Priority: 1, LastUsedAt: nil},
			{ID: 2, Priority: 1, LastUsedAt: nil},
			{ID: 3, Priority: 1, LastUsedAt: nil},
		}

		seen := map[int64]bool{}
		for i := 0; i < 100; i++ {
			cpy := make([]*BasicProvider, len(providers))
			copy(cpy, providers)
			SortProvidersByPriorityAndLastUsed(cpy, false)
			seen[cpy[0].ID] = true
		}
		require.GreaterOrEqual(t, len(seen), 2, "identical sort keys should produce different orderings after shuffle")
	})

	t.Run("different priorities still sorted correctly", func(t *testing.T) {
		now := time.Now()
		providers := []*BasicProvider{
			{ID: 3, Priority: 3, LastUsedAt: &now},
			{ID: 1, Priority: 1, LastUsedAt: &now},
			{ID: 2, Priority: 2, LastUsedAt: &now},
		}

		SortProvidersByPriorityAndLastUsed(providers, false)
		require.Equal(t, int64(1), providers[0].ID)
		require.Equal(t, int64(2), providers[1].ID)
		require.Equal(t, int64(3), providers[2].ID)
	})
}
