package creative_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	billingcore "github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
)

// parallelCreativeQueue 通过内存通道为并行 worker 提供任务。
type parallelCreativeQueue struct {
	ready chan string
}

// parallelCreativeRunRepo 为并行测试保护 fake 仓储的 map 访问。
type parallelCreativeRunRepo struct {
	*creativeFakeRunRepo
	mu sync.Mutex
}

// parallelCreativeTransient 保护并行结算写入的输出 map。
type parallelCreativeTransient struct {
	*creativeFakeTransient
	mu sync.Mutex
}

// parallelCreativeBilling 保护并行结算更新的 fake 计数器。
type parallelCreativeBilling struct {
	*creativeFakeBillingRepo
	mu sync.Mutex
}

// parallelCreativeUserCache 以原子计数模拟用户并发槽位。
type parallelCreativeUserCache struct {
	creativeUserCacheFixture
	active atomic.Int64
}

// overlappingCreativeExecutor 在两个 platform 调用同时进入时通知测试，然后等待统一放行。
type overlappingCreativeExecutor struct {
	active      atomic.Int64
	overlapped  chan struct{}
	overlapOnce sync.Once
	release     chan struct{}
	releaseOnce sync.Once
}

// TestCreativeWorkerRuntimeWorkerCount 检查 worker 数量默认值和正数热更新。
func TestCreativeWorkerRuntimeWorkerCount(t *testing.T) {
	runtime := newCreativeRuntimeFixture(nil, nil, &config.Config{})
	require.Equal(t, creative.DefaultCreativeWorkerCount, runtime.WorkerCount())

	runtime.SetWorkerCount(3)
	require.Equal(t, 3, runtime.WorkerCount())
	runtime.SetWorkerCount(0)
	require.Equal(t, 3, runtime.WorkerCount())
}

// TestCreativeWorkerRuntimeStartStopAndScale 验证任务 worker 池可启动、缩容并完整停止。
func TestCreativeWorkerRuntimeStartStopAndScale(t *testing.T) {
	fixture := newCreativeWorkerFixture()
	runtime := newCreativeRuntimeFixture(fixture.worker, fixture.service, &config.Config{
		Creative: config.CreativeConfig{QueueEnabled: true},
	})
	runtime.SetWorkerCount(2)
	runtime.Start()
	require.Eventually(t, runtime.Running, time.Second, 10*time.Millisecond)
	require.Equal(t, 2, runtime.WorkerCount())

	runtime.SetWorkerCount(1)
	require.Equal(t, 1, runtime.WorkerCount())
	runtime.Stop()
	require.False(t, runtime.Running())
}

// TestCreativeWorkerRuntimeStatus 检查状态快照的运行状态、工作池规模和忙碌 worker 数量。
func TestCreativeWorkerRuntimeStatus(t *testing.T) {
	stopped := newCreativeRuntimeFixture(nil, nil, &config.Config{
		Creative: config.CreativeConfig{QueueEnabled: true},
	})
	require.Equal(t, creative.CreativeWorkerStatus{}, stopped.Status())

	fixture := newCreativeWorkerFixture()
	seedCreativeRun(fixture, "crun_status_1", true)
	seedCreativeRun(fixture, "crun_status_2", true)
	testassert.MustType[*creativeFakeUserRepo](testassert.MustType[creativeUserReader](fixture.service.UserRepo).source).user.Concurrency = 2

	repo := &parallelCreativeRunRepo{creativeFakeRunRepo: fixture.repo}
	transient := &parallelCreativeTransient{creativeFakeTransient: fixture.store}
	billing := &parallelCreativeBilling{creativeFakeBillingRepo: fixture.billing}
	bindCreativeRepoFixture(fixture.service, repo)
	bindCreativeTransientFixture(fixture.service, transient)
	fixture.service.Results.Funding = creativeFundingProjection(billing)
	queue := &parallelCreativeQueue{ready: make(chan string, 2)}
	executor := &overlappingCreativeExecutor{
		overlapped: make(chan struct{}),
		release:    make(chan struct{}),
	}
	worker := newCreativeWorkerFixtureForSources(queue, repo, transient, executor, fixture.service, fixture.worker.Options(), scheduler.NewConcurrencyService(&parallelCreativeUserCache{}, scheduler.Diagnostics{
		Logf:  logging.LegacyPrintf,
		Event: logging.Event,
	},
	))
	runtime := newCreativeRuntimeFixture(worker, fixture.service, &config.Config{
		Creative: config.CreativeConfig{QueueEnabled: true},
	})
	runtime.SetWorkerCount(2)
	runtime.Start()
	t.Cleanup(func() {
		executor.allow()
		runtime.Stop()
	})

	queue.ready <- "crun_status_1"
	queue.ready <- "crun_status_2"
	select {
	case <-executor.overlapped:
	case <-time.After(2 * time.Second):
		t.Fatal("两个创作台任务未同时进入 platform")
	}
	require.Eventually(t, func() bool {
		status := runtime.Status()
		return status.Running && status.WorkerCount == 2 && status.BusyWorkers == 2
	}, 2*time.Second, 10*time.Millisecond)

	executor.allow()
	runtime.Stop()
	require.Equal(t, creative.CreativeWorkerStatus{}, runtime.Status())
}

