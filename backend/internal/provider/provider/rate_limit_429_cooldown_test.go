package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type rateLimit429ProviderRepoStub struct {
	providercore.HealthStore
	rateLimitCalls     int
	lastRateLimitID    int64
	lastRateLimitReset time.Time
}

func (r *rateLimit429ProviderRepoStub) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitID = id
	r.lastRateLimitReset = resetAt
	return nil
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
