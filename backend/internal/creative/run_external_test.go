package creative_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
)

// TestNormalizeCreativeWorkspaceID 校验工作区 header 的缺失、非法与规范化行为。
func TestNormalizeCreativeWorkspaceID(t *testing.T) {
	_, err := creative.NormalizeCreativeWorkspaceID("")
	require.ErrorIs(t, err, creative.ErrCreativeWorkspaceRequired)
	_, err = creative.NormalizeCreativeWorkspaceID("not-a-uuid")
	require.ErrorIs(t, err, creative.ErrCreativeWorkspaceInvalid)
	normalized, err := creative.NormalizeCreativeWorkspaceID("11111111-1111-4111-8111-111111111111")
	require.NoError(t, err)
	require.Equal(t, testCreativeWorkspaceID, normalized)
	scope, err := creative.NormalizeCreativeRunScope(creative.CreativeRunScope{UserID: 7, WorkspaceID: "11111111-1111-4111-8111-111111111111"})
	require.NoError(t, err)
	require.Equal(t, testCreativeWorkspaceID, scope.WorkspaceID)
	_, err = creative.NormalizeCreativeRunScope(creative.CreativeRunScope{UserID: 0, WorkspaceID: testCreativeWorkspaceID})
	require.ErrorIs(t, err, creative.ErrCreativeRunNotFound)
}

// TestCanTransitionCreativeRun 校验创作台任务状态机。
func TestCanTransitionCreativeRun(t *testing.T) {
	valid := []struct{ from, to string }{
		{creative.CreativeRunStatusQueued, creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusQueued, creative.CreativeRunStatusCancelled},
		// 创建失败回滚路径允许 queued 直接转 failed。
		{creative.CreativeRunStatusQueued, creative.CreativeRunStatusFailed},
		// worker 恢复发现载荷过期时允许 queued 直接转 result_lost。
		{creative.CreativeRunStatusQueued, creative.CreativeRunStatusResultLost},
		{creative.CreativeRunStatusRunning, creative.CreativeRunStatusSucceeded},
		{creative.CreativeRunStatusRunning, creative.CreativeRunStatusFailed},
		{creative.CreativeRunStatusRunning, creative.CreativeRunStatusCancelled},
		{creative.CreativeRunStatusRunning, creative.CreativeRunStatusResultLost},
		// 成功任务的临时输出过期后可降级为 result_lost。
		{creative.CreativeRunStatusSucceeded, creative.CreativeRunStatusResultLost},
	}
	for _, tc := range valid {
		require.True(t, creative.CanTransitionCreativeRun(tc.from, tc.to), "%s -> %s 应当合法", tc.from, tc.to)
	}

	invalid := []struct{ from, to string }{
		{"", creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusQueued, ""},
		{creative.CreativeRunStatusQueued, creative.CreativeRunStatusSucceeded},
		{creative.CreativeRunStatusRunning, creative.CreativeRunStatusQueued},
		{creative.CreativeRunStatusRunning, creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusSucceeded, creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusFailed, creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusCancelled, creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusResultLost, creative.CreativeRunStatusRunning},
		{creative.CreativeRunStatusSucceeded, creative.CreativeRunStatusFailed},
		{creative.CreativeRunStatusFailed, creative.CreativeRunStatusResultLost},
	}
	for _, tc := range invalid {
		require.False(t, creative.CanTransitionCreativeRun(tc.from, tc.to), "%s -> %s 应当非法", tc.from, tc.to)
	}
}

func TestIsTerminalCreativeRunStatus(t *testing.T) {
	for _, status := range []string{creative.CreativeRunStatusSucceeded, creative.CreativeRunStatusFailed, creative.CreativeRunStatusCancelled, creative.CreativeRunStatusResultLost} {
		require.True(t, creative.IsTerminalCreativeRunStatus(status))
	}
	for _, status := range []string{creative.CreativeRunStatusQueued, creative.CreativeRunStatusRunning, ""} {
		require.False(t, creative.IsTerminalCreativeRunStatus(status))
	}
}

func TestIsValidCreativeRunID(t *testing.T) {
	require.True(t, creative.IsValidCreativeRunID("crun_0123456789abcdef"))
	require.False(t, creative.IsValidCreativeRunID("imgbatch_0123"))
	require.False(t, creative.IsValidCreativeRunID("crun_"))
	require.False(t, creative.IsValidCreativeRunID(""))
}

func TestNewCreativeRunID(t *testing.T) {
	runID, err := creative.NewCreativeRunID()
	require.NoError(t, err)
	require.True(t, creative.IsValidCreativeRunID(runID))
	other, err := creative.NewCreativeRunID()
	require.NoError(t, err)
	require.NotEqual(t, runID, other)
}
