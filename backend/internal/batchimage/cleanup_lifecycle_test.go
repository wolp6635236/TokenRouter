package batchimage_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
)

type cleanupRepo struct {
	batchimage.BatchImageRepository
}

func (*cleanupRepo) ListBatchImageJobsDueForInputCleanup(ctx context.Context, _ time.Time, _ int) ([]*batchimage.BatchImageJob, error) {
	return nil, ctx.Err()
}

func (*cleanupRepo) ListBatchImageJobsDueForOutputCleanup(ctx context.Context, _ time.Time, _ int) ([]*batchimage.BatchImageJob, error) {
	return nil, ctx.Err()
}

func TestCleanupImmediateStop(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	core := &batchimage.Cleanup{Repo: &cleanupRepo{}}
	s := batchimage.NewRuntime("batch image cleanup", true, core.Run)
	s.Start()
	s.Stop()
}
