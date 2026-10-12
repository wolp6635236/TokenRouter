package scheduler

import (
	"context"
	"time"
)

const (
	SchedulerOutboxEventProviderChanged       = "provider_changed"
	SchedulerOutboxEventProviderGroupsChanged = "provider_groups_changed"
	SchedulerOutboxEventProviderBulkChanged   = "provider_bulk_changed"
	SchedulerOutboxEventProviderLastUsed      = "provider_last_used"
	SchedulerOutboxEventGroupChanged          = "group_changed"
	SchedulerOutboxEventFullRebuild           = "full_rebuild"
)

type SchedulerOutboxEvent struct {
	ID         int64
	EventType  string
	ProviderID *int64
	GroupID    *int64
	Payload    map[string]any
	CreatedAt  time.Time
}

// SchedulerOutboxRepository 提供调度 outbox 的读取接口。
type SchedulerOutboxRepository interface {
	ListAfterAndReleaseDedup(ctx context.Context, afterID int64, limit int) ([]SchedulerOutboxEvent, error)
	// FirstCreatedAtAfter 读取指定水位之后第一条待消费事件的创建时间。
	FirstCreatedAtAfter(ctx context.Context, afterID int64) (time.Time, bool, error)
	MaxID(ctx context.Context) (int64, error)
	DeleteConsumedUpTo(ctx context.Context, watermark int64, limit int) (int64, error)
	TryAcquireCleanupLock(ctx context.Context) (SchedulerOutboxCleanupLease, bool, error)
}

// SchedulerOutboxCleanupLease 持有调度 outbox 清理使用的 PostgreSQL 咨询锁。
type SchedulerOutboxCleanupLease interface {
	Release()
}

// GroupPayload 在分组为空时返回 untyped nil，该值参与持久化去重指纹计算。
func GroupPayload(groupIDs []int64) any {
	if len(groupIDs) == 0 {
		return nil
	}
	return map[string]any{"group_ids": groupIDs}
}
