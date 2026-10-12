package scheduler

import (
	"context"
	"time"
)

// SnapshotMetadata 保存重建和展开事件所需的提供商 ID、名称、平台及所属分组。
type SnapshotMetadata struct {
	ID       int64
	Name     string
	Platform string
	GroupIDs []int64
}

// SnapshotProvider 向调度器提供重建元数据，发布数据由存储适配器保存。
// 同一批次的重建复用该对象。
type SnapshotProvider interface {
	SnapshotMetadata() SnapshotMetadata
}

// SnapshotGroup 保存判断分组启停所需的当前状态。
type SnapshotGroup struct {
	ID       int64
	Hydrated bool
	Name     string
	Status   string
}

// SnapshotProviderSource 按分组和平台查询可调度提供商，返回可发布的快照对象。
type SnapshotProviderSource interface {
	GetByID(context.Context, int64) (SnapshotProvider, error)
	GetByIDs(context.Context, []int64) ([]SnapshotProvider, error)
	ListSchedulableByPlatform(context.Context, string) ([]SnapshotProvider, error)
	ListSchedulableUngroupedByPlatform(context.Context, string) ([]SnapshotProvider, error)
	ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]SnapshotProvider, error)
	ListSchedulableByPlatforms(context.Context, []string) ([]SnapshotProvider, error)
	ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]SnapshotProvider, error)
	ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]SnapshotProvider, error)
}

type SnapshotGroupSource interface {
	GetByID(context.Context, int64) (*SnapshotGroup, error)
	GetByIDLite(context.Context, int64) (*SnapshotGroup, error)
	ListActive(context.Context) ([]SnapshotGroup, error)
}

// SnapshotOptions 由 app 提供。nil 配置使用默认值，DbFallbackEnabled 为 false 则关闭数据库回退。
type SnapshotOptions struct {
	DbFallbackEnabled          bool
	DbFallbackMaxQPS           int
	DbFallbackTimeoutSeconds   int
	OutboxPollIntervalSeconds  int
	FullRebuildIntervalSeconds int
	OutboxLagWarnSeconds       int
	OutboxLagRebuildSeconds    int
	OutboxLagRebuildFailures   int
	OutboxBacklogRebuildRows   int
}

// SnapshotCache 读写调度快照，快照对象由存储适配器解码和发布。
type SnapshotCache interface {
	GetSnapshot(context.Context, SchedulerBucket) ([]SnapshotProvider, bool, error)
	CaptureBucketWriteToken(context.Context, SchedulerBucket) (SchedulerBucketWriteToken, error)
	SetSnapshot(context.Context, SchedulerBucket, SchedulerBucketWriteToken, []SnapshotProvider) error
	RetireBucket(context.Context, SchedulerBucket) error
	ReopenBucket(context.Context, SchedulerBucket) (SchedulerBucketWriteToken, error)
	TryAcquireGroupLifecycleLease(context.Context, int64, time.Duration) (SchedulerGroupLifecycleLease, bool, error)
	ReleaseGroupLifecycleLease(context.Context, SchedulerGroupLifecycleLease) error
	GetProvider(context.Context, int64) (SnapshotProvider, error)
	SetProvider(context.Context, SnapshotProvider) error
	DeleteProvider(context.Context, int64) error
	UpdateLastUsed(context.Context, map[int64]time.Time) error
	AcquireBucketLease(context.Context, SchedulerBucket, time.Duration) (*BucketLease, bool, error)
	ListBuckets(context.Context) ([]SchedulerBucket, error)
	GetOutboxWatermark(context.Context) (int64, error)
	SetOutboxWatermark(context.Context, int64) error
}

// SnapshotBindings 提供未找到提供商或分组时的错误值，以及诊断回调。
type SnapshotBindings struct {
	ProviderNotFound error
	GroupNotFound    error
	Diagnostics      Diagnostics
}

func (g *SnapshotGroup) IsActive() bool { return g != nil && g.Status == "active" }
