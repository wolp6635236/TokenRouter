package testutil

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"

	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// ============================================================
// StubConcurrencyCache — scheduler.ConcurrencyCache 的空实现
// ============================================================

// 编译期接口断言
var _ scheduler.ConcurrencyCache = StubConcurrencyCache{}

// StubConcurrencyCache 是 ConcurrencyCache 的默认空实现，所有方法返回零值。
type StubConcurrencyCache struct{}

func (c StubConcurrencyCache) AcquireProviderSlot(_ context.Context, _ int64, _ int, _ string) (bool, error) {
	return true, nil
}

func (c StubConcurrencyCache) ReleaseProviderSlot(_ context.Context, _ int64, _ string) error {
	return nil
}

func (c StubConcurrencyCache) GetProviderConcurrency(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

func (c StubConcurrencyCache) IncrementProviderWaitCount(_ context.Context, _ int64, _ int) (bool, error) {
	return true, nil
}

func (c StubConcurrencyCache) DecrementProviderWaitCount(_ context.Context, _ int64) error {
	return nil
}

func (c StubConcurrencyCache) GetProviderWaitingCount(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

func (c StubConcurrencyCache) AcquireUserSlot(_ context.Context, _ int64, _ int, _ string) (bool, error) {
	return true, nil
}

func (c StubConcurrencyCache) ReleaseUserSlot(_ context.Context, _ int64, _ string) error {
	return nil
}

func (c StubConcurrencyCache) GetUserConcurrency(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

func (c StubConcurrencyCache) IncrementWaitCount(_ context.Context, _ int64, _ int) (bool, error) {
	return true, nil
}
func (c StubConcurrencyCache) DecrementWaitCount(_ context.Context, _ int64) error { return nil }
func (c StubConcurrencyCache) GetProvidersLoadBatch(_ context.Context, providers []scheduler.ProviderWithConcurrency) (map[int64]*scheduler.ProviderLoadInfo, error) {
	result := make(map[int64]*scheduler.ProviderLoadInfo, len(providers))
	for _, acc := range providers {
		result[acc.ID] = &scheduler.ProviderLoadInfo{ProviderID: acc.ID, LoadRate: 0}
	}
	return result, nil
}

func (c StubConcurrencyCache) GetUsersLoadBatch(_ context.Context, users []scheduler.UserWithConcurrency) (map[int64]*scheduler.UserLoadInfo, error) {
	result := make(map[int64]*scheduler.UserLoadInfo, len(users))
	for _, u := range users {
		result[u.ID] = &scheduler.UserLoadInfo{UserID: u.ID, LoadRate: 0}
	}
	return result, nil
}

func (c StubConcurrencyCache) GetProviderConcurrencyBatch(_ context.Context, providerIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(providerIDs))
	for _, id := range providerIDs {
		result[id] = 0
	}
	return result, nil
}

func (c StubConcurrencyCache) CleanupExpiredProviderSlots(_ context.Context, _ int64) error {
	return nil
}

func (c StubConcurrencyCache) CleanupExpiredProviderSlotKeys(_ context.Context) error {
	return nil
}

func (c StubConcurrencyCache) CleanupStaleProcessSlots(_ context.Context, _ string) error {
	return nil
}

// ============================================================
// StubGatewayCache — session.GatewayCache 的空实现
// ============================================================

var _ session.GatewayCache = StubGatewayCache{}

type StubGatewayCache struct{}

func (c StubGatewayCache) GetSessionProviderID(_ context.Context, _ int64, _ string) (int64, error) {
	return 0, nil
}

func (c StubGatewayCache) SetSessionProviderID(_ context.Context, _ int64, _ string, _ int64, _ time.Duration) error {
	return nil
}

func (c StubGatewayCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (c StubGatewayCache) DeleteSessionProviderID(_ context.Context, _ int64, _ string) error {
	return nil
}

func (c StubGatewayCache) SetSessionOwnerGroupID(_ context.Context, _ int64, _, _ string, _ int64, _ time.Duration) (bool, error) {
	return true, nil
}

func (c StubGatewayCache) GetSessionOwnerGroupID(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}

func (c StubGatewayCache) RefreshSessionOwnerTTL(_ context.Context, _ int64, _, _ string, _ time.Duration) error {
	return nil
}

func (c StubGatewayCache) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

func (c StubGatewayCache) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (c StubGatewayCache) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (c StubGatewayCache) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}

// ============================================================
// StubSessionLimitCache — scheduler.SessionLimitCache 的空实现
// ============================================================

var (
	_ scheduler.SessionLimitCache = StubSessionLimitCache{}
	_ billing.WindowCostCache     = StubSessionLimitCache{}
)

type StubSessionLimitCache struct{}

func (c StubSessionLimitCache) RegisterSession(_ context.Context, _ int64, _ string, _ int, _ time.Duration) (bool, error) {
	return true, nil
}

func (c StubSessionLimitCache) RefreshSession(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (c StubSessionLimitCache) UnregisterSession(_ context.Context, _ int64, _ string) error {
	return nil
}

func (c StubSessionLimitCache) GetActiveSessionCount(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

func (c StubSessionLimitCache) GetActiveSessionCountBatch(_ context.Context, _ []int64, _ map[int64]time.Duration) (map[int64]int, error) {
	return nil, nil
}

func (c StubSessionLimitCache) IsSessionActive(_ context.Context, _ int64, _ string) (bool, error) {
	return false, nil
}

func (c StubSessionLimitCache) GetWindowCost(_ context.Context, _ int64) (float64, bool, error) {
	return 0, false, nil
}

func (c StubSessionLimitCache) SetWindowCost(_ context.Context, _ int64, _ float64) error {
	return nil
}

func (c StubSessionLimitCache) GetWindowCostBatch(_ context.Context, _ []int64) (map[int64]float64, error) {
	return nil, nil
}
