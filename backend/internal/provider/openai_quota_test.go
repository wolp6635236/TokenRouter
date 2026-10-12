package provider

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

type quotaLifecycleClient struct {
	entered chan struct{}
	release chan struct{}
}

func (c *quotaLifecycleClient) GetJSON(ctx context.Context, _ string, _ map[string]string) (map[string]any, error) {
	close(c.entered)
	if c.release != nil {
		<-c.release
	} else {
		<-ctx.Done()
	}
	return nil, ctx.Err()
}

func (*quotaLifecycleClient) GetJSONRaw(context.Context, string, map[string]string) ([]byte, error) {
	return nil, errors.New("unexpected credit detail request")
}

func (*quotaLifecycleClient) PostJSON(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, errors.New("unexpected quota reset")
}

// TestOpenAIQuotaLifecycleCancelsAndWaits 验证停止须取消并等待实际请求；构造不读取提供商，关闭后也不能重新认领。
func TestOpenAIQuotaLifecycleCancelsAndWaits(t *testing.T) {
	client := &quotaLifecycleClient{entered: make(chan struct{})}
	var reads atomic.Int32
	service := NewOpenAIQuotaService(OpenAIQuotaOptions{
		Configured: func() bool { return true },

		Read: func(context.Context, int64) (*Record, error) {
			reads.Add(1)
			return &Record{
				ID:          1,
				Platform:    "openai",
				Type:        "oauth",
				Credentials: map[string]any{"chatgpt_account_id": "fixture-provider", "access_token": "fixture-token"},
			}, nil
		},

		Client: func(context.Context, *Record, string) (OpenAIQuotaClient, error) { return client, nil },
	})
	require.Zero(t, reads.Load())
	finished := make(chan error, 1)
	go func() { _, err := service.QueryUsage(context.Background(), 1); finished <- err }()
	<-client.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, service.StopContext(ctx))
	require.ErrorIs(t, <-finished, context.Canceled)
	require.NoError(t, service.StopContext(ctx))
	_, err := service.QueryUsage(context.Background(), 1)
	require.ErrorIs(t, err, ErrOpenAIQuotaStopped)
	require.EqualValues(t, 1, reads.Load())
}

// TestOpenAIQuotaLifecycleReportsUnfinishedRequest 检查外部查询忽略取消时按停止预算返回，后续停止调用返回同一次未完成结果。
func TestOpenAIQuotaLifecycleReportsUnfinishedRequest(t *testing.T) {
	client := &quotaLifecycleClient{entered: make(chan struct{}), release: make(chan struct{})}
	service := NewOpenAIQuotaService(OpenAIQuotaOptions{
		Configured: func() bool { return true },

		Read: func(context.Context, int64) (*Record, error) {
			return &Record{
				ID:          1,
				Platform:    "openai",
				Type:        "oauth",
				Credentials: map[string]any{"chatgpt_account_id": "fixture-provider", "access_token": "fixture-token"},
			}, nil
		},

		Client: func(context.Context, *Record, string) (OpenAIQuotaClient, error) { return client, nil },
	})
	finished := make(chan error, 1)
	go func() { _, err := service.QueryUsage(context.Background(), 1); finished <- err }()
	<-client.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := service.StopContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, strings.Contains(err.Error(), "OpenAIQuotaService"))
	close(client.release)
	require.ErrorIs(t, <-finished, context.Canceled)
	require.Equal(t, err, service.StopContext(context.Background()))
}

// TestBuildCodexSparkWindowExtraUpdates_ContainsCodexKeys 验证:
//   - 产出包含 codex_5h_used_percent / codex_7d_used_percent
//   - 不含任何 codex_spark_ 前缀的 key（Method Z 前缀已禁止）
//   - 数值正确映射（primary 较短→5h，secondary 较长→7d）
func TestBuildCodexSparkWindowExtraUpdates_ContainsCodexKeys(t *testing.T) {
	now := time.Now().UTC()
	usage := &openai.OpenAIQuotaUsage{
		AdditionalRateLimits: []openai.OpenAIAdditionalRateLimit{
			{
				MeteredFeature: "codex_bengalfox",
				RateLimit: &openai.OpenAIRateLimit{
					PrimaryWindow: &openai.OpenAIRateLimitWindow{
						UsedPercent:        0.42,
						LimitWindowSeconds: 18000, // 300 min = 5 h
						ResetAfterSeconds:  3600,
					},
					SecondaryWindow: &openai.OpenAIRateLimitWindow{
						UsedPercent:        0.15,
						LimitWindowSeconds: 604800, // 7 d
						ResetAfterSeconds:  86400,
					},
				},
			},
		},
	}

	updates := BuildCodexSparkWindowExtraUpdates(usage, now)
	require.NotNil(t, updates, "expected non-nil updates for valid codex_bengalfox entry")

	// 必须含有 codex_5h_* 和 codex_7d_* 键
	require.Contains(t, updates, "codex_5h_used_percent")
	require.Contains(t, updates, "codex_7d_used_percent")

	// 任何键不得含有 codex_spark_ 前缀（Method Z 已禁止）
	for k := range updates {
		require.False(t, strings.Contains(k, "codex_spark_"),
			"unexpected Method-Z prefix in key: %s", k)
	}

	// 数值验证（primary=5h, secondary=7d）
	require.InDelta(t, 0.42, updates["codex_5h_used_percent"], 1e-9)
	require.InDelta(t, 0.15, updates["codex_7d_used_percent"], 1e-9)
}

// TestBuildCodexSparkWindowExtraUpdates_NilUsage 验证 nil usage 返回 nil。
func TestBuildCodexSparkWindowExtraUpdates_NilUsage(t *testing.T) {
	require.Nil(t, BuildCodexSparkWindowExtraUpdates(nil, time.Now()))
}

// TestBuildCodexSparkWindowExtraUpdates_NoBengalfox 验证无 codex_bengalfox 条目时返回 nil。
func TestBuildCodexSparkWindowExtraUpdates_NoBengalfox(t *testing.T) {
	usage := &openai.OpenAIQuotaUsage{
		AdditionalRateLimits: []openai.OpenAIAdditionalRateLimit{
			{MeteredFeature: "other_feature", RateLimit: &openai.OpenAIRateLimit{}},
		},
	}
	require.Nil(t, BuildCodexSparkWindowExtraUpdates(usage, time.Now()))
}