// TestCreativeWorkerRuntimeParallelProviderExecution 验证两个 worker 可让同一用户的两个任务并行进入 platform。
func TestCreativeWorkerRuntimeParallelProviderExecution(t *testing.T) {
	fixture := newCreativeWorkerFixture()
	seedCreativeRun(fixture, "crun_parallel_1", true)
	seedCreativeRun(fixture, "crun_parallel_2", true)
	testassert.MustType[*creativeFakeUserRepo](testassert.MustType[creativeUserReader](fixture.service.UserRepo).source).user.Concurrency = 2

	repo := &parallelCreativeRunRepo{creativeFakeRunRepo: fixture.repo}
	transient := &parallelCreativeTransient{creativeFakeTransient: fixture.store}
	billing := &parallelCreativeBilling{creativeFakeBillingRepo: fixture.billing}
	bindCreativeRepoFixture(fixture.service, repo)
	bindCreativeTransientFixture(fixture.service, transient)
	fixture.service.Results.Funding = creativeFundingProjection(billing)
	queue := &parallelCreativeQueue{ready: make(chan string, 2)}
	executor := &overlappingCreativeExecutor{
		overlapped: make(chan struct{}),
		release:    make(chan struct{}),
	}
	concurrency := scheduler.NewConcurrencyService(&parallelCreativeUserCache{}, scheduler.Diagnostics{
		Logf:  logging.LegacyPrintf,
		Event: logging.Event,
	},
	)
	worker := newCreativeWorkerFixtureForSources(queue, repo, transient, executor, fixture.service, fixture.worker.Options(), concurrency)
	runtime := newCreativeRuntimeFixture(worker, fixture.service, &config.Config{Creative: config.CreativeConfig{QueueEnabled: true}})
	runtime.SetWorkerCount(2)
	runtime.Start()
	t.Cleanup(func() {
		executor.allow()
		runtime.Stop()
	})

	queue.ready <- "crun_parallel_1"
	queue.ready <- "crun_parallel_2"
	select {
	case <-executor.overlapped:
	case <-time.After(2 * time.Second):
		t.Fatal("两个创作台任务未同时进入 platform")
	}
	executor.allow()
	// 等待两个并行任务成功后检查停止，停止时的取消行为由专门测试覆盖。
	require.Eventually(t, func() bool {
		first, e1 := repo.GetCreativeRunByRunID(context.Background(), "crun_parallel_1")
		second, e2 := repo.GetCreativeRunByRunID(context.Background(), "crun_parallel_2")
		return e1 == nil && e2 == nil && first.Status == creative.CreativeRunStatusSucceeded && second.Status == creative.CreativeRunStatusSucceeded
	}, 2*time.Second, time.Millisecond)
	runtime.Stop()

	first, err := repo.GetCreativeRunByRunID(context.Background(), "crun_parallel_1")
	require.NoError(t, err)
	second, err := repo.GetCreativeRunByRunID(context.Background(), "crun_parallel_2")
	require.NoError(t, err)
	require.Equal(t, creative.CreativeRunStatusSucceeded, first.Status)
	require.Equal(t, creative.CreativeRunStatusSucceeded, second.Status)
}

