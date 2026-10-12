package testutil

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// 编译期接口断言。
var _ scheduler.ConcurrencyCache = StubConcurrencyCache{}

// StubConcurrencyCache 是返回可用并发槽和空负载的缓存替身。
type StubConcurrencyCache struct{}

// AcquireProviderSlot 模拟成功获取提供商并发槽。
func (c StubConcurrencyCache) AcquireProviderSlot(_ context.Context, _ int64, _ int, _ string) (bool, error) {
	return true, nil
}

// ReleaseProviderSlot 模拟成功释放提供商并发槽。
func (c StubConcurrencyCache) ReleaseProviderSlot(_ context.Context, _ int64, _ string) error {
	return nil
}

// GetProviderConcurrency 返回零提供商并发数。
func (c StubConcurrencyCache) GetProviderConcurrency(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

// IncrementProviderWaitCount 模拟成功进入提供商等待队列。
func (c StubConcurrencyCache) IncrementProviderWaitCount(_ context.Context, _ int64, _ int) (bool, error) {
	return true, nil
}

// DecrementProviderWaitCount 模拟成功离开提供商等待队列。
func (c StubConcurrencyCache) DecrementProviderWaitCount(_ context.Context, _ int64) error {
	return nil
}

// GetProviderWaitingCount 返回零提供商等待数。
func (c StubConcurrencyCache) GetProviderWaitingCount(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

// AcquireUserSlot 模拟成功获取用户并发槽。
func (c StubConcurrencyCache) AcquireUserSlot(_ context.Context, _ int64, _ int, _ string) (bool, error) {
	return true, nil
}

// ReleaseUserSlot 模拟成功释放用户并发槽。
func (c StubConcurrencyCache) ReleaseUserSlot(_ context.Context, _ int64, _ string) error {
	return nil
}

// GetUserConcurrency 返回零用户并发数。
func (c StubConcurrencyCache) GetUserConcurrency(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

// IncrementWaitCount 模拟成功进入用户等待队列。
func (c StubConcurrencyCache) IncrementWaitCount(_ context.Context, _ int64, _ int) (bool, error) {
	return true, nil
}

// DecrementWaitCount 模拟成功离开用户等待队列。
func (c StubConcurrencyCache) DecrementWaitCount(_ context.Context, _ int64) error { return nil }

// GetProvidersLoadBatch 为指定提供商返回零负载。
func (c StubConcurrencyCache) GetProvidersLoadBatch(_ context.Context, providers []scheduler.ProviderWithConcurrency) (map[int64]*scheduler.ProviderLoadInfo, error) {
	result := make(map[int64]*scheduler.ProviderLoadInfo, len(providers))
	for _, acc := range providers {
		result[acc.ID] = &scheduler.ProviderLoadInfo{ProviderID: acc.ID, LoadRate: 0}
	}
	return result, nil
}

// GetUsersLoadBatch 为指定用户返回零负载。
func (c StubConcurrencyCache) GetUsersLoadBatch(_ context.Context, users []scheduler.UserWithConcurrency) (map[int64]*scheduler.UserLoadInfo, error) {
	result := make(map[int64]*scheduler.UserLoadInfo, len(users))
	for _, u := range users {
		result[u.ID] = &scheduler.UserLoadInfo{UserID: u.ID, LoadRate: 0}
	}
	return result, nil
}

// GetProviderConcurrencyBatch 为指定提供商返回零并发数。
func (c StubConcurrencyCache) GetProviderConcurrencyBatch(_ context.Context, providerIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(providerIDs))
	for _, id := range providerIDs {
		result[id] = 0
	}
	return result, nil
}

// CleanupExpiredProviderSlots 模拟成功清理过期提供商并发槽。
func (c StubConcurrencyCache) CleanupExpiredProviderSlots(_ context.Context, _ int64) error {
	return nil
}

// CleanupExpiredProviderSlotKeys 模拟成功清理过期并发槽键。
func (c StubConcurrencyCache) CleanupExpiredProviderSlotKeys(_ context.Context) error {
	return nil
}

// CleanupStaleProcessSlots 模拟成功清理失效进程的并发槽。
func (c StubConcurrencyCache) CleanupStaleProcessSlots(_ context.Context, _ string) error {
	return nil
}
