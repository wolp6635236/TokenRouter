package rediscache

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheUpdateLastUsedUsesSideKeyWithoutRewritingPayloads(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	bucket := scheduler.SchedulerBucket{
		GroupID:  9,
		Platform: capability.PlatformGrok,
		Mode:     scheduler.SchedulerModeSingle,
	}
	initial := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	provider := providercore.Record{
		ID:          9201,
		Name:        "grok-large-oauth",
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		LastUsedAt:  &initial,
		Credentials: map[string]any{
			"access_token":  strings.Repeat("a", 4096),
			"refresh_token": strings.Repeat("r", 4096),
		},
		Extra: map[string]any{"large": strings.Repeat("x", 4096)},
	}
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []providercore.Record{provider}))

	id := strconv.FormatInt(provider.ID, 10)
	fullBefore, err := cache.rdb.Get(ctx, schedulerProviderKey(id)).Bytes()
	require.NoError(t, err)
	metaBefore, err := cache.rdb.Get(ctx, schedulerProviderMetaKey(id)).Bytes()
	require.NoError(t, err)

	latest := initial.Add(37 * time.Second)
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{provider.ID: latest}))

	fullAfter, err := cache.rdb.Get(ctx, schedulerProviderKey(id)).Bytes()
	require.NoError(t, err)
	metaAfter, err := cache.rdb.Get(ctx, schedulerProviderMetaKey(id)).Bytes()
	require.NoError(t, err)
	require.Equal(t, fullBefore, fullAfter)
	require.Equal(t, metaBefore, metaAfter)
	require.Equal(t, strconv.FormatInt(latest.UnixMilli(), 10), cache.rdb.Get(ctx, schedulerLastUsedKey(id)).Val())

	cached, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.NotNil(t, cached.LastUsedAt)
	require.Equal(t, latest, *cached.LastUsedAt)

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)
	require.NotNil(t, snapshot[0].LastUsedAt)
	require.Equal(t, latest, *snapshot[0].LastUsedAt)
}

func TestSchedulerCacheLastUsedSideKeyIsMonotonicAndRequiresProvider(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	provider := providercore.Record{ID: 9202, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}
	require.NoError(t, cache.SetProvider(ctx, &provider))

	newer := time.Now().UTC().Truncate(time.Millisecond)
	older := newer.Add(-time.Minute)
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{provider.ID: newer}))
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{provider.ID: older}))

	id := strconv.FormatInt(provider.ID, 10)
	require.Equal(t, strconv.FormatInt(newer.UnixMilli(), 10), cache.rdb.Get(ctx, schedulerLastUsedKey(id)).Val())
	cached, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Equal(t, newer, *cached.LastUsedAt)

	const missingID int64 = 9299
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{missingID: newer}))
	_, err = cache.rdb.Get(ctx, schedulerLastUsedKey(strconv.FormatInt(missingID, 10))).Result()
	require.ErrorIs(t, err, redis.Nil)

	require.NoError(t, cache.DeleteProvider(ctx, provider.ID))
	_, err = cache.rdb.Get(ctx, schedulerLastUsedKey(id)).Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestSchedulerCacheLastUsedSideKeyFallsBackToNewerEmbeddedValue(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	embedded := time.Now().UTC().Truncate(time.Millisecond)
	provider := providercore.Record{
		ID:         9203,
		Platform:   capability.PlatformGrok,
		Type:       capability.ProviderTypeOAuth,
		LastUsedAt: &embedded,
	}
	require.NoError(t, cache.SetProvider(ctx, &provider))

	id := strconv.FormatInt(provider.ID, 10)
	require.NoError(t, cache.rdb.Set(ctx, schedulerLastUsedKey(id), embedded.Add(-time.Hour).UnixMilli(), 0).Err())
	cached, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Equal(t, embedded, *cached.LastUsedAt)
}

func TestSchedulerCacheLastUsedSideKeySurvivesStaleProviderAndSnapshotWrites(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	bucket := scheduler.SchedulerBucket{
		GroupID:  10,
		Platform: capability.PlatformGrok,
		Mode:     scheduler.SchedulerModeSingle,
	}
	embedded := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	latest := embedded.Add(30 * time.Second)
	provider := providercore.Record{
		ID:          9204,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Schedulable: true,
		LastUsedAt:  &embedded,
	}
	require.NoError(t, cache.SetProvider(ctx, &provider))
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{provider.ID: latest}))

	require.NoError(t, cache.SetProvider(ctx, &provider))
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []providercore.Record{provider}))

	id := strconv.FormatInt(provider.ID, 10)
	require.Equal(t, strconv.FormatInt(latest.UnixMilli(), 10), cache.rdb.Get(ctx, schedulerLastUsedKey(id)).Val())
	cached, err := cache.GetProvider(ctx, provider.ID)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Equal(t, latest, *cached.LastUsedAt)
	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)
	require.Equal(t, latest, *snapshot[0].LastUsedAt)
}

func TestSchedulerCacheUpdateLastUsedChunksLargeBatches(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	total := schedulerLastUsedUpdateChunkSize + 1
	providers := make([]providercore.Record, 0, total)
	updates := make(map[int64]time.Time, total)
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < total; i++ {
		id := int64(9300 + i)
		providers = append(providers, providercore.Record{ID: id, Platform: capability.PlatformGrok})
		updates[id] = base.Add(time.Duration(i) * time.Millisecond)
	}

	written, err := cache.writeProviderIDs(ctx, providers)
	require.NoError(t, err)
	require.Len(t, written, total)
	require.NoError(t, cache.UpdateLastUsed(ctx, updates))

	for id, usedAt := range updates {
		key := schedulerLastUsedKey(strconv.FormatInt(id, 10))
		require.Equal(t, strconv.FormatInt(usedAt.UnixMilli(), 10), cache.rdb.Get(ctx, key).Val())
	}
}
