package provider

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// updateExtraSpyRepo 记录 UpdateExtra 是否被调用,用于验证影子 codex_* 快照守卫。
type updateExtraSpyRepo struct {
	*openAI429SnapshotRepo
	updateExtraCalled bool
}

type openAI429SnapshotRepo struct {
	providercore.HealthStore
	rateLimitedID      int64
	updatedExtra       map[string]any
	bulkUpdatedIDs     []int64
	bulkUpdatedPayload providercore.ProviderBulkUpdate
}

// 观测替身记录本次字段写入，健康规则和平台解析使用生产实现。
type openAI429Store interface {
	providercore.HealthStore
	providercore.SessionWindowStore
	providercore.OpenAIPlanWriter
}

type rateLimit429ProviderRepoStub struct {
	providercore.HealthStore
	rateLimitCalls     int
	lastRateLimitID    int64
	lastRateLimitReset time.Time
}

// TestPersistOpenAI429PlanType_SkipsShadow 检查影子提供商跳过 429 响应中的 plan_type 写入。
// 普通提供商通过 BulkUpdate 写入 credentials。
func TestPersistOpenAI429PlanType_SkipsShadow(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"error":{"type":"usage_limit_reached","plan_type":"pro"}}`)
	parentID := int64(1)

	t.Run("shadow_skipped", func(t *testing.T) {
		repo := &openAI429SnapshotRepo{}
		shadow := &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{}, ParentProviderID: &parentID}
		providercore.PersistOpenAIObservedPlan(ctx, repo, shadow, openai.ParseUsageLimitPlanType(body), slog.Info, slog.Warn)
		require.Empty(t, shadow.Credentials, "影子不可被写入 plan_type 凭据")
	})

	t.Run("normal_provider_writes", func(t *testing.T) {
		repo := &openAI429SnapshotRepo{}
		normal := &providercore.Record{ID: 9, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{}}
		providercore.PersistOpenAIObservedPlan(ctx, repo, normal, openai.ParseUsageLimitPlanType(body), slog.Info, slog.Warn)
		require.Equal(t, "pro", normal.Credentials["plan_type"], "普通提供商应写入 plan_type(反向对照,证明 body 有效、写路径通)")
	})
}

// TestPersistOpenAICodexSnapshot_SkipsShadow 检查影子提供商跳过 429 响应中的全局额度头。
// 影子 codex_* 快照由 QueryUsage 的 /wham/usage bengalfox 查询更新。
func TestPersistOpenAICodexSnapshot_SkipsShadow(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "50")
	parentID := int64(1)

	t.Run("shadow_skipped", func(t *testing.T) {
		spy := &updateExtraSpyRepo{openAI429SnapshotRepo: &openAI429SnapshotRepo{}}
		s := newOpenAI429Observer(spy)
		shadow := &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ParentProviderID: &parentID}
		s.PersistCodexSnapshot(context.Background(), shadow, headers)
		require.False(t, spy.updateExtraCalled, "影子不应写 codex_* 头快照")
	})

	t.Run("normal_provider_writes", func(t *testing.T) {
		spy := &updateExtraSpyRepo{openAI429SnapshotRepo: &openAI429SnapshotRepo{}}
		s := newOpenAI429Observer(spy)
		normal := &providercore.Record{ID: 9, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}
		s.PersistCodexSnapshot(context.Background(), normal, headers)
		require.True(t, spy.updateExtraCalled, "普通提供商应写 codex_* 头快照(反向对照)")
	})
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

func TestHandle429_FallbackUsesDBSeconds(t *testing.T) {
	providerRepo := &rateLimit429ProviderRepoStub{}
	settingRepo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.RateLimit429CooldownSettings{Enabled: true, CooldownSeconds: 12})
	require.NoError(t, err)
	settingRepo.data[providercore.SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := providercore.NewRuntimeSettings(settingRepo, errCooldownSettingMissing)
	svc := &RateLimitObserver{Health: providercore.NewHealthService(providerRepo, nil, providercore.HealthOptions{RateLimit429Settings: settingSvc.GetRateLimit429CooldownSettings})}

	provider := &providercore.Record{ID: 42, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}
	before := time.Now()
	svc.Observe429(context.Background(), provider, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, providerRepo.rateLimitCalls)
	require.Equal(t, int64(42), providerRepo.lastRateLimitID)
	require.True(t, !providerRepo.lastRateLimitReset.Before(before.Add(12*time.Second)) && !providerRepo.lastRateLimitReset.After(after.Add(12*time.Second)))
}

func TestHandle429_FallbackDisabledSkipsLocalMark(t *testing.T) {
	providerRepo := &rateLimit429ProviderRepoStub{}
	settingRepo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.RateLimit429CooldownSettings{Enabled: false, CooldownSeconds: 12})
	require.NoError(t, err)
	settingRepo.data[providercore.SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := providercore.NewRuntimeSettings(settingRepo, errCooldownSettingMissing)
	svc := &RateLimitObserver{Health: providercore.NewHealthService(providerRepo, nil, providercore.HealthOptions{RateLimit429Settings: settingSvc.GetRateLimit429CooldownSettings})}

	provider := &providercore.Record{ID: 43, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}
	svc.Observe429(context.Background(), provider, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))

	require.Zero(t, providerRepo.rateLimitCalls)
}

// TestHandle429_AnthropicNoResetTimeUsesFallbackCooldown 检查 Anthropic 的 429 缺少 reset 头时采用默认短冷却。
func TestHandle429_AnthropicNoResetTimeUsesFallbackCooldown(t *testing.T) {
	providerRepo := &rateLimit429ProviderRepoStub{}
	settingRepo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.RateLimit429CooldownSettings{Enabled: true, CooldownSeconds: 12})
	require.NoError(t, err)
	settingRepo.data[providercore.SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := providercore.NewRuntimeSettings(settingRepo, errCooldownSettingMissing)
	svc := &RateLimitObserver{Health: providercore.NewHealthService(providerRepo, nil, providercore.HealthOptions{RateLimit429Settings: settingSvc.GetRateLimit429CooldownSettings})}

	provider := &providercore.Record{ID: 45, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	before := time.Now()
	svc.Observe429(context.Background(), provider, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"Extra usage required"}}`))
	after := time.Now()

	require.Equal(t, 1, providerRepo.rateLimitCalls)
	require.Equal(t, int64(45), providerRepo.lastRateLimitID)
	require.True(t, !providerRepo.lastRateLimitReset.Before(before.Add(12*time.Second)) && !providerRepo.lastRateLimitReset.After(after.Add(12*time.Second)))
}

