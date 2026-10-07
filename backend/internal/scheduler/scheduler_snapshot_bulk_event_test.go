package scheduler

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type bulkEventProviderRepo struct {
	*batchProviderQueryRepo
	providers []SnapshotProvider
}

func newBulkEventProviderRepo(providers ...SnapshotProvider) *bulkEventProviderRepo {
	return &bulkEventProviderRepo{
		batchProviderQueryRepo: newBatchProviderQueryRepo(),
		providers:              providers,
	}
}

func (r *bulkEventProviderRepo) GetByIDs(context.Context, []int64) ([]SnapshotProvider, error) {
	return append([]SnapshotProvider(nil), r.providers...), nil
}

type bulkEventSnapshotCache struct {
	*batchSnapshotCache

	providerMu        sync.Mutex
	setProviderIDs    []int64
	deleteProviderIDs []int64
}

func newBulkEventSnapshotCache() *bulkEventSnapshotCache {
	return &bulkEventSnapshotCache{batchSnapshotCache: newBatchSnapshotCache()}
}

func (c *bulkEventSnapshotCache) SetProvider(_ context.Context, provider SnapshotProvider) error {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()
	c.setProviderIDs = append(c.setProviderIDs, snapshotTestData(provider).ID)
	return nil
}

func (c *bulkEventSnapshotCache) DeleteProvider(_ context.Context, providerID int64) error {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()
	c.deleteProviderIDs = append(c.deleteProviderIDs, providerID)
	return nil
}

func (c *bulkEventSnapshotCache) providerWrites() (set []int64, deleted []int64) {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()
	return append([]int64(nil), c.setProviderIDs...), append([]int64(nil), c.deleteProviderIDs...)
}

func (c *bulkEventSnapshotCache) capturedBuckets() []SchedulerBucket {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]SchedulerBucket(nil), c.captures...)
}

func newBulkEventTestService(cache SnapshotCache, providers SnapshotProviderSource) *SnapshotService {
	return NewSnapshotService(cache, nil, providers, nil, &SnapshotOptions{})
}

func bulkEventPayload(providerIDs []int64, groupIDs []int64) map[string]any {
	providerValues := make([]any, 0, len(providerIDs))
	for _, id := range providerIDs {
		providerValues = append(providerValues, id)
	}
	groupValues := make([]any, 0, len(groupIDs))
	for _, id := range groupIDs {
		groupValues = append(groupValues, id)
	}
	return map[string]any{
		"provider_ids": providerValues,
		"group_ids":    groupValues,
	}
}

func schedulerBucketsForTest(groupIDs []int64, platforms ...string) []SchedulerBucket {
	if !slices.Contains(platforms, "") {
		platforms = append([]string{""}, platforms...)
	}
	buckets := make([]SchedulerBucket, 0, len(groupIDs)*len(platforms)*2)
	for _, platform := range platforms {
		for _, groupID := range groupIDs {
			buckets = append(buckets,
				SchedulerBucket{GroupID: groupID, Platform: platform, Mode: SchedulerModeSingle},
				SchedulerBucket{GroupID: groupID, Platform: platform, Mode: SchedulerModeForced},
			)
		}
	}
	return buckets
}

func TestSchedulerBulkProviderEventScopesOpenAIRebuildToFreshPlatform(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 1, Platform: PlatformOpenAI, GroupIDs: []int64{12}})
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{1}, []int64{11}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{11, 12}, PlatformOpenAI), cache.capturedBuckets())
	set, deleted := cache.providerWrites()
	require.Equal(t, []int64{1}, set)
	require.Empty(t, deleted)
}

// TestSchedulerBulkProviderEventScopesQoderRebuildToFreshPlatform 锁定 fork 的独立 Qoder 调度范围。
func TestSchedulerBulkProviderEventScopesQoderRebuildToFreshPlatform(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 13, Platform: PlatformQoder, GroupIDs: []int64{82}})
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{13}, []int64{81}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{81, 82}, PlatformQoder), cache.capturedBuckets())
}

func TestSchedulerBulkProviderEventRebuildsOpenAIUngroupedBucket(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 6, Platform: PlatformOpenAI})
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{6}, nil), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{0}, PlatformOpenAI), cache.capturedBuckets())
}

func TestSchedulerBulkProviderEventKeepsGroupedAndUngroupedBuckets(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(
		&snapshotTestProvider{ID: 7, Platform: PlatformOpenAI, GroupIDs: []int64{51}},
		&snapshotTestProvider{ID: 8, Platform: PlatformOpenAI},
	)
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{7, 8}, nil), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{0, 51}, PlatformOpenAI), cache.capturedBuckets())
}

func TestSchedulerBulkProviderEventDoesNotCrossCurrentGroupsBetweenPlatforms(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(
		&snapshotTestProvider{ID: 9, Platform: PlatformOpenAI, GroupIDs: []int64{61}},
		&snapshotTestProvider{ID: 10, Platform: PlatformGrok, GroupIDs: []int64{62}},
	)
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{9, 10}, []int64{63}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	want := append(
		schedulerBucketsForTest([]int64{61, 63}, PlatformOpenAI),
		schedulerBucketsForTest([]int64{62, 63}, PlatformGrok)...,
	)
	require.ElementsMatch(t, dedupeBuckets(want), cache.capturedBuckets())
}

func TestSchedulerBulkProviderEventKeepsGroupMembershipIn(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 11, Platform: PlatformOpenAI, GroupIDs: []int64{71}})
	svc := NewSnapshotService(cache, nil, repo, nil, &SnapshotOptions{})

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{11}, []int64{72}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{71, 72}, PlatformOpenAI), cache.capturedBuckets())
}

func TestSchedulerBulkProviderEventRefreshesAntigravityAndSharedPool(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	// Antigravity 状态变化同步到所属分组的共享池。
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 2, Platform: PlatformAntigravity, GroupIDs: []int64{22}})
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{2}, []int64{21}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	require.ElementsMatch(t,
		schedulerBucketsForTest([]int64{21, 22}, PlatformAntigravity),
		cache.capturedBuckets(),
	)
}

func TestSchedulerBulkProviderEventMissingProviderFallsBackToAllPlatforms(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 3, Platform: PlatformOpenAI, GroupIDs: []int64{32}})
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{3, 4}, []int64{31}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	platforms := schedulerSnapshotPlatforms()
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{31, 32}, platforms[:]...), cache.capturedBuckets())
	set, deleted := cache.providerWrites()
	require.Equal(t, []int64{3}, set)
	require.Equal(t, []int64{4}, deleted)
}

func TestSchedulerBulkProviderEventUnknownPlatformFallsBackToAllPlatforms(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventProviderRepo(&snapshotTestProvider{ID: 5, GroupIDs: []int64{42}})
	svc := newBulkEventTestService(cache, repo)

	err := svc.handleBulkProviderEvent(context.Background(), bulkEventPayload([]int64{5}, []int64{41}), make(map[batchSeenKey]struct{}))

	require.NoError(t, err)
	platforms := schedulerSnapshotPlatforms()
	require.ElementsMatch(t, schedulerBucketsForTest([]int64{41, 42}, platforms[:]...), cache.capturedBuckets())
}
