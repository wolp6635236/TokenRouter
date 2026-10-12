package creative_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
)

func TestCreativeWorkerSuccessPath(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workersuccess01"
	seedCreativeRun(f, runID, true)
	f.exec.onExecute = func(id string) {
		require.Equal(t, creative.PlatformGemini, f.repo.runs[id].Platform)
		require.Equal(t, int64(55), *f.repo.runs[id].ProviderID)
	}
	f.exec.result = &creative.CreativeExecuteResult{
		Outputs:    []creative.CreativeOutput{{Index: 0, Bytes: []byte("img"), Mime: "image/png"}},
		ProviderID: 55,
	}

	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)

	run := f.repo.runs[runID]
	require.Equal(t, creative.CreativeRunStatusSucceeded, run.Status)
	require.NotNil(t, run.ProviderID)
	require.Equal(t, int64(55), *run.ProviderID)
	require.Equal(t, 1, f.repo.setProviderN)
	require.NotNil(t, run.ActualCost)
	// 输出行进入 succeeded，临时输出已保存。
	output := f.repo.outputs[runID][0]
	require.Equal(t, creative.CreativeRunOutputStatusSucceeded, output.Status)
	require.NotNil(t, output.TransientExpiresAt)
	data, err := f.store.LoadOutput(context.Background(), runID, 0)
	require.NoError(t, err)
	require.Equal(t, []byte("img"), data)
	// 计费：预占发生在创建阶段（worker 测试直接种入任务，故为 0），worker 只做捕获。
	require.Equal(t, 0, f.billing.reserveN)
	require.Equal(t, 1, f.billing.captureN)
	require.Equal(t, 0, f.billing.releaseN)
}

// TestCreativeWorkerRecoversPersistedProviderOutput 验证输出元数据已落库但状态写入失败时不重复调用 platform。
func TestCreativeWorkerRecoversPersistedProviderOutput(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workerrecover01"
	seedCreativeRun(f, runID, false)
	run := f.repo.runs[runID]
	run.Status = creative.CreativeRunStatusRunning
	providerID := int64(55)
	run.ProviderID = &providerID
	f.repo.outputs[runID][0].Status = creative.CreativeRunOutputStatusSucceeded
	f.store.outputs[runID+":0"] = []byte("img")

	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, 0, f.exec.calls)
	require.Equal(t, creative.CreativeRunStatusSucceeded, f.repo.runs[runID].Status)
	require.Equal(t, 1, f.billing.captureN)
}

// TestCreativeWorkerUserConcurrencyPending 验证用户槽位未获取时任务保持 queued 并释放 worker。
func TestCreativeWorkerUserConcurrencyPending(t *testing.T) {
	f := newCreativeWorkerFixture()
	seedCreativeRun(f, "crun_worker_user_pending", true)
	userRepo := testassert.MustType[*creativeFakeUserRepo](testassert.MustType[creativeUserReader](f.service.UserRepo).source)
	userRepo.user.Concurrency = 1
	cache := &creativeUserCacheFixture{acquireResult: false}
	f.worker = newCreativeWorkerFixtureForSources(f.queue, f.repo, f.store, f.exec, f.service, f.worker.Options(), scheduler.NewConcurrencyService(cache, scheduler.Diagnostics{
		Logf:  logging.LegacyPrintf,
		Event: logging.Event,
	},
	))

	result, err := f.worker.Process(context.Background(), "crun_worker_user_pending")
	require.NoError(t, err)
	require.False(t, result.Terminal)
	require.Equal(t, creative.DefaultCreativeConcurrencyRequeueDelay, result.RequeueAfter)
	require.Equal(t, creative.CreativeRunStatusQueued, f.repo.runs["crun_worker_user_pending"].Status)
	require.Equal(t, 0, f.exec.calls)
}