// TestStoppedRuntimesRejectStart 检查停止后的工作池拒绝再次启动。
func TestStoppedRuntimesRejectStart(t *testing.T) {
	t.Run("creative", func(t *testing.T) {
		q := &parallelCreativeQueue{ready: make(chan string)}
		w := newCreativeWorkerFixtureForSources(q, nil, nil, nil, nil, creative.CreativeWorkerOptions{})
		r := newCreativeRuntimeFixture(w, nil, &config.Config{Creative: config.CreativeConfig{QueueEnabled: true}})
		r.SetWorkerCount(1)
		r.Stop()
		r.Start()
		defer r.Stop()
		require.False(t, r.Running(), "停止后的创作台 runtime 不应重开")
	})
}

func newCreativeRuntimeFixture(worker *creative.CreativeRunWorker, public *creative.Public, cfg *config.Config) *creative.CreativeWorkerRuntime {
	options := creative.RuntimeOptions{Enabled: cfg != nil && cfg.Creative.QueueEnabled}
	if public != nil && public.Results != nil {
		options.Outbox = public.Results.RunCreativeOutboxReconciler
		options.Transient = public.Results.RunCreativeTransientReconciler
	}
	return creative.NewCreativeWorkerRuntime(worker, options)
}

func bindCreativeRepoFixture(public *creative.Public, repo creative.CreativeRunRepository) {
	public.Repo = repo
	public.Results.Repo = repo
}

func (q *parallelCreativeQueue) Enqueue(_ context.Context, runID string) error {
	q.ready <- runID
	return nil
}

func (q *parallelCreativeQueue) Reserve(ctx context.Context, blockTimeout time.Duration) (creative.ReservedCreativeRun, error) {
	timer := time.NewTimer(blockTimeout)
	defer timer.Stop()
	select {
	case runID := <-q.ready:
		return creative.ReservedCreativeRun{RunID: runID, LeaseToken: "test-lease"}, nil
	case <-ctx.Done():
		return creative.ReservedCreativeRun{}, ctx.Err()
	case <-timer.C:
		return creative.ReservedCreativeRun{}, creative.ErrCreativeQueueEmpty
	}
}

func (q *parallelCreativeQueue) RequeueAfter(context.Context, string, string, time.Duration) error {
	return nil
}

func (q *parallelCreativeQueue) Ack(context.Context, string, string) error { return nil }

func (q *parallelCreativeQueue) Heartbeat(context.Context, string, string) (bool, error) {
	return true, nil
}

func (q *parallelCreativeQueue) MoveDueDelayedToReady(context.Context, int) (int, error) {
	return 0, nil
}

func (q *parallelCreativeQueue) RecoverStaleActive(context.Context, time.Duration, int) (int, error) {
	return 0, nil
}

func (q *parallelCreativeQueue) TryAcquireJobLock(context.Context, string, time.Duration) (creative.CreativeRunJobLock, bool, error) {
	return &creativeFakeJobLock{}, true, nil
}

func (r *parallelCreativeRunRepo) GetCreativeRunByRunID(ctx context.Context, runID string) (*creative.CreativeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, err := r.creativeFakeRunRepo.GetCreativeRunByRunID(ctx, runID)
	if err != nil {
		return nil, err
	}
	return cloneCreativeParallelValue(v), nil
}

func (r *parallelCreativeRunRepo) MarkCreativeRunRunning(ctx context.Context, runID string, providerID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.MarkCreativeRunRunning(ctx, runID, providerID, now)
}

func (r *parallelCreativeRunRepo) SetCreativeRunExecution(ctx context.Context, runID string, providerID int64, platform string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.SetCreativeRunExecution(ctx, runID, providerID, platform, now)
}

func (r *parallelCreativeRunRepo) MarkCreativeRunSucceeded(ctx context.Context, runID string, actualCost float64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.MarkCreativeRunSucceeded(ctx, runID, actualCost, now)
}

