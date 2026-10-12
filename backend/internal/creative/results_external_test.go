package creative_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// creativeFakeUsageLogRepo 记录 Create 收到的用量日志。
type creativeFakeUsageLogRepo struct {
	usage.UsageLogRepository
	logs []*usage.UsageLog
}

func TestCreativeSucceedRunIdempotentSettlement(t *testing.T) {
	svc := newCreativeTestService()
	ctx := context.Background()
	usageRepo := &creativeFakeUsageLogRepo{}
	bindCreativeUsageFixture(svc.Results, usageRepo)
	repo := testassert.MustType[*creativeFakeRunRepo](svc.Repo)
	billing := testassert.MustType[*creativeFakeBillingRepo](creativeFixtureBilling(svc))

	runID := "crun_settleidempotent1"
	providerID := int64(55)
	repo.runs[runID] = &creative.CreativeRun{
		RunID:                runID,
		UserID:               7,
		WorkspaceID:          creativeStringValuePtr(testCreativeWorkspaceID),
		GroupID:              12,
		APIKeyID:             900,
		ProviderID:           &providerID,
		Model:                "gemini-3.1-flash-image",
		Operation:            creative.CreativeOperationGenerate,
		RequestedOutputCount: 1,
		Status:               creative.CreativeRunStatusRunning,
		EstimatedCost:        0.02,
		BaseUnitPrice:        0.02,
	}
	repo.outputs[runID] = []*creative.CreativeRunOutput{
		{RunID: runID, OutputIndex: 0, Status: creative.CreativeRunOutputStatusPending},
	}
	results := []creative.ProviderOutput{{Index: 0, Success: true, Bytes: []byte("img"), Mime: "image/png"}}

	first, err := svc.Results.SucceedRun(ctx, runID, providerID, results)
	require.NoError(t, err)
	require.Equal(t, creative.CreativeRunStatusSucceeded, first.Status)

	second, err := svc.Results.SucceedRun(ctx, runID, providerID, results)
	require.NoError(t, err)
	require.Equal(t, creative.CreativeRunStatusSucceeded, second.Status)

	// 捕获与用量日志各只发生一次，且幂等键稳定。
	require.Equal(t, 1, billing.captureN)
	require.Equal(t, []string{"creative_capture:" + runID}, billing.captureIDs)
	require.Len(t, usageRepo.logs, 1)
	require.Len(t, usageRepo.logs[0].RequestID, 36)
	require.Equal(t, "creative_settle:"+runID, usageRepo.logs[0].BillingKey)
	require.Equal(t, "image", stringValue(usageRepo.logs[0].BillingMode))
	require.Equal(t, 1, usageRepo.logs[0].ImageCount)
	// 输出行保持一条 succeeded，重复结算不产生重复输出。
	outputs := repo.outputs[runID]
	require.Len(t, outputs, 1)
	require.Equal(t, creative.CreativeRunOutputStatusSucceeded, outputs[0].Status)
}

// TestCreativeSucceedRunRequiresTransientOutput 校验任务成功前必须先持久化输出。
func TestCreativeSucceedRunRequiresTransientOutput(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*creative.Public)
		bytes     []byte
	}{
		{
			name: "transient store missing",
			configure: func(svc *creative.Public) {
				bindCreativeTransientFixture(svc, nil)
			},
		},
		{
			name: "save output failed",
			configure: func(svc *creative.Public) {
				testassert.MustType[*creativeFakeTransient](svc.TransientStore).saveOutputErr = errors.New("redis unavailable")
			},
		},
		{
			name:  "empty output",
			bytes: []byte{},
			configure: func(svc *creative.Public) {
				// 使用默认 fake transient，校验空字节在写入前被拒绝。
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := newCreativeTestService()
			repo := testassert.MustType[*creativeFakeRunRepo](svc.Repo)
			providerID := int64(55)
			runID := "crun_transientrequired"
			repo.runs[runID] = &creative.CreativeRun{
				RunID:                runID,
				UserID:               7,
				WorkspaceID:          creativeStringValuePtr(testCreativeWorkspaceID),
				GroupID:              12,
				APIKeyID:             900,
				ProviderID:           &providerID,
				Model:                "gemini-3.1-flash-image",
				Operation:            creative.CreativeOperationGenerate,
				RequestedOutputCount: 1,
				Status:               creative.CreativeRunStatusRunning,
				EstimatedCost:        0.02,
				BaseUnitPrice:        0.02,
			}
			repo.outputs[runID] = []*creative.CreativeRunOutput{{
				RunID: runID, OutputIndex: 0, Status: creative.CreativeRunOutputStatusPending,
			}}
			test.configure(svc)

			outputBytes := test.bytes
			if outputBytes == nil {
				outputBytes = []byte("image")
			}
			_, err := svc.Results.SucceedRun(context.Background(), runID, providerID, []creative.ProviderOutput{{
				Index: 0, Success: true, Bytes: outputBytes, Mime: "image/png",
			}})

			billingRepo, ok := creativeFixtureBilling(svc).(*creativeFakeBillingRepo)
			require.True(t, ok)
			if test.name == "empty output" {
				require.ErrorIs(t, err, creative.ErrCreativeTransientFailed)
				require.Equal(t, creative.CreativeRunStatusRunning, repo.runs[runID].Status)
				require.Equal(t, creative.CreativeRunOutputStatusPending, repo.outputs[runID][0].Status)
				require.Zero(t, billingRepo.captureN)
			} else {
				// 已发生服务但无法交付时，记录 result_lost 并计费一次。
				require.NoError(t, err)
				require.Equal(t, creative.CreativeRunStatusResultLost, repo.runs[runID].Status)
				require.Equal(t, 1, billingRepo.captureN)
			}
		})
	}
}

func (r *creativeFakeUsageLogRepo) Create(ctx context.Context, log *usage.UsageLog) (bool, error) {
	r.logs = append(r.logs, log)
	return true, nil
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
