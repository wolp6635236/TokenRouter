package testutil

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

var (
	_ scheduler.SessionLimitCache = StubSessionLimitCache{}
	_ billing.WindowCostCache     = StubSessionLimitCache{}
)

// StubSessionLimitCache 是返回空会话计数和窗口费用的缓存替身。
type StubSessionLimitCache struct{}

// RegisterSession 模拟成功登记会话。
func (c StubSessionLimitCache) RegisterSession(_ context.Context, _ int64, _ string, _ int, _ time.Duration) (bool, error) {
	return true, nil
}

// RefreshSession 模拟成功续期会话登记。
func (c StubSessionLimitCache) RefreshSession(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

// UnregisterSession 模拟成功注销会话。
func (c StubSessionLimitCache) UnregisterSession(_ context.Context, _ int64, _ string) error {
	return nil
}

// GetActiveSessionCount 返回零活跃会话数。
func (c StubSessionLimitCache) GetActiveSessionCount(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

// GetActiveSessionCountBatch 返回空的批量活跃会话计数。
func (c StubSessionLimitCache) GetActiveSessionCountBatch(_ context.Context, _ []int64, _ map[int64]time.Duration) (map[int64]int, error) {
	return nil, nil
}

// IsSessionActive 返回会话未活跃。
func (c StubSessionLimitCache) IsSessionActive(_ context.Context, _ int64, _ string) (bool, error) {
	return false, nil
}

// GetWindowCost 返回窗口费用未命中。
func (c StubSessionLimitCache) GetWindowCost(_ context.Context, _ int64) (float64, bool, error) {
	return 0, false, nil
}

// SetWindowCost 模拟成功写入窗口费用。
func (c StubSessionLimitCache) SetWindowCost(_ context.Context, _ int64, _ float64) error {
	return nil
}

// GetWindowCostBatch 返回空的批量窗口费用。
func (c StubSessionLimitCache) GetWindowCostBatch(_ context.Context, _ []int64) (map[int64]float64, error) {
	return nil, nil
}