func (r *parallelCreativeRunRepo) UpdateCreativeRunOutput(ctx context.Context, runID string, outputIndex int, status, mimeType string, byteSize int64, transientExpiresAt *time.Time, errorCode, errorMessage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.UpdateCreativeRunOutput(ctx, runID, outputIndex, status, mimeType, byteSize, transientExpiresAt, errorCode, errorMessage)
}

func (r *parallelCreativeRunRepo) ListCreativeRunOutputs(ctx context.Context, runID string) ([]*creative.CreativeRunOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, err := r.creativeFakeRunRepo.ListCreativeRunOutputs(ctx, runID)
	if err != nil {
		return nil, err
	}
	return cloneCreativeParallelValue(v), nil
}

func (s *parallelCreativeTransient) SaveOutput(ctx context.Context, runID string, index int, data []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creativeFakeTransient.SaveOutput(ctx, runID, index, data, ttl)
}

func (r *parallelCreativeBilling) Capture(ctx context.Context, cmd *billingcore.TaskFundsCommand) (*billingcore.TaskFundsResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeBillingRepo.Capture(ctx, cmd)
}

func (c *parallelCreativeUserCache) AcquireUserSlot(_ context.Context, _ int64, maxConcurrency int, _ string) (bool, error) {
	for {
		current := c.active.Load()
		if maxConcurrency > 0 && current >= int64(maxConcurrency) {
			return false, nil
		}
		if c.active.CompareAndSwap(current, current+1) {
			return true, nil
		}
	}
}

func (c *parallelCreativeUserCache) ReleaseUserSlot(context.Context, int64, string) error {
	c.active.Add(-1)
	return nil
}

func (e *overlappingCreativeExecutor) Prepare(_ context.Context, run creative.CreativeRun) (*creative.CreativeExecution, error) {
	return &creative.CreativeExecution{
		ProviderID: 55,
		Target: creativeFixtureTarget(func(ctx context.Context, run creative.CreativeRun, payload creative.CreativeRunPayload) (*creative.CreativeExecuteResult, error) {
			return e.Execute(ctx, run, payload, nil)
		}),
		UpstreamModel: run.Model,
		ReleaseFunc:   func() {},
	}, nil
}

func (e *overlappingCreativeExecutor) Execute(ctx context.Context, _ creative.CreativeRun, _ creative.CreativeRunPayload, _ *creative.CreativeExecution) (*creative.CreativeExecuteResult, error) {
	if e.active.Add(1) >= 2 {
		e.overlapOnce.Do(func() { close(e.overlapped) })
	}
	select {
	case <-e.release:
	case <-ctx.Done():
		e.active.Add(-1)
		return nil, ctx.Err()
	}
	e.active.Add(-1)
	return &creative.CreativeExecuteResult{
		Outputs:    []creative.CreativeOutput{{Index: 0, Bytes: []byte("img"), Mime: "image/png"}},
		ProviderID: 55,
	}, nil
}

func (e *overlappingCreativeExecutor) IsRetryable(err error) bool {
	return creative.IsRetryableCreativeError(err)
}

func (e *overlappingCreativeExecutor) allow() {
	e.releaseOnce.Do(func() { close(e.release) })
}

// CreateCreativeRun 持锁创建测试任务。
func (r *parallelCreativeRunRepo) CreateCreativeRun(ctx context.Context, params creative.CreateCreativeRunParams) (*creative.CreativeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.CreateCreativeRun(ctx, params)
}

// GetCreativeRunByRunIDForOwner 持锁读取指定用户和工作区的任务。
func (r *parallelCreativeRunRepo) GetCreativeRunByRunIDForOwner(ctx context.Context, scope creative.CreativeRunScope, runID string) (*creative.CreativeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.GetCreativeRunByRunIDForOwner(ctx, scope, runID)
}

// GetCreativeRunByIdempotencyKey 持锁按幂等键读取任务。
func (r *parallelCreativeRunRepo) GetCreativeRunByIdempotencyKey(ctx context.Context, scope creative.CreativeRunScope, key string) (*creative.CreativeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.GetCreativeRunByIdempotencyKey(ctx, scope, key)
}