// TestCreativeWorkerProviderConcurrencyPending 验证执行器报告提供商槽位不足时不推进任务状态。
func TestCreativeWorkerProviderConcurrencyPending(t *testing.T) {
	f := newCreativeWorkerFixture()
	seedCreativeRun(f, "crun_worker_provider_pending", true)
	f.exec.execPrepareErr = creative.ErrCreativeExecutionPending

	result, err := f.worker.Process(context.Background(), "crun_worker_provider_pending")
	require.NoError(t, err)
	require.False(t, result.Terminal)
	require.Equal(t, creative.DefaultCreativeConcurrencyRequeueDelay, result.RequeueAfter)
	require.Equal(t, creative.CreativeRunStatusQueued, f.repo.runs["crun_worker_provider_pending"].Status)
}

// TestCreativeWorkerRetriesTransientOutputFailure 检查保存失败重试时的输出元数据、费用和供应商调用次数。
func TestCreativeWorkerRetriesTransientOutputFailure(t *testing.T) {
	f := newCreativeWorkerFixture()
	id := "crun_workeroutputretry1"
	seedCreativeRun(f, id, true)
	f.store.saveOutputErr = errors.New("redis unavailable")
	f.exec.result = &creative.CreativeExecuteResult{Outputs: []creative.CreativeOutput{{Index: 0, Bytes: []byte("img"), Mime: "image/png"}}, ProviderID: 55}
	result, err := f.worker.Process(context.Background(), id)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, creative.CreativeRunStatusResultLost, f.repo.runs[id].Status)
	require.NotNil(t, f.repo.runs[id].ProviderResultRecordedAt)
	require.Equal(t, 1, f.billing.captureN)
	require.Zero(t, f.billing.releaseN)
	f.store.saveOutputErr = nil
	result, err = f.worker.Process(context.Background(), id)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, 1, f.exec.calls)
	require.Equal(t, 1, f.billing.captureN)
}

func TestCreativeWorkerRetryableError(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workerretry0001"
	seedCreativeRun(f, runID, true)
	f.exec.err = creative.CreativeHTTPStatusError(503, "upstream busy")

	// 第一次与第二次处理：attempt 递增并重排。
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := f.worker.Process(context.Background(), runID)
		require.NoError(t, err)
		require.False(t, result.Terminal)
		require.Greater(t, result.RequeueAfter, time.Duration(0))
		require.Equal(t, attempt, f.repo.runs[runID].AttemptCount)
	}
	require.Equal(t, 2, f.exec.calls)

	// 第三次：attempt 达到上限后，FailRun 出队并释放预占。
	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	run := f.repo.runs[runID]
	require.Equal(t, creative.CreativeRunStatusFailed, run.Status)
	require.NotNil(t, run.ErrorCode)
	require.Equal(t, 3, run.AttemptCount)
	require.Equal(t, 1, f.billing.releaseN)
}

// TestCreativeWorkerImageResultFailure 检查结果读取失败后的任务终态和上游调用次数。
func TestCreativeWorkerImageResultFailure(t *testing.T) {
	for _, code := range []string{"IMAGE_DOWNLOAD_FAILED", "INVALID_IMAGE_RESPONSE"} {
		t.Run(code, func(t *testing.T) {
			f := newCreativeWorkerFixture()
			runID := "crun_image_result_failure"
			seedCreativeRun(f, runID, true)
			f.exec.err = creative.CreativeImageResultError(code, "creative image result failed")
			for range 2 {
				result, err := f.worker.Process(context.Background(), runID)
				require.NoError(t, err)
				require.True(t, result.Terminal)
				require.Zero(t, result.RequeueAfter)
			}
			run := f.repo.runs[runID]
			require.Equal(t, creative.CreativeRunStatusFailed, run.Status)
			require.Equal(t, code, *run.ErrorCode)
			require.Equal(t, 1, f.exec.calls)
			require.Zero(t, run.AttemptCount)
			require.Equal(t, 1, f.billing.releaseN)
		})
	}
}

func TestCreativeWorkerNonRetryableError(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workernonretry01"
	seedCreativeRun(f, runID, true)
	f.exec.err = creative.CreativeNonRetryableError("grok platform does not support creative operation edit")

	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, 1, f.exec.calls)
	// 不可重试错误不消耗 attempt，直接失败并释放。
	run := f.repo.runs[runID]
	require.Equal(t, creative.CreativeRunStatusFailed, run.Status)
	require.Equal(t, 0, run.AttemptCount)
	require.Equal(t, 1, f.billing.releaseN)
}

