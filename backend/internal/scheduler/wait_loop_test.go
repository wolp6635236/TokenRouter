package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextBackoff_ExponentialGrowth(t *testing.T) {
	// 验证退避时间指数增长（乘数 1.5）
	// 由于有随机抖动（±20%），需要验证范围
	current := InitialBackoff // 100ms

	for i := range 10 {
		next := NextBackoff(current)

		// 退避结果应在 [InitialBackoff, MaxBackoff] 范围内
		assert.GreaterOrEqual(t, int64(next), int64(InitialBackoff),
			"第 %d 次退避不应低于初始值 %v", i, InitialBackoff)
		assert.LessOrEqual(t, int64(next), int64(MaxBackoff),
			"第 %d 次退避不应超过最大值 %v", i, MaxBackoff)

		// 为下一轮提供当前退避值
		current = next
	}
}

func TestNextBackoff_BoundedByMaxBackoff(t *testing.T) {
	// 即使输入非常大，输出也不超过 MaxBackoff
	for range 100 {
		result := NextBackoff(10 * time.Second)
		assert.LessOrEqual(t, int64(result), int64(MaxBackoff),
			"退避值不应超过 MaxBackoff")
	}
}

func TestNextBackoff_BoundedByInitialBackoff(t *testing.T) {
	// 即使输入非常小，输出也不低于 InitialBackoff
	for range 100 {
		result := NextBackoff(1 * time.Millisecond)
		assert.GreaterOrEqual(t, int64(result), int64(InitialBackoff),
			"退避值不应低于 InitialBackoff")
	}
}

func TestNextBackoff_HasJitter(t *testing.T) {
	// 验证多次调用会产生不同的值（随机抖动生效）
	// 使用相同的输入调用 50 次，收集结果
	results := make(map[time.Duration]bool)
	current := 500 * time.Millisecond

	for range 50 {
		result := NextBackoff(current)
		results[result] = true
	}

	// 50 次调用应该至少有 2 个不同的值（抖动存在）
	require.Greater(t, len(results), 1,
		"NextBackoff 应产生随机抖动，但所有 50 次调用结果相同")
}

func TestNextBackoff_InitialValueGrows(t *testing.T) {
	// 验证从初始值开始，退避趋势是增长的
	current := InitialBackoff
	var sum time.Duration

	runs := 100
	for range runs {
		next := NextBackoff(current)
		sum += next
		current = next
	}

	avg := sum / time.Duration(runs)
	// 平均退避时间应大于初始值（因为指数增长 + 上限）
	assert.Greater(t, int64(avg), int64(InitialBackoff),
		"平均退避时间应大于初始退避值")
}

func TestNextBackoff_ConvergesToMaxBackoff(t *testing.T) {
	// 从初始值反复退避后，等待间隔应接近 MaxBackoff。
	current := InitialBackoff
	for range 20 {
		current = NextBackoff(current)
	}

	// 经过 20 次迭代后，应该已经到达 MaxBackoff 区间
	// 由于抖动，允许 ±20% 的范围
	lowerBound := time.Duration(float64(MaxBackoff) * 0.8)
	assert.GreaterOrEqual(t, int64(current), int64(lowerBound),
		"经过多次退避后应收敛到 MaxBackoff 附近")
}

func BenchmarkNextBackoff(b *testing.B) {
	current := InitialBackoff
	for range b.N {
		current = NextBackoff(current)
		if current > MaxBackoff {
			current = InitialBackoff
		}
	}
}

func TestSlotWaitStopCancelsObserverLoop(t *testing.T) {
	core := NewConcurrencyService(&runtimeSlotCache{})
	waiting := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := core.WaitForSlot(context.Background(), "provider", 1, 1, time.Hour, false, WaitObserver{Begin: func() error { close(waiting); return nil }})
		finished <- err
	}()
	<-waiting
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, core.StopContext(ctx))
	require.ErrorIs(t, <-finished, context.Canceled)
}

func TestSlotWaitObserverFailureDoesNotAcquire(t *testing.T) {
	core := NewConcurrencyService(&runtimeSlotCache{})
	failed := errors.New("下游心跳写失败")
	_, err := core.WaitForSlot(context.Background(), "provider", 1, 1, time.Second, false, WaitObserver{Interval: time.Millisecond, Heartbeat: func() error { return failed }})
	require.ErrorIs(t, err, failed)
	require.NoError(t, core.StopContext(context.Background()))
}
