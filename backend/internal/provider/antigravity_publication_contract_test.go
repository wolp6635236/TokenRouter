package provider

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
	"github.com/stretchr/testify/require"
)

// TestUpdateProviderModelRateLimitInCache_UpdatesExtraAndCallsCache 测试模型限流后更新缓存
func TestUpdateProviderModelRateLimitInCache_UpdatesExtraAndCallsCache(t *testing.T) {
	cache := &antigravityPublicationFixture{}
	svc := &AntigravityHealth{Publish: cache.publish}

	provider := &Record{
		ID:       100,
		Name:     "test-provider",
		Platform: capability.PlatformAntigravity,
	}
	modelKey := "claude-sonnet-4-5"
	resetAt := time.Now().Add(30 * time.Second)

	svc.UpdateProviderModelRateLimitInCache(context.Background(), provider, modelKey, resetAt)

	// 验证 Extra 字段被正确更新
	require.NotNil(t, provider.Extra)
	limits, ok := provider.Extra["model_rate_limits"].(map[string]any)
	require.True(t, ok)
	modelLimit, ok := limits[modelKey].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, modelLimit["rate_limited_at"])
	require.NotEmpty(t, modelLimit["rate_limit_reset_at"])

	// 验证 cache.SetProvider 被调用
	require.Len(t, cache.setProviderCalls, 1)
	require.Equal(t, provider.ID, cache.setProviderCalls[0].ID)
}

// TestUpdateProviderModelRateLimitInCache_NilSchedulerSnapshot 测试 schedulerSnapshot 为 nil 时不 panic
func TestUpdateProviderModelRateLimitInCache_NilSchedulerSnapshot(t *testing.T) {
	svc := &AntigravityHealth{}

	provider := &Record{ID: 1, Name: "test"}

	// 不应 panic
	svc.UpdateProviderModelRateLimitInCache(context.Background(), provider, "claude-sonnet-4-5", time.Now().Add(30*time.Second))

	// Extra 不应被更新（因为函数提前返回）
	require.Nil(t, provider.Extra)
}

// TestUpdateProviderModelRateLimitInCache_PreservesExistingExtra 测试保留已有的 Extra 数据
func TestUpdateProviderModelRateLimitInCache_PreservesExistingExtra(t *testing.T) {
	cache := &antigravityPublicationFixture{}
	svc := &AntigravityHealth{Publish: cache.publish}

	provider := &Record{
		ID:       200,
		Name:     "test-provider",
		Platform: capability.PlatformAntigravity,
		Extra: map[string]any{
			"existing_key": "existing_value",
			"model_rate_limits": map[string]any{
				"gemini-3-flash": map[string]any{
					"rate_limited_at":     "2024-01-01T00:00:00Z",
					"rate_limit_reset_at": "2024-01-01T00:05:00Z",
				},
			},
		},
	}

	svc.UpdateProviderModelRateLimitInCache(context.Background(), provider, "claude-sonnet-4-5", time.Now().Add(30*time.Second))

	// 验证已有数据被保留
	require.Equal(t, "existing_value", provider.Extra["existing_key"])
	limits := testassert.MustType[map[string]any](provider.Extra["model_rate_limits"])
	require.NotNil(t, limits["gemini-3-flash"])
	require.NotNil(t, limits["claude-sonnet-4-5"])
}

// 发布替身记录收到的提供商数据。
type antigravityPublicationFixture struct{ setProviderCalls []*Record }

func (s *antigravityPublicationFixture) publish(_ context.Context, v *Record) error {
	s.setProviderCalls = append(s.setProviderCalls, v)
	return nil
}
