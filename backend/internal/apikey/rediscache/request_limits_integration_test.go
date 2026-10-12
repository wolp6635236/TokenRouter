//go:build integration

package rediscache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/testutil/rediscontainer"
)

// TestRequestLimitsAtomicAdmission 用并行请求验证多个客户端共用同一份上限。
func TestRequestLimitsAtomicAdmission(t *testing.T) {
	client := rediscontainer.New(t)
	cache := &ApiKeyCache{rdb: client}
	ctx := context.Background()
	for _, tc := range []struct {
		name             string
		concurrency, rpm int
	}{
		{"concurrency", 3, 0}, {"rpm", 0, 3}, {"both", 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, client.FlushDB(ctx).Err())
			var accepted atomic.Int64
			var wg sync.WaitGroup
			for i := range 24 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, err := cache.ReserveRequest(ctx, 1, fmt.Sprint(i), tc.concurrency, tc.rpm)
					if err == nil {
						accepted.Add(1)
					}
				}(i)
			}
			wg.Wait()
			require.EqualValues(t, 3, accepted.Load())
		})
	}
	// 同一请求的 Redis 命令重放不重复计数，也不推迟 RPM 窗口。
	require.NoError(t, client.FlushDB(ctx).Err())
	for _, limits := range [][2]int{{1, 0}, {0, 1}, {1, 1}} {
		require.NoError(t, client.Del(ctx, requestLimitKeys(9)...).Err())
		_, err := cache.ReserveRequest(ctx, 9, "replayed", limits[0], limits[1])
		require.NoError(t, err)
		score := client.ZScore(ctx, requestLimitKeys(9)[1], "replayed").Val()
		_, err = cache.ReserveRequest(ctx, 9, "replayed", limits[0], limits[1])
		require.NoError(t, err)
		require.Equal(t, score, client.ZScore(ctx, requestLimitKeys(9)[1], "replayed").Val())
		if limits[0] > 0 {
			require.EqualValues(t, 1, client.ZCard(ctx, requestLimitKeys(9)[0]).Val())
		}
		if limits[1] > 0 {
			require.EqualValues(t, 1, client.ZCard(ctx, requestLimitKeys(9)[1]).Val())
		}
	}
	// 并发拒绝不占用 RPM，归还槽后仍能使用下一次额度。
	require.NoError(t, client.FlushDB(ctx).Err())
	_, err := cache.ReserveRequest(ctx, 2, "first", 1, 2)
	require.NoError(t, err)
	_, err = cache.ReserveRequest(ctx, 2, "blocked", 1, 2)
	require.ErrorIs(t, err, apikey.ErrKeyConcurrencyExceeded)
	require.NoError(t, cache.ReleaseRequest(ctx, 2, "first"))
	_, err = cache.ReserveRequest(ctx, 2, "second", 1, 2)
	require.NoError(t, err)
	require.NoError(t, cache.ReleaseRequest(ctx, 2, "second"))
	retry, err := cache.ReserveRequest(ctx, 2, "third", 1, 2)
	require.ErrorIs(t, err, apikey.ErrKeyRPMExceeded)
	require.Positive(t, retry)
	require.LessOrEqual(t, retry, time.Minute)
	require.Zero(t, client.ZCard(ctx, requestLimitKeys(2)[0]).Val())
	// Key ID 独立，复合分组使用同一 ID 时共用额度。
	_, err = cache.ReserveRequest(ctx, 3, "other", 1, 2)
	require.NoError(t, err)
	require.NoError(t, cache.RefreshRequest(ctx, 3, "other"))
	require.NoError(t, cache.ReleaseRequest(ctx, 3, "other"))
	require.ErrorIs(t, cache.RefreshRequest(ctx, 3, "other"), apikey.ErrKeyLimiterUnavailable)
	// 过期的租约和 RPM 记录在下一次准入时清理。
	now := time.Now().Add(-3 * time.Minute).UnixMilli()
	require.NoError(t, client.ZAdd(ctx, requestLimitKeys(4)[0], redis.Z{Score: float64(now), Member: "stale"}).Err())
	require.NoError(t, client.ZAdd(ctx, requestLimitKeys(4)[1], redis.Z{Score: float64(now), Member: "stale"}).Err())
	_, err = cache.ReserveRequest(ctx, 4, "fresh", 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, client.ZCard(ctx, requestLimitKeys(4)[0]).Val())
}
