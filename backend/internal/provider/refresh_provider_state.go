package provider

import (
	"context"
	"errors"
	"sync"
)

// RefreshAttemptGate 在即将交换凭据时检查准入并占用配额。
type RefreshAttemptGate interface {
	Acquire(context.Context) (func(), error)
}

// RefreshProviderState 只持有单轮平台失败状态；限速与并发门仍由整个 worker 共用。
type RefreshProviderState struct {
	rateGate            RefreshAttemptGate
	poolGate            *RefreshConcurrencyGate
	failureThreshold    int
	nonRetryable        func(error) bool
	mu                  sync.Mutex
	consecutiveFailures int
	tripped             bool
}

func NewRefreshProviderState(rate RefreshAttemptGate, pool *RefreshConcurrencyGate, threshold int, nonRetryable func(error) bool) *RefreshProviderState {
	return &RefreshProviderState{rateGate: rate, poolGate: pool, failureThreshold: threshold, nonRetryable: nonRetryable}
}

func (p *RefreshProviderState) IsTripped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tripped
}

func (p *RefreshProviderState) Acquire(ctx context.Context) (func(), error) {
	if p == nil || p.IsTripped() {
		return nil, ErrRefreshSkipped
	}
	release, err := p.poolGate.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if p.IsTripped() {
		release()
		return nil, ErrRefreshSkipped
	}
	return release, nil
}

func (p *RefreshProviderState) AcquireRate(ctx context.Context) (func(), error) {
	if p == nil || p.IsTripped() {
		return nil, ErrRefreshSkipped
	}
	release := func() {}
	if p.rateGate != nil {
		var err error
		release, err = p.rateGate.Acquire(ctx)
		if err != nil {
			return nil, err
		}
	}
	if p.IsTripped() {
		release()
		return nil, ErrRefreshSkipped
	}
	return release, nil
}

func (p *RefreshProviderState) RecordResult(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err == nil {
		p.consecutiveFailures = 0
		return
	}
	if errors.Is(err, ErrRefreshSkipped) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if _, ok := errors.AsType[*RefreshAttemptTimeoutError](err); !ok {
			return
		}
	}
	if _, ok := errors.AsType[*RefreshAttemptTimeoutError](err); ok {
		p.consecutiveFailures++
		if p.consecutiveFailures >= p.failureThreshold {
			p.tripped = true
		}
		return
	}
	if _, ok := errors.AsType[*ProviderConfigurationRefreshError](err); ok {
		p.tripped = true
		return
	}
	if _, ok := errors.AsType[*ProviderCycleContainmentRefreshError](err); ok {
		p.tripped = true
		return
	}
	if _, ok := errors.AsType[*ProviderPermanentRefreshError](err); ok {
		p.consecutiveFailures = 0
		return
	}
	if p.nonRetryable != nil && p.nonRetryable(err) {
		// 永久凭据错误按单个提供商处理，该平台的其他提供商继续刷新。
		p.consecutiveFailures = 0
		return
	}
	p.consecutiveFailures++
	if p.consecutiveFailures >= p.failureThreshold {
		p.tripped = true
	}
}
