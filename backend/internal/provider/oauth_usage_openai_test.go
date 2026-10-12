package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// 五秒独立写回不阻塞查询返回，应用停止仍能等待或报告未完成。
type openAIUsageWriteOwner struct {
	OAuthUsageReader
	entered, release, exited chan struct{}
	version                  UsageObservationVersion
	updates                  map[string]any
}

func TestShouldRefreshOpenAICodexSnapshot(t *testing.T) {
	t.Parallel()

	rateLimitedUntil := time.Now().Add(5 * time.Minute)
	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}

	if !ShouldRefreshOpenAICodexSnapshot(&Record{RateLimitResetAt: &rateLimitedUntil}, usage, now) {
		t.Fatal("expected rate-limited provider to force codex snapshot refresh")
	}

	if ShouldRefreshOpenAICodexSnapshot(&Record{}, usage, now) {
		t.Fatal("expected complete non-rate-limited usage to skip codex snapshot refresh")
	}

	if !ShouldRefreshOpenAICodexSnapshot(&Record{}, &UsageInfo{FiveHour: nil, SevenDay: &UsageProgress{}}, now) {
		t.Fatal("expected missing 5h snapshot to require refresh")
	}

	staleAt := now.Add(-(OAuthUsageOpenAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	if !ShouldRefreshOpenAICodexSnapshot(&Record{
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"codex_usage_updated_at":                       staleAt,
		},
	}, usage, now) {
		t.Fatal("expected stale ws snapshot to trigger refresh")
	}
}

// TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2 spark 影子用量走
// QueryUsage(/wham/usage,与 WSv2 无关),staleness 不得被 WSv2 门控,否则首刷后窗口永久冻结。
func TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2(t *testing.T) {
	t.Parallel()

	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}
	staleAt := now.Add(-(OAuthUsageOpenAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	freshAt := now.Add(-time.Minute).Format(time.RFC3339)
	parentID := int64(7001)

	// 影子无 WSv2,但首刷后窗口已存在;过期 codex_usage_updated_at 必须触发再刷新。
	shadowStale := &Record{
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   QuotaDimensionSpark,
		Extra:            map[string]any{"codex_usage_updated_at": staleAt},
	}
	if !ShouldRefreshOpenAICodexSnapshot(shadowStale, usage, now) {
		t.Fatal("expected stale spark shadow (no WSv2) to trigger refresh")
	}

	// 影子快照时间戳在 TTL 内时使用缓存。
	shadowFresh := &Record{
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   QuotaDimensionSpark,
		Extra:            map[string]any{"codex_usage_updated_at": freshAt},
	}
	if ShouldRefreshOpenAICodexSnapshot(shadowFresh, usage, now) {
		t.Fatal("expected fresh spark shadow to skip refresh (TTL not elapsed)")
	}

	// 普通提供商未声明上游 WS 能力时，过期时间戳也不触发这项探测。
	normalNoWS := &Record{
		Credentials: map[string]any{"upstream_protocols": []string{"openai_responses"}},
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Extra:       map[string]any{"codex_usage_updated_at": staleAt},
	}
	if ShouldRefreshOpenAICodexSnapshot(normalNoWS, usage, now) {
		t.Fatal("expected non-WSv2 normal provider to skip codex probe refresh")
	}
}

func (w *openAIUsageWriteOwner) UpdateUsageExtraIfUnchanged(ctx context.Context, v UsageObservationVersion, updates map[string]any) (bool, error) {
	w.version = v
	w.updates = updates
	close(w.entered)
	defer close(w.exited)
	<-w.release
	return false, ctx.Err()
}

func TestOAuthUsageOpenAIWritebackHasBoundedOwner(t *testing.T) {
	store := &openAIUsageWriteOwner{entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
	t.Cleanup(func() { close(store.release); <-store.exited })
	core := NewOAuthUsageService(store, nil, nil, OAuthUsageOptions{})
	observed := &Record{ID: 2, Credentials: map[string]any{"access_token": "observed"}}
	updates := map[string]any{"nested": map[string]any{"used": float64(1)}}
	core.PersistOpenAICodexProbeSnapshot(observed, updates)
	<-store.entered
	observed.Credentials["access_token"] = "changed"
	nested, ok := updates["nested"].(map[string]any)
	require.True(t, ok)
	nested["used"] = float64(2)
	require.Equal(t, "observed", store.version.Credentials["access_token"])
	copied, ok := store.updates["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(1), copied["used"])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := core.StopContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "unfinished")
	require.Same(t, err, core.StopContext(context.Background()))
}

// TestOpenAIUsesOneWindowQuery 核对供应商重置时间和两个窗口结果的独立性。
func TestOpenAIUsesOneWindowQuery(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	reset5 := now.Add(time.Hour)
	reset7 := now.Add(24 * time.Hour)
	reader := &windowPairReader{}
	stats := NewLocalUsageStatistics(reader, NewOAuthUsageCache(), LocalUsageStatisticsOptions{Now: func() time.Time { return now }})
	service := NewOAuthUsageService(nil, nil, stats, OAuthUsageOptions{Now: func() time.Time { return now }})
	value, err := service.GetOpenAIUsage(context.Background(), &Record{ID: 1, Platform: PlatformOpenAI, Type: ProviderTypeOAuth, Extra: map[string]any{
		"codex_5h_used_percent": 10.0, "codex_7d_used_percent": 20.0,
		"codex_5h_reset_at": reset5.Format(time.RFC3339), "codex_7d_reset_at": reset7.Format(time.RFC3339),
	}}, false)
	require.NoError(t, err)
	require.Equal(t, 1, reader.pairs)
	require.Zero(t, reader.singles)
	require.Equal(t, []time.Time{reset5.Add(-5 * time.Hour), reset7.Add(-7 * 24 * time.Hour)}, reader.starts)
	value.FiveHour.WindowStats.Requests = 99
	require.EqualValues(t, 7, value.SevenDay.WindowStats.Requests)
}
