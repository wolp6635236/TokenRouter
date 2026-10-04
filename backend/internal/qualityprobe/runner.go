package qualityprobe

import (
	"context"
	"sync"
	"time"
)

// Runner 按固定间隔扫描到期的自动探测。
type Runner struct {
	Engine *Engine
	Tick   time.Duration
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// StartContext 启动后台扫描。
func (r *Runner) StartContext(context.Context) error {
	if r == nil || r.Engine == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return nil
	}
	tick := r.Tick
	if tick <= 0 {
		tick = time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		r.Engine.RunDue(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.Engine.RunDue(ctx)
			}
		}
	}()
	return nil
}

// StopContext 停止后台扫描并等待当前一轮结束。
func (r *Runner) StopContext(context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.done = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}
