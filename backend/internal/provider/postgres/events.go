package postgres

import (
	"context"
	"time"

	postgresinfra "github.com/TokenFlux/TokenRouter/internal/infra/postgres"
)

const (
	ProviderChanged ProviderEvent = iota
	ProviderGroupsChanged
	ProviderLastUsed
	ProviderBulkChanged
)

// ProviderEvent 表达存储写入产生的技术变更，实际 outbox 名称与编码由装配提供。
type ProviderEvent uint8

// ProviderEvents 提供 outbox 编码、事件写入和提交后的缓存发布。
type ProviderEvents interface {
	Name(ProviderEvent) string
	Write(context.Context, postgresinfra.Executor, ProviderEvent, *int64, *int64, any) error
	GroupPayload([]int64) any
	SyncOne(context.Context, int64)
	SyncMany(context.Context, []int64)
	Drop(context.Context, int64)
}

func (r *ProviderStore) enqueue(ctx context.Context, exec postgresinfra.Executor, event ProviderEvent, providerID, groupID *int64, payload any) error {
	if r.options.Events == nil {
		return nil
	}
	return r.options.Events.Write(ctx, exec, event, providerID, groupID, payload)
}

func (r *ProviderStore) groupPayload(ids []int64) any {
	if r.options.Events == nil {
		return nil
	}
	return r.options.Events.GroupPayload(ids)
}

func (r *ProviderStore) eventName(event ProviderEvent) string { return r.options.Events.Name(event) }

func (r *ProviderStore) afterChanges(ctx context.Context, ids []int64) {
	if r.options.Events != nil {
		r.options.Events.SyncMany(ctx, ids)
	}
}

func (r *ProviderStore) dropSnapshot(ctx context.Context, id int64) {
	if r.options.Events != nil {
		r.options.Events.Drop(ctx, id)
	}
}

// afterChangeDetached 在请求取消后仍以短超时传播最新提供商快照。
func (r *ProviderStore) afterChangeDetached(ctx context.Context, providerID int64) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	propagationCtx, cancel := context.WithTimeout(base, 2*time.Second)
	defer cancel()
	r.afterChange(propagationCtx, providerID)
}
