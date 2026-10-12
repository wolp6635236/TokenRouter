package scheduler

import (
	"context"
	"time"
)

const (
	WaitNotCounted WaitOwnership = iota
	WaitCounted
	WaitUncertain
)

// WaitCounters 读写用户和提供商的等待计数。
type WaitCounters interface {
	IncrementWaitCount(context.Context, int64, int) (bool, error)
	DecrementWaitCount(context.Context, int64) error
	IncrementProviderWaitCount(context.Context, int64, int) (bool, error)
	DecrementProviderWaitCount(context.Context, int64) error
}

// WaitOwnership 区分未写入、确认取得和写入结果不明；只有确认取得才拥有补偿责任。
type WaitOwnership uint8

// WaitResult 的副本共用一次释放。故障放行时 Ownership 为 WaitUncertain，释放时跳过计数扣减。
type WaitResult struct {
	Allowed   bool
	Ownership WaitOwnership
	resource  *Lease
}

func (r WaitResult) Release() { r.resource.Release() }

// EnterUserWait 登记用户等待计数，缓存故障时放行并标记计数结果不明。
func EnterUserWait(ctx context.Context, cache WaitCounters, id int64, maxWait int, diagnostics Diagnostics) (WaitResult, error) {
	if cache == nil {
		return WaitResult{Allowed: true}, nil
	}
	return enterWait(ctx, id, maxWait, "user", cache.IncrementWaitCount, cache.DecrementWaitCount, diagnostics)
}

// EnterProviderWait 按提供商等待上限登记计数，缓存故障时放行。释放时仅扣减本次确认取得的计数。
func EnterProviderWait(ctx context.Context, cache WaitCounters, id int64, maxWait int, diagnostics Diagnostics) (WaitResult, error) {
	if cache == nil {
		return WaitResult{Allowed: true}, nil
	}
	return enterWait(ctx, id, maxWait, "provider", cache.IncrementProviderWaitCount, cache.DecrementProviderWaitCount, diagnostics)
}

func enterWait(ctx context.Context, id int64, maxWait int, kind string, increment func(context.Context, int64, int) (bool, error), decrement func(context.Context, int64) error, diagnostics Diagnostics) (WaitResult, error) {
	allowed, err := increment(ctx, id, maxWait)
	if err != nil {
		diagnostics.printf("service.concurrency", "Warning: increment wait count failed for %s %d: %v", kind, id, err)
		return WaitResult{Allowed: true, Ownership: WaitUncertain}, nil
	}
	if !allowed {
		return WaitResult{}, nil
	}
	resource := NewLease(context.Background(), ReleaseOnCompletion, func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := decrement(cleanup, id); err != nil {
			diagnostics.printf("service.concurrency", "Warning: decrement wait count failed for %s %d: %v", kind, id, err)
		}
	})
	return WaitResult{Allowed: true, Ownership: WaitCounted, resource: resource}, nil
}
