package provider

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type openAI429SnapshotRepo struct {
	providercore.HealthStore
	rateLimitedID      int64
	updatedExtra       map[string]any
	bulkUpdatedIDs     []int64
	bulkUpdatedPayload providercore.ProviderBulkUpdate
}

func (r *openAI429SnapshotRepo) SetRateLimited(_ context.Context, id int64, _ time.Time) error {
	r.rateLimitedID = id
	return nil
}

func (r *openAI429SnapshotRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updatedExtra = updates
	return nil
}

func (r *openAI429SnapshotRepo) BulkUpdate(_ context.Context, ids []int64, updates providercore.ProviderBulkUpdate) (int64, error) {
	r.bulkUpdatedIDs = append([]int64(nil), ids...)
	r.bulkUpdatedPayload = updates
	return int64(len(ids)), nil
}

func TestHandle429_OpenAIPersistsCodexSnapshotImmediately(t *testing.T) {
	repo := &openAI429SnapshotRepo{}
	svc := newOpenAI429Observer(repo)
	provider := &providercore.Record{ID: 123, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}

	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	svc.Observe429(context.Background(), provider, headers, nil)

	if repo.rateLimitedID != provider.ID {
		t.Fatalf("rateLimitedID = %d, want %d", repo.rateLimitedID, provider.ID)
	}
	if len(repo.updatedExtra) == 0 {
		t.Fatal("expected codex snapshot to be persisted on 429")
	}
	if got := repo.updatedExtra["codex_5h_used_percent"]; got != 100.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 100", got)
	}
	if got := repo.updatedExtra["codex_7d_used_percent"]; got != 100.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 100", got)
	}
}

func TestHandle429_OpenAISyncsObservedPlanType(t *testing.T) {
	repo := &openAI429SnapshotRepo{}
	svc := newOpenAI429Observer(repo)
	provider := &providercore.Record{
		ID:          124,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Credentials: map[string]any{"plan_type": "plus"},
	}
	body := []byte(`{"error":{"type":"usage_limit_reached","message":"limit reached","plan_type":"free","resets_at":1777283883}}`)

	svc.Observe429(context.Background(), provider, http.Header{}, body)

	require.Equal(t, []int64{provider.ID}, repo.bulkUpdatedIDs)
	require.Equal(t, "free", repo.bulkUpdatedPayload.Credentials["plan_type"])
	require.Equal(t, "free", provider.Credentials["plan_type"])
	require.Equal(t, provider.ID, repo.rateLimitedID)
}

// TestHandle429_SkipsSparkShadow spark 影子的限流状态只由 QueryUsage(/wham/usage
// codex_bengalfox)维护;/responses 429 携带的 global x-codex-* 不得对影子做任何 DB 限流写入,
// 否则会把 spark 误耦合到 global codex 窗口、冷却到 global reset。
func TestHandle429_SkipsSparkShadow(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	parentID := int64(900)
	shadowRepo := &openAI429SnapshotRepo{}
	shadowSvc := newOpenAI429Observer(shadowRepo)
	shadow := &providercore.Record{
		ID:               901,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   providercore.QuotaDimensionSpark,
	}

	shadowSvc.Observe429(context.Background(), shadow, headers, nil)

	require.Zero(t, shadowRepo.rateLimitedID, "spark shadow must not be SetRateLimited from /responses global 429")
	require.Empty(t, shadowRepo.updatedExtra, "spark shadow must not get a codex snapshot from /responses 429")

	// 反向对照:普通 OpenAI OAuth 提供商仍按 global 429 限流。
	normalRepo := &openAI429SnapshotRepo{}
	normalSvc := newOpenAI429Observer(normalRepo)
	normal := &providercore.Record{ID: 902, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}

	normalSvc.Observe429(context.Background(), normal, headers, nil)

	require.Equal(t, normal.ID, normalRepo.rateLimitedID, "normal OpenAI OAuth provider should still be rate limited")
}

func TestRateLimitService_HandleUpstreamError_403PreservesOriginalUpstreamMessage(t *testing.T) {
	repo := &unauthorizedHealthStore{}
	svc := newUnauthorizedObserver(repo, nil)
	provider := &providercore.Record{
		ID:       201,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
	}

	shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), 403, http.Header{}, []byte(`{"error":{"message":"workspace forbidden by policy","type":"invalid_request_error"}}`))).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Contains(t, repo.lastErrorMsg, "workspace forbidden by policy")
	require.NotContains(t, repo.lastErrorMsg, "provider may be suspended or lack permissions")
}

func TestRateLimitService_HandleUpstreamError_403FallsBackToRawBody(t *testing.T) {
	repo := &unauthorizedHealthStore{}
	svc := newUnauthorizedObserver(repo, nil)
	provider := &providercore.Record{
		ID:       202,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
	}

	shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), 403, http.Header{}, []byte(`{"error":{"type":"access_denied","details":{"reason":"ip_blocked"}}}`))).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Contains(t, repo.lastErrorMsg, `"access_denied"`)
	require.Contains(t, repo.lastErrorMsg, `"ip_blocked"`)
	require.NotContains(t, repo.lastErrorMsg, "provider may be suspended or lack permissions")
}

func TestHandle429_AnthropicPlatformUnaffected(t *testing.T) {
	// Verify that Anthropic platform providers still use the original logic
	// This test ensures we don't break existing Claude provider rate limiting

	// Simulate Anthropic 429 headers
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", "1737820800") // A future Unix timestamp

	// For Anthropic platform, calculateOpenAI429ResetTime should return nil
	// because it only handles OpenAI platform
	resetAt := providercore.OpenAI429ResetTime(openai.ParseCodexRateLimitHeaders(headers), time.Now, slog.Info)

	// Should return nil since there are no x-codex-* headers
	if resetAt != nil {
		t.Errorf("expected nil for Anthropic headers, got %v", resetAt)
	}
}

// 观测替身记录本次字段写入，健康规则和平台解析使用生产实现。
type openAI429Store interface {
	providercore.HealthStore
	providercore.SessionWindowStore
	providercore.OpenAIPlanWriter
}

func newOpenAI429Observer(repo openAI429Store) *RateLimitObserver {
	return &RateLimitObserver{Health: providercore.NewHealthService(repo, nil, providercore.HealthOptions{SessionWindows: repo}), Plans: repo}
}

func (*openAI429SnapshotRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	panic("unexpected session window")
}
