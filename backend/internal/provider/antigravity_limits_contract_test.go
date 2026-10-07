package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSetModelRateLimitByModelName_UsesOfficialModelID 验证写入端使用官方模型 ID
func TestSetModelRateLimitByModelName_UsesOfficialModelID(t *testing.T) {
	tests := []struct {
		name             string
		modelName        string
		expectedModelKey string
		expectedSuccess  bool
	}{
		{
			name:             "claude-sonnet-4-5 should be stored as-is",
			modelName:        "claude-sonnet-4-5",
			expectedModelKey: "claude-sonnet-4-5",
			expectedSuccess:  true,
		},
		{
			name:             "gemini-3-pro-high should be stored as-is",
			modelName:        "gemini-3-pro-high",
			expectedModelKey: "gemini-3-pro-high",
			expectedSuccess:  true,
		},
		{
			name:             "gemini-3-flash should be stored as-is",
			modelName:        "gemini-3-flash",
			expectedModelKey: "gemini-3-flash",
			expectedSuccess:  true,
		},
		{
			name:             "empty model name should fail",
			modelName:        "",
			expectedModelKey: "",
			expectedSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &antigravityHealthStoreFixture{}
			resetAt := time.Now().Add(30 * time.Second)

			success := SetModelRateLimitByModelName(
				context.Background(),
				repo,
				123, // providerID
				tt.modelName,
				"[test]",
				429,
				resetAt,
				false, // afterSmartRetry
				func(string, ...any) {},
			)

			require.Equal(t, tt.expectedSuccess, success)

			if tt.expectedSuccess {
				require.Len(t, repo.modelRateLimitCalls, 1)
				call := repo.modelRateLimitCalls[0]
				require.Equal(t, int64(123), call.providerID)
				// 检查存储键为官方模型 ID。
				require.Equal(t, tt.expectedModelKey, call.modelKey, "should store official model ID, not scope")
				require.WithinDuration(t, resetAt, call.resetAt, time.Second)
			} else {
				require.Empty(t, repo.modelRateLimitCalls)
			}
		})
	}
}

// TestSetModelRateLimitByModelName_NotConvertToScope 验证不会将模型名转换为 scope
func TestSetModelRateLimitByModelName_NotConvertToScope(t *testing.T) {
	repo := &antigravityHealthStoreFixture{}
	resetAt := time.Now().Add(30 * time.Second)

	// 调用 setModelRateLimitByModelName，传入官方模型 ID
	success := SetModelRateLimitByModelName(
		context.Background(),
		repo,
		456,
		"claude-sonnet-4-5", // 官方模型 ID
		"[test]",
		429,
		resetAt,
		true, // afterSmartRetry
		func(string, ...any) {},
	)

	require.True(t, success)
	require.Len(t, repo.modelRateLimitCalls, 1)

	call := repo.modelRateLimitCalls[0]
	// 检查存储键为 claude-sonnet-4-5。
	require.Equal(t, "claude-sonnet-4-5", call.modelKey, "should NOT convert to scope like claude_sonnet")
	require.NotEqual(t, "claude_sonnet", call.modelKey, "should NOT be scope")
}