// TestCreativeWorkerCancelRace 校验执行期间进入 cancelled：仍捕获计费，但保留该终态。
func TestCreativeWorkerCancelRace(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workercancel001"
	seedCreativeRun(f, runID, true)
	f.exec.result = &creative.CreativeExecuteResult{
		Outputs:    []creative.CreativeOutput{{Index: 0, Bytes: []byte("img"), Mime: "image/png"}},
		ProviderID: 55,
	}
	// 模拟执行期间进入 cancelled：execute 返回前把任务置为 cancelled。
	f.exec.onExecute = func(runID string) {
		run := f.repo.runs[runID]
		run.Status = creative.CreativeRunStatusCancelled
	}

	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)

	run := f.repo.runs[runID]
	require.Equal(t, creative.CreativeRunStatusCancelled, run.Status)
	// platform 已确认成功：完成捕获与用量记录，任务状态保持 cancelled。
	require.Equal(t, 1, f.billing.captureN)
	require.Equal(t, 0, f.billing.releaseN)
	require.NotNil(t, run.ActualCost)
	output := f.repo.outputs[runID][0]
	require.Equal(t, creative.CreativeRunOutputStatusSucceeded, output.Status)
}

// TestCreativeWorkerPayloadLost 校验载荷过期：result_lost 且未执行的预占被释放。
func TestCreativeWorkerPayloadLost(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workerlost00001"
	seedCreativeRun(f, runID, false)

	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, 0, f.exec.calls)

	run := f.repo.runs[runID]
	require.Equal(t, creative.CreativeRunStatusResultLost, run.Status)
	// platform 未执行：释放预占。
	require.Equal(t, 1, f.billing.releaseN)
	require.Equal(t, 0, f.billing.captureN)
}

// TestCreativeWorkerCancelBeforeExecute 校验执行前发现已 cancelled：不调用上游，直接清理出队。
func TestCreativeWorkerCancelBeforeExecute(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workerprecancel1"
	seedCreativeRun(f, runID, true)
	f.repo.runs[runID].Status = creative.CreativeRunStatusCancelled

	result, err := f.worker.Process(context.Background(), runID)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, 0, f.exec.calls)
	// 任务在入队前已释放预占，worker 执行幂等清理。
	require.Equal(t, 0, f.billing.releaseN)
}

// TestCreativeWorkerRunOnceAck 检查 RunOnce 预留任务、加锁、处理和确认的流程。
func TestCreativeWorkerRunOnceAck(t *testing.T) {
	f := newCreativeWorkerFixture()
	runID := "crun_workerrunonce001"
	seedCreativeRun(f, runID, true)
	f.queue.reserveBatch = []string{runID}
	f.exec.result = &creative.CreativeExecuteResult{
		Outputs:    []creative.CreativeOutput{{Index: 0, Bytes: []byte("img"), Mime: "image/png"}},
		ProviderID: 55,
	}

	require.NoError(t, f.worker.RunOnce(context.Background()))
	require.Equal(t, []string{runID}, f.queue.acked)
	require.Empty(t, f.queue.requeued)
	require.NotNil(t, f.queue.lastLock)
	require.True(t, f.queue.lastLock.released)
	require.Equal(t, creative.CreativeRunStatusSucceeded, f.repo.runs[runID].Status)
}

func TestCreativeOutputFailureMustNotInferAgain(t *testing.T) {
	f := newCreativeWorkerFixture()
	id := "crun_test_output_failure"
	seedCreativeRun(f, id, true)
	f.store.saveOutputErr = errors.New("redis unavailable")
	f.exec.result = &creative.CreativeExecuteResult{Outputs: []creative.CreativeOutput{{Index: 0, Bytes: []byte("img"), Mime: "image/png"}}, ProviderID: 55}
	_, err := f.worker.Process(context.Background(), id)
	require.NoError(t, err)
	f.store.saveOutputErr = nil
	_, err = f.worker.Process(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, 1, f.exec.calls, "platform 已成功，保存输出失败后的恢复不能再次推理")
}