// ListCreativeRunsForOwner 持锁列出指定用户的任务。
func (r *parallelCreativeRunRepo) ListCreativeRunsForOwner(ctx context.Context, scope creative.CreativeRunScope, filter creative.CreativeRunFilter) ([]*creative.CreativeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.ListCreativeRunsForOwner(ctx, scope, filter)
}

// TransitionCreativeRunStatus 持锁推进任务状态。
func (r *parallelCreativeRunRepo) TransitionCreativeRunStatus(ctx context.Context, runID, toStatus string, opts creative.CreativeRunTransitionOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.TransitionCreativeRunStatus(ctx, runID, toStatus, opts)
}

// GetCreativeRunOutput 持锁读取任务输出。
func (r *parallelCreativeRunRepo) GetCreativeRunOutput(ctx context.Context, runID string, outputIndex int) (*creative.CreativeRunOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.GetCreativeRunOutput(ctx, runID, outputIndex)
}

// MarkCreativeRunOutputAcked 持锁标记输出已确认。
func (r *parallelCreativeRunRepo) MarkCreativeRunOutputAcked(ctx context.Context, runID string, outputIndex int, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.MarkCreativeRunOutputAcked(ctx, runID, outputIndex, now)
}

// ListCreativeRunsDueForTransientCleanup 持锁列出等待清理临时数据的任务。
func (r *parallelCreativeRunRepo) ListCreativeRunsDueForTransientCleanup(ctx context.Context, cutoff time.Time, limit int) ([]*creative.CreativeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.ListCreativeRunsDueForTransientCleanup(ctx, cutoff, limit)
}

// IncrementCreativeRunAttempt 持锁递增执行尝试次数。
func (r *parallelCreativeRunRepo) IncrementCreativeRunAttempt(ctx context.Context, runID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.IncrementCreativeRunAttempt(ctx, runID)
}

// IncrementCreativeRunSettlementAttempt 持锁递增结算尝试次数。
func (r *parallelCreativeRunRepo) IncrementCreativeRunSettlementAttempt(ctx context.Context, runID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.IncrementCreativeRunSettlementAttempt(ctx, runID)
}

// IncrementCreativeRunReleaseAttempt 持锁递增释放资金尝试次数。
func (r *parallelCreativeRunRepo) IncrementCreativeRunReleaseAttempt(ctx context.Context, runID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.IncrementCreativeRunReleaseAttempt(ctx, runID)
}

// SetCreativeRunProvisioningPhase 持锁写入任务准备阶段。
func (r *parallelCreativeRunRepo) SetCreativeRunProvisioningPhase(ctx context.Context, runID, phase string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.SetCreativeRunProvisioningPhase(ctx, runID, phase)
}

// MarkCreativeRunProviderSucceeded 持锁标记提供商执行成功。
func (r *parallelCreativeRunRepo) MarkCreativeRunProviderSucceeded(ctx context.Context, runID string, providerID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.MarkCreativeRunProviderSucceeded(ctx, runID, providerID, now)
}

// SetCreativeRunReconcileError 持锁写入任务恢复错误和下次重试时间。
func (r *parallelCreativeRunRepo) SetCreativeRunReconcileError(ctx context.Context, runID, message string, next time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.SetCreativeRunReconcileError(ctx, runID, message, next)
}

// cloneCreativeParallelValue 复制查询结果，供调用方在锁外读取。
func cloneCreativeParallelValue[T any](v T) T {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
}

// RecordProviderOutcome 持锁写入提供商输出元数据。
func (r *parallelCreativeRunRepo) RecordProviderOutcome(ctx context.Context, id string, providerID int64, outputs []creative.CreativeRunOutput, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.RecordProviderOutcome(ctx, id, providerID, outputs, now)
}

func (r *parallelCreativeRunRepo) CompleteProviderOutcome(ctx context.Context, id string, cost float64, lost bool, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creativeFakeRunRepo.CompleteProviderOutcome(ctx, id, cost, lost, now)
}

// LoadOutput 与 SaveOutput 使用同一把锁读取输出。
func (s *parallelCreativeTransient) LoadOutput(ctx context.Context, runID string, index int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.creativeFakeTransient.LoadOutput(ctx, runID, index)
	return append([]byte(nil), data...), err
}
