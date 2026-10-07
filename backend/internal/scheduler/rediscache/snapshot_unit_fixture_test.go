package rediscache

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/redis/go-redis/v9"
)

// newSchedulerCacheWithChunkSizes 按测试指定的分块大小构造快照缓存，提供商报文由 codec 编解码。
func newSchedulerCacheWithChunkSizes(rdb *redis.Client, read, write int) *schedulerCache {
	return &schedulerCache{NewSnapshotCache(rdb, codec.ProviderCodec{}, SnapshotCacheOptions{MGetChunkSize: read, WriteChunkSize: write})}
}

func (c *schedulerCache) writeProviderIDs(ctx context.Context, values []providercore.Record) ([]int64, error) {
	return c.SnapshotCache.writeProviderIDs(ctx, snapshotRecords(values))
}

func (c *schedulerCache) writeSnapshotVersionAndReturnProviderIDs(ctx context.Context, bucket scheduler.SchedulerBucket, version string, values []providercore.Record) ([]int64, error) {
	return c.SnapshotCache.writeSnapshotVersionAndReturnProviderIDs(ctx, bucket, version, snapshotRecords(values))
}

func marshalSchedulerCacheProvider(value providercore.Record) ([]byte, []byte, error) {
	return (codec.ProviderCodec{}).Encode(codec.WrapRecord(&value))
}
