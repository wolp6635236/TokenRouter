package search

import (
	"context"
	"errors"
	"sync"
	"time"
)

type noSearchExecutor struct{}

type quotaFixture struct {
	mu               sync.Mutex
	used, decrements int64
	uncertain        bool
	releaseDeadline  bool
}

func (noSearchExecutor) Search(context.Context, ProviderConfig, SearchRequest) (*SearchResponse, error) {
	return &SearchResponse{}, nil
}

func (noSearchExecutor) IsProxyError(error) bool { return false }

func (noSearchExecutor) CloseIdle() {}

func (q *quotaFixture) Increment(context.Context, string, time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.used++
	if q.uncertain {
		return 0, errors.New("unknown result")
	}
	return q.used, nil
}

func (q *quotaFixture) Decrement(ctx context.Context, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.decrements++
	_, q.releaseDeadline = ctx.Deadline()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	q.used--
	return nil
}

func (q *quotaFixture) Usage(context.Context, string) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.used, nil
}

func (q *quotaFixture) Reset(context.Context, string) error { return nil }

func (q *quotaFixture) MarkProxy(context.Context, int64, time.Duration) error { return nil }

func (q *quotaFixture) ProxyAvailable(context.Context, int64) bool { return true }
