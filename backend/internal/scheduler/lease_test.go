package scheduler

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLeaseConcurrentReleaseOwnership 检查取消与请求完成竞争时资源归还一次，后取得的提供商资源先于用户资源释放。
func TestLeaseConcurrentReleaseOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var order []string
	l := NewLease(ctx, ReleaseOnCancel, func() { order = append(order, "user") }, func() { order = append(order, "provider") })
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { ; l.Release() })
	}
	cancel()
	wg.Wait()
	require.Equal(t, []string{"provider", "user"}, order)
}

// TestLeaseCompletionIgnoresClientCancellation 检查 Qoder 完成释放模式在客户端取消后持有容量，直到上游结束。
func TestLeaseCompletionIgnoresClientCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var count atomic.Int64
	l := NewLease(ctx, ReleaseOnCompletion, func() { count.Add(1) })
	cancel()
	require.Zero(t, count.Load())
	l.Release()
	l.Release()
	require.EqualValues(t, 1, count.Load())
}

// TestLeaseRejectsLateResource 检查请求结束后收到的资源被立即释放。
func TestLeaseRejectsLateResource(t *testing.T) {
	l := NewLease(context.Background(), ReleaseOnCompletion)
	l.Release()
	var count int
	require.False(t, l.Own(func() { count++ }))
	require.Equal(t, 1, count)
}

// TestAttemptLeaseRetainsCompletionOutcome 验证本次成功会话保留与另一未完成尝试的失败清理互不覆盖。
func TestAttemptLeaseRetainsCompletionOutcome(t *testing.T) {
	parent := NewLease(context.Background(), ReleaseOnCompletion)
	var outcomes []bool
	var released int
	a := NewAttemptLease(parent, func(o AttemptOutcome) { outcomes = append(outcomes, o.Served) }, func() { released++ })
	a.Finish(AttemptOutcome{Served: true})
	NewAttemptLease(parent, func(o AttemptOutcome) { outcomes = append(outcomes, o.Served) }, func() { released++ })
	parent.Release()
	a.Release()
	require.Equal(t, []bool{true, false}, outcomes)
	require.Equal(t, 2, released)
}

// TestWrapReleaseOnDone_NoGoroutineLeak 检查释放后的 goroutine 数量。
func TestWrapReleaseOnDone_NoGoroutineLeak(t *testing.T) {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var releaseCount int32
	release := WrapRelease(ctx, ReleaseOnCancel, func() {
		atomic.AddInt32(&releaseCount, 1)
	})

	release()

	// 给取消回调中的 goroutine 留出退出时间。
	time.Sleep(200 * time.Millisecond)

	if count := atomic.LoadInt32(&releaseCount); count != 1 {
		t.Errorf("expected release count to be 1, got %d", count)
	}

	// 强制 GC，清理已退出的 goroutine
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// 验证 goroutine 数量没有增加（允许±2 的误差，考虑到测试框架本身可能创建的 goroutine）
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("goroutine leak detected: initial=%d, final=%d, leaked=%d",
			initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines)
	}
}

// TestWrapReleaseOnDone_ContextCancellation 检查 context 取消时释放资源。
func TestWrapReleaseOnDone_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var releaseCount int32
	_ = WrapRelease(ctx, ReleaseOnCancel, func() {
		atomic.AddInt32(&releaseCount, 1)
	})

	// 取消 context，应该触发释放
	cancel()

	time.Sleep(100 * time.Millisecond)

	if count := atomic.LoadInt32(&releaseCount); count != 1 {
		t.Errorf("expected release count to be 1, got %d", count)
	}
}

func TestWrapReleaseOnDone_AlreadyCancelledReleasesExactlyOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var releaseCount int32
	release := WrapRelease(ctx, ReleaseOnCancel, func() {
		atomic.AddInt32(&releaseCount, 1)
	})
	release()

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&releaseCount) == 1
	}, time.Second, time.Millisecond)
}

// TestWrapReleaseOnDone_MultipleCallsOnlyReleaseOnce 验证多次调用 release 只释放一次。
func TestWrapReleaseOnDone_MultipleCallsOnlyReleaseOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var releaseCount int32
	release := WrapRelease(ctx, ReleaseOnCancel, func() {
		atomic.AddInt32(&releaseCount, 1)
	})

	release()
	release()
	release()

	time.Sleep(100 * time.Millisecond)

	if count := atomic.LoadInt32(&releaseCount); count != 1 {
		t.Errorf("expected release count to be 1, got %d", count)
	}
}

// TestWrapReleaseOnDone_NilReleaseFunc 检查 nil 释放函数的返回值。
func TestWrapReleaseOnDone_NilReleaseFunc(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := WrapRelease(ctx, ReleaseOnCancel, nil)

	if release != nil {
		t.Error("expected nil release function when releaseFunc is nil")
	}
}

// TestWrapReleaseOnDone_ConcurrentCalls 检查并发调用只释放一次。
func TestWrapReleaseOnDone_ConcurrentCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var releaseCount int32
	release := WrapRelease(ctx, ReleaseOnCancel, func() {
		atomic.AddInt32(&releaseCount, 1)
	})

	const numGoroutines = 10
	for range numGoroutines {
		go release()
	}

	time.Sleep(200 * time.Millisecond)

	if count := atomic.LoadInt32(&releaseCount); count != 1 {
		t.Errorf("expected release count to be 1, got %d", count)
	}
}

// BenchmarkWrapReleaseOnDone 测量租约释放包装函数的开销。
func BenchmarkWrapReleaseOnDone(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.ResetTimer()
	for range b.N {
		release := WrapRelease(ctx, ReleaseOnCancel, func() {})
		release()
	}
}
