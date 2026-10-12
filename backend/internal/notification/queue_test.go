package notification

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingMailTask struct {
	started chan struct{}
	once    sync.Once
}

func TestMailQueueBoundedDrain(t *testing.T) {
	p := &blockingMailTask{started: make(chan struct{})}
	q := NewEmailQueueService(p, 1)
	require.NoError(t, q.EnqueueVerifyCode("fixture@example.com", "fixture"))
	require.False(t, q.started)
	q.Start()
	q.Start()
	<-p.started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := q.StopContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Error(t, q.EnqueueVerifyCode("fixture@example.com", "fixture"))
	require.ErrorIs(t, q.StopContext(context.Background()), context.DeadlineExceeded)
	q.Start()
	require.True(t, q.stopped)
}

func TestMailQueueStopBeforeStartReportsPending(t *testing.T) {
	queue := NewEmailQueueService(nil, 1)
	require.NoError(t, queue.EnqueueVerifyCode("fixture@example.com", "fixture"))
	require.ErrorContains(t, queue.StopContext(context.Background()), "1 tasks")
	queue.Start()
	require.False(t, queue.started)
}

func (p *blockingMailTask) SendVerifyCode(ctx context.Context, _, _ string, _ ...string) error {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (p *blockingMailTask) SendPasswordResetEmailWithCooldown(ctx context.Context, a, b, c string, d ...string) error {
	return p.SendVerifyCode(ctx, a, b, d...)
}
