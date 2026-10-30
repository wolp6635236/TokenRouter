package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/TokenFlux/TokenRouter/internal/config"
)

// batch_image_runtime_scenario_test.go verifies automatic worker and cleanup
// construction across batch_image_worker.go and tasks.go. An observed idle queue
// and observed repository scans establish that recovery and cleanup actually
// run with zero-value application settings before their shutdown is joined.

// batchRuntimeRepository observes recovery and cleanup scans over an empty store.
type batchRuntimeRepository struct {
	batchimage.BatchImageRepository              // unused task persistence methods
	recovery                        atomic.Int64 // stale submission scan count
	inputs                          atomic.Int64 // expired input scan count
	outputs                         atomic.Int64 // expired output scan count
}

// batchRuntimeQueue observes startup while keeping every worker reservation idle.
type batchRuntimeQueue struct {
	batchimage.BatchImageQueue              // unused operations for queued jobs
	reservations               atomic.Int64 // started queue reservation count
}

// Reserve records one worker reservation and waits for runtime cancellation.
// No task is returned, so the application test can observe startup and shutdown
// without invoking provider processing or any persistent funding operation.
func (q *batchRuntimeQueue) Reserve(ctx context.Context, _ time.Duration) (batchimage.ReservedBatchImageJob, error) {
	q.reservations.Add(1)
	// Hold the idle reservation until shutdown so the test observes an actual
	// worker call and can verify that cancellation releases its pending operation.
	<-ctx.Done()
	return batchimage.ReservedBatchImageJob{}, ctx.Err()
}

// MoveDueDelayedToReady reports no delayed jobs for this idle runtime fixture.
func (*batchRuntimeQueue) MoveDueDelayedToReady(context.Context, int) (int, error) {
	return 0, nil
}

// RecoverStaleActive reports no active jobs for this idle runtime fixture.
func (*batchRuntimeQueue) RecoverStaleActive(context.Context, time.Duration, int) (int, error) {
	return 0, nil
}

// ListStaleUnsubmittedBatchImageJobs observes funding recovery and returns an empty task list.
func (r *batchRuntimeRepository) ListStaleUnsubmittedBatchImageJobs(context.Context, time.Time, int) ([]*batchimage.BatchImageJob, error) {
	r.recovery.Add(1)
	return nil, nil
}

// ListBatchImageJobsDueForInputCleanup observes input cleanup and returns an empty task list.
func (r *batchRuntimeRepository) ListBatchImageJobsDueForInputCleanup(context.Context, time.Time, int) ([]*batchimage.BatchImageJob, error) {
	r.inputs.Add(1)
	return nil, nil
}

// ListBatchImageJobsDueForOutputCleanup observes output cleanup and returns an empty task list.
func (r *batchRuntimeRepository) ListBatchImageJobsDueForOutputCleanup(context.Context, time.Time, int) ([]*batchimage.BatchImageJob, error) {
	r.outputs.Add(1)
	return nil, nil
}

// TestBatchImageRuntimeStartsWithoutFeatureFlags constructs production worker
// and cleanup runtimes with zero-value settings. Both scan paths must start
// automatically and stop cleanly while an idle queue supplies no task work.
func TestBatchImageRuntimeStartsWithoutFeatureFlags(t *testing.T) {
	cfg := &config.Config{}
	repo := &batchRuntimeRepository{}
	registry := provideBatchRegistry(cfg)
	queue := &batchRuntimeQueue{}
	worker := provideBatchRuntime(nil, repo, nil, queue, nil, nil, nil, nil, cfg, registry)
	cleanup := provideBatchCleanupRuntime(provideBatchCleanup(repo, nil, cfg, registry))
	// Join both loops during cleanup even when an assertion fails, allowing
	// cancellation to release the pending reservation before this fixture is discarded.
	t.Cleanup(worker.Stop)
	t.Cleanup(cleanup.Stop)
	worker.Start()
	cleanup.Start()
	require.Eventually(t, func() bool {
		return worker.Running() && cleanup.Running() && queue.reservations.Load() > 0 &&
			repo.recovery.Load() > 0 && repo.inputs.Load() > 0 && repo.outputs.Load() > 0
	}, time.Second, time.Millisecond)
	require.NoError(t, worker.StopContext(context.Background()))
	require.NoError(t, cleanup.StopContext(context.Background()))
	require.False(t, worker.Running())
	require.False(t, cleanup.Running())
}
