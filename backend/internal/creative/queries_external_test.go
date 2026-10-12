package creative_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
)

func TestCreativeOwnershipEnforced(t *testing.T) {
	svc := newCreativeTestService()
	ctx := context.Background()
	runID := "crun_ownerother00001"
	// 任务属于用户 99，当前用户是 7。
	seedOwnedRun(t, svc, runID, 99, creative.CreativeRunStatusSucceeded)
	store := testassert.MustType[*creativeFakeTransient](svc.TransientStore)
	store.outputs[runID+":0"] = []byte("img")

	_, err := svc.GetRun(ctx, testCreativeScope(7), runID)
	require.ErrorIs(t, err, creative.ErrCreativeRunNotFound)

	_, err = svc.GetOutputContent(ctx, testCreativeScope(7), runID, 0)
	require.ErrorIs(t, err, creative.ErrCreativeRunNotFound)

	err = svc.AckOutput(ctx, testCreativeScope(7), runID, 0)
	require.ErrorIs(t, err, creative.ErrCreativeRunNotFound)

	// 本人访问不受影响。
	got, err := svc.GetRun(ctx, testCreativeScope(99), runID)
	require.NoError(t, err)
	require.Equal(t, runID, got.ID)
}

func TestCreativeGetOutputContentExpiresToResultLost(t *testing.T) {
	svc := newCreativeTestService()
	ctx := context.Background()
	runID := "crun_outputexpired001"
	repo := testassert.MustType[*creativeFakeRunRepo](svc.Repo)
	// succeeded 任务，输出已过期且临时键已不存在。
	past := time.Now().Add(-time.Minute)
	repo.runs[runID] = &creative.CreativeRun{
		RunID:                runID,
		UserID:               7,
		WorkspaceID:          creativeStringValuePtr(testCreativeWorkspaceID),
		GroupID:              12,
		APIKeyID:             900,
		Model:                "gemini-3.1-flash-image",
		Operation:            creative.CreativeOperationGenerate,
		RequestedOutputCount: 1,
		Status:               creative.CreativeRunStatusSucceeded,
		EstimatedCost:        0.02,
	}
	repo.outputs[runID] = []*creative.CreativeRunOutput{
		{RunID: runID, OutputIndex: 0, Status: creative.CreativeRunOutputStatusSucceeded, MimeType: creativeStringValuePtr("image/png"), TransientExpiresAt: &past},
	}

	_, err := svc.GetOutputContent(ctx, testCreativeScope(7), runID, 0)
	require.ErrorIs(t, err, creative.ErrCreativeOutputExpired)
	// 输出过期的任务进入 result_lost。
	require.Equal(t, creative.CreativeRunStatusResultLost, repo.runs[runID].Status)
}

func TestCreativeGetOutputContentMissingTransientToResultLost(t *testing.T) {
	svc := newCreativeTestService()
	ctx := context.Background()
	runID := "crun_outputmissing001"
	repo := testassert.MustType[*creativeFakeRunRepo](svc.Repo)
	future := time.Now().Add(30 * time.Minute)
	repo.runs[runID] = &creative.CreativeRun{
		RunID:                runID,
		UserID:               7,
		WorkspaceID:          creativeStringValuePtr(testCreativeWorkspaceID),
		GroupID:              12,
		APIKeyID:             900,
		Model:                "gemini-3.1-flash-image",
		Operation:            creative.CreativeOperationGenerate,
		RequestedOutputCount: 1,
		Status:               creative.CreativeRunStatusSucceeded,
		EstimatedCost:        0.02,
	}
	repo.outputs[runID] = []*creative.CreativeRunOutput{
		{RunID: runID, OutputIndex: 0, Status: creative.CreativeRunOutputStatusSucceeded, MimeType: creativeStringValuePtr("image/png"), TransientExpiresAt: &future},
	}
	// 临时存储中的输出字节已丢失或被清理。

	_, err := svc.GetOutputContent(ctx, testCreativeScope(7), runID, 0)
	require.ErrorIs(t, err, creative.ErrCreativeResultLost)
	require.Equal(t, creative.CreativeRunStatusResultLost, repo.runs[runID].Status)
}

func TestCreativeGetOutputContentSuccess(t *testing.T) {
	svc := newCreativeTestService()
	ctx := context.Background()
	runID := "crun_outputok000000001"
	seedOwnedRun(t, svc, runID, 7, creative.CreativeRunStatusSucceeded)
	store := testassert.MustType[*creativeFakeTransient](svc.TransientStore)
	store.outputs[runID+":0"] = []byte("png-bytes")

	content, err := svc.GetOutputContent(ctx, testCreativeScope(7), runID, 0)
	require.NoError(t, err)
	require.Equal(t, []byte("png-bytes"), content.Content)
	require.Equal(t, "image/png", content.ContentType)
	// 成功读取不得误降级。
	require.Equal(t, creative.CreativeRunStatusSucceeded, testassert.MustType[*creativeFakeRunRepo](svc.Repo).runs[runID].Status)
}

// TestCreativeListRunsIncludesOutputs 校验历史列表携带输出元数据：
// 前端历史组件依赖 outputs 关联本地素材与缺失占位，列表不能只返回任务壳。
func TestCreativeListRunsIncludesOutputs(t *testing.T) {
	svc := newCreativeTestService()
	ctx := context.Background()
	runID := "crun_listoutputs001"
	seedOwnedRun(t, svc, runID, 7, creative.CreativeRunStatusSucceeded)

	got, err := svc.ListRuns(ctx, testCreativeScope(7), creative.CreativeRunFilter{Limit: 20})
	require.NoError(t, err)
	require.Len(t, got.Data, 1)
	require.Len(t, got.Data[0].Outputs, 1)
	require.Equal(t, 0, got.Data[0].Outputs[0].Index)
	require.Equal(t, "image/png", got.Data[0].Outputs[0].MimeType)
}

// seedOwnedRun 种入一个属于指定用户的 succeeded 任务（含一张 succeeded 输出）。
func seedOwnedRun(t *testing.T, svc *creative.Public, runID string, userID int64, status string) {
	t.Helper()
	repo := testassert.MustType[*creativeFakeRunRepo](svc.Repo)
	expires := time.Now().Add(30 * time.Minute)
	repo.runs[runID] = &creative.CreativeRun{
		RunID:                runID,
		UserID:               userID,
		WorkspaceID:          creativeStringValuePtr(testCreativeWorkspaceID),
		GroupID:              12,
		APIKeyID:             900,
		Model:                "gemini-3.1-flash-image",
		Operation:            creative.CreativeOperationGenerate,
		RequestedOutputCount: 1,
		Status:               status,
		EstimatedCost:        0.02,
	}
	repo.outputs[runID] = []*creative.CreativeRunOutput{
		{
			RunID:              runID,
			OutputIndex:        0,
			Status:             creative.CreativeRunOutputStatusSucceeded,
			MimeType:           creativeStringValuePtr("image/png"),
			ByteSize:           creativeInt64ValuePtr(4),
			TransientExpiresAt: &expires,
		},
	}
}

func creativeInt64ValuePtr(v int64) *int64 { return &v }
