package batchimage_test

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/stretchr/testify/require"
)

type blockedQueue struct {
	blockingBatchImageRuntimeQueue
	entered, cancelled, release chan struct{}
}

func (q *blockedQueue) Reserve(ctx context.Context, _ time.Duration) (batchimage.ReservedBatchImageJob, error) {
	close(q.entered)
	<-ctx.Done()
	close(q.cancelled)
	<-q.release
	return batchimage.ReservedBatchImageJob{}, ctx.Err()
}

func TestRepeatedStopWaitsForSameWork(t *testing.T) {
	q := &blockedQueue{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	r := batchimage.NewWorkerRuntime(batchimage.NewBatchImageWorker(q, &fakeBatchImageProcessor{}, batchimage.BatchImageWorkerOptions{}), nil, true)
	r.Start()
	<-q.entered
	first := make(chan struct{})
	go func() { r.Stop(); close(first) }()
	<-q.cancelled
	second := make(chan struct{})
	go func() { r.Stop(); close(second) }()
	returned := false
	select {
	case <-second:
		returned = true
	case <-time.After(30 * time.Millisecond):
	}
	close(q.release)
	<-first
	<-second
	require.False(t, returned, "第一次停止仍有在途工作时，重复 Stop 不能报告完成")
}

// TestStoppedRuntimesRejectStart 检查批量任务停止后再次启动仍保持停止状态。
func TestStoppedRuntimesRejectStart(t *testing.T) {
	t.Run("batchimage", func(t *testing.T) {
		r := batchimage.NewWorkerRuntime(batchimage.NewBatchImageWorker(&blockingBatchImageRuntimeQueue{}, &fakeBatchImageProcessor{}, batchimage.BatchImageWorkerOptions{}), nil, true)
		r.Stop()
		r.Start()
		defer r.Stop()
		require.False(t, r.Running(), "停止后的批量图片 runtime 不应重开")
	})
}
