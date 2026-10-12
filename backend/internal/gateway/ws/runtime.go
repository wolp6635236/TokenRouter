package ws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// SettingSource 批量读取持久设置，缺键返回空 map。
type SettingSource interface {
	GetMultiple(context.Context, []string) (map[string]string, error)
}

// Runtime 持有有效参数和刷新任务，发布代次防止旧读取覆盖新保存值。
type Runtime struct {
	defaults   Parameters
	current    atomic.Pointer[Parameters]
	source     SettingSource
	mu         sync.Mutex
	generation uint64
	apply      func(Parameters) error
	report     func(error)
	cancel     context.CancelFunc
	done       chan struct{}
	closed     bool
}

// NewRuntime 创建尚未启动刷新任务的配置读取器。
func NewRuntime(defaults Parameters, source SettingSource, report func(error)) *Runtime {
	r := &Runtime{defaults: defaults, source: source, report: report}
	r.current.Store(&defaults)
	return r
}

// Snapshot 返回按值复制的有效参数。
func (r *Runtime) Snapshot() Parameters {
	if r == nil {
		return DefaultParameters()
	}
	return *r.current.Load()
}

// Defaults 返回部署默认值，供设置合并和恢复默认使用。
func (r *Runtime) Defaults() Parameters { return r.defaults }

// SetApply 在 app 装配时绑定连接池更新操作，池由调用方按需创建。
func (r *Runtime) SetApply(apply func(Parameters) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.apply = apply
}

func (r *Runtime) publishLocked(value Parameters) error {
	if r.closed {
		return errors.New("responses websocket settings are stopped")
	}
	if r.apply != nil {
		if err := r.apply(value); err != nil {
			return err
		}
	}
	r.current.Store(&value)
	return nil
}

// Publish 在持久化成功后发布完整配置，先使尚未结束的旧读取失效。
func (r *Runtime) Publish(raw string) error {
	value, err := ResolveParameters(r.defaults, raw)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generation++
	return r.publishLocked(value)
}

// Refresh 在锁外读取数据库，再核对发布代次。
func (r *Runtime) Refresh(ctx context.Context) error {
	if r.source == nil {
		return nil
	}
	r.mu.Lock()
	generation, closed := r.generation, r.closed
	r.mu.Unlock()
	if closed {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	stored, err := r.source.GetMultiple(ctx, []string{SettingKey})
	if err != nil {
		return err
	}
	value, err := ResolveParameters(r.defaults, stored[SettingKey])
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.generation != generation {
		return nil
	}
	if value == r.Snapshot() {
		return nil
	}
	r.generation++
	return r.publishLocked(value)
}

// Start 每五秒读取一次共享设置，首次读取失败时继续使用部署默认值。
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.closed || r.cancel != nil {
		r.mu.Unlock()
		return nil
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	r.mu.Unlock()
	if err := r.Refresh(ctx); err != nil && r.report != nil {
		r.report(err)
	}
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if err := r.Refresh(workerCtx); err != nil && r.report != nil {
					r.report(err)
				}
			}
		}
	}()
	return nil
}

// Stop 取消刷新并等待退出，发布回调在返回后不会再执行。
func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
