package requestcontext

import "context"

// abortKey 保存由 WithAbort 返回的函数触发的服务端终止信号。
type abortKey struct{}

// detachedContext 用服务端信号决定取消状态，请求值由 Context 提供。
type detachedContext struct {
	context.Context
	signal context.Context
}

// WithAbort 返回请求 context 和内部终止函数，内部终止会传递给 Detach 的结果。
func WithAbort(ctx context.Context) (context.Context, context.CancelFunc) {
	signal, abort := context.WithCancel(context.Background())
	request, cancel := context.WithCancel(context.WithValue(ctx, abortKey{}, signal))
	return request, func() {
		abort()
		cancel()
	}
}

// AfterAbort 在服务端主动终止时执行回调，返回停止监听的函数。
func AfterAbort(ctx context.Context, callback func()) func() bool {
	signal, ok := ctx.Value(abortKey{}).(context.Context)
	if !ok {
		return func() bool { return true }
	}
	return context.AfterFunc(signal, callback)
}

// Detach 保留请求值和内部终止信号，调用方断连和截止时间与结果分离。
// 返回值直接读取已有信号，生命周期随内部终止信号结束。
func Detach(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	base := context.WithoutCancel(ctx)
	signal, ok := ctx.Value(abortKey{}).(context.Context)
	if !ok {
		return base
	}
	return detachedContext{Context: base, signal: signal}
}

func (c detachedContext) Done() <-chan struct{} { return c.signal.Done() }
func (c detachedContext) Err() error            { return c.signal.Err() }
