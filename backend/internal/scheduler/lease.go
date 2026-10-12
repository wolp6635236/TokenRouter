package scheduler

import (
	"context"
	"slices"
	"sync"
)

const (
	ReleaseOnCompletion ReleaseMode = iota
	ReleaseOnCancel
)

// ReleaseMode 由执行入口选择。ReleaseOnCompletion 在上游执行完成后归还容量。
type ReleaseMode uint8

// Lease 持有本次请求实际取得的资源，按取得顺序的逆序清理。
// @project-doc docs/architecture/gateway_request_lifecycle.md#account_selection_and_failover
type Lease struct {
	mu        sync.Mutex
	closed    bool
	resources []func()
	stop      func() bool
	once      sync.Once
}

// AttemptOutcome 中 Served 包括可结算的部分结果，决定空闲会话是否继续保留。
type AttemptOutcome struct {
	Served bool
}

// AttemptLease 管理本次提供商尝试的资源，父请求可在其结束后继续尝试其他提供商。
type AttemptLease struct {
	resources *Lease
	finish    func(AttemptOutcome)
	once      sync.Once
}

// requestLeaseKey 标识 context 中管理当前请求资源的 Lease。
type requestLeaseKey struct{}

// NewLease 先登记资源再关联取消，传入已取消的 context 时也会释放刚取得的资源。
func NewLease(ctx context.Context, mode ReleaseMode, resources ...func()) *Lease {
	l := &Lease{}
	for _, release := range resources {
		l.Own(release)
	}
	if mode == ReleaseOnCancel && ctx != nil {
		stop := context.AfterFunc(ctx, l.Release)
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			stop()
		} else {
			l.stop = stop
			l.mu.Unlock()
		}
	}
	return l
}

// Own 转移一个已取得资源的释放责任；租约已结束时立即归还并返回 false。
func (l *Lease) Own(release func()) bool {
	if release == nil {
		return true
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		release()
		return false
	}
	l.resources = append(l.resources, release)
	l.mu.Unlock()
	return true
}

// Release 在取消、错误补偿和请求完成并发发生时清理一次，重复调用等待这次清理完成。
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		resources, stop := l.resources, l.stop
		l.resources, l.stop = nil, nil
		l.mu.Unlock()
		if stop != nil {
			stop()
		}
		for _, resource := range slices.Backward(resources) {
			resource()
		}
	})
}

// WrapRelease 返回 Lease 的释放函数，取消处理和重复释放由 Lease 管理。
func WrapRelease(ctx context.Context, mode ReleaseMode, release func()) func() {
	if release == nil {
		return nil
	}
	return NewLease(ctx, mode, release).Release
}

// NewAttemptLease 注册到请求拥有者；没有完成结果的异常退出按失败清理。
func NewAttemptLease(parent *Lease, finish func(AttemptOutcome), resources ...func()) *AttemptLease {
	a := &AttemptLease{resources: NewLease(context.Background(), ReleaseOnCompletion, resources...), finish: finish}
	if parent != nil {
		parent.Own(a.Release)
	}
	return a
}

// Finish 先记录会话结果，再释放本次资源。父租约和本方法共用一次清理。
func (a *AttemptLease) Finish(outcome AttemptOutcome) {
	if a == nil {
		return
	}
	a.once.Do(func() {
		defer a.resources.Release()
		if a.finish != nil {
			a.finish(outcome)
		}
	})
}

// Release 将尚未完成的尝试按失败结果结束。
func (a *AttemptLease) Release() { a.Finish(AttemptOutcome{}) }

// WithRequestLease 将已经取得的用户租约传递给后续提供商尝试，执行层继续决定释放时机。
func WithRequestLease(ctx context.Context, lease *Lease) context.Context {
	return context.WithValue(ctx, requestLeaseKey{}, lease)
}

func RequestLease(ctx context.Context) *Lease {
	if ctx == nil {
		return nil
	}
	lease, _ := ctx.Value(requestLeaseKey{}).(*Lease)
	return lease
}

// ownRequestResource 登记请求异常返回时的清理函数，attempt 完成和请求清理共用一次释放。
func ownRequestResource(ctx context.Context, release func()) {
	if owner := RequestLease(ctx); owner != nil {
		owner.Own(release)
	}
}