// TestHandle429_AnthropicNoResetTimeFallbackDisabledSkipsMark 检查默认冷却关闭后，缺少 reset 头的 429 保持提供商状态不变。
func TestHandle429_AnthropicNoResetTimeFallbackDisabledSkipsMark(t *testing.T) {
	providerRepo := &rateLimit429ProviderRepoStub{}
	settingRepo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.RateLimit429CooldownSettings{Enabled: false, CooldownSeconds: 12})
	require.NoError(t, err)
	settingRepo.data[providercore.SettingKeyRateLimit429CooldownSettings] = string(data)

	settingSvc := providercore.NewRuntimeSettings(settingRepo, errCooldownSettingMissing)
	svc := &RateLimitObserver{Health: providercore.NewHealthService(providerRepo, nil, providercore.HealthOptions{RateLimit429Settings: settingSvc.GetRateLimit429CooldownSettings})}

	provider := &providercore.Record{ID: 46, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	svc.Observe429(context.Background(), provider, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"Extra usage required"}}`))

	require.Zero(t, providerRepo.rateLimitCalls)
}

func TestHandle429_FallbackUsesDefaultSecondsWhenSettingServiceMissing(t *testing.T) {
	providerRepo := &rateLimit429ProviderRepoStub{}
	svc := &RateLimitObserver{Health: providercore.NewHealthService(providerRepo, nil, providercore.HealthOptions{})}

	provider := &providercore.Record{ID: 44, Platform: capability.PlatformGemini, Type: capability.ProviderTypeAPIKey}
	before := time.Now()
	svc.Observe429(context.Background(), provider, http.Header{}, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, providerRepo.rateLimitCalls)
	require.Equal(t, int64(44), providerRepo.lastRateLimitID)
	require.True(t, !providerRepo.lastRateLimitReset.Before(before.Add(5*time.Second)) && !providerRepo.lastRateLimitReset.After(after.Add(5*time.Second)))
}

func (r *updateExtraSpyRepo) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	r.updateExtraCalled = true
	return nil
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

func newOpenAI429Observer(repo openAI429Store) *RateLimitObserver {
	return &RateLimitObserver{Health: providercore.NewHealthService(repo, nil, providercore.HealthOptions{SessionWindows: repo}), Plans: repo}
}

func (*openAI429SnapshotRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	panic("unexpected session window")
}

func (r *rateLimit429ProviderRepoStub) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitID = id
	r.lastRateLimitReset = resetAt
	return nil
}
