package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type openAIAPIKeyHealthSettingRepo struct {
	RuntimeSettingsStore
	value    string
	getCalls int
}

type openAIAPIKeyHealthProviderRepo struct {
	HealthStore
	setCalls int
	reason   string
}

type openAIAPIKeyHealthCacheStub struct {
	TempUnschedCache
	recordCalls int
	setCalls    int
	tripped     bool
}

type openAIAPIKeyHealthRuntimeBlocker struct{ calls int }

func (r *openAIAPIKeyHealthSettingRepo) GetValue(context.Context, string) (string, error) {
	r.getCalls++
	return r.value, nil
}

func (r *openAIAPIKeyHealthProviderRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.setCalls++
	r.reason = reason
	return nil
}

func (c *openAIAPIKeyHealthCacheStub) RecordOpenAIAPIKeyHealthFailure(context.Context, int64, int, int) (int64, bool, error) {
	c.recordCalls++
	return 3, c.tripped, nil
}

func (c *openAIAPIKeyHealthCacheStub) SetTempUnsched(context.Context, int64, *TempUnschedState) error {
	c.setCalls++
	return nil
}

func (b *openAIAPIKeyHealthRuntimeBlocker) BlockProviderScheduling(*Record, time.Time, string) {
	b.calls++
}

func (*openAIAPIKeyHealthRuntimeBlocker) ClearProviderSchedulingBlock(int64) {}

func openAIHealthPoolProvider() *Record {
	return &Record{
		ID:       42,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
}

func TestOpenAIAPIKeyHealthBreakerDefaultDisabled(t *testing.T) {
	settings := NewRuntimeSettings(&openAIAPIKeyHealthSettingRepo{}, nil)
	cache := &openAIAPIKeyHealthCacheStub{tripped: true}
	svc := NewHealthService(&openAIAPIKeyHealthProviderRepo{}, cache, HealthOptions{APIKeyHealthCounter: cache, APIKeyHealthSettings: settings.GetOpenAIAPIKeyHealthBreakerSettings})

	require.False(t, svc.ApplyAPIKeyHealthFailure(context.Background(), openAIHealthPoolProvider(), http.StatusBadGateway, nil, true))
	require.Zero(t, cache.recordCalls)
}

func TestOpenAIAPIKeyHealthBreakerTripsPersistedAndRuntimeState(t *testing.T) {
	encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{Enabled: true, WindowMinutes: 1, FailureThreshold: 3, CooldownMinutes: 5})
	require.NoError(t, err)
	settings := NewRuntimeSettings(&openAIAPIKeyHealthSettingRepo{value: string(encoded)}, nil)
	cache := &openAIAPIKeyHealthCacheStub{tripped: true}
	repo := &openAIAPIKeyHealthProviderRepo{}
	blocker := &openAIAPIKeyHealthRuntimeBlocker{}
	svc := NewHealthService(repo, cache, HealthOptions{APIKeyHealthCounter: cache, APIKeyHealthSettings: settings.GetOpenAIAPIKeyHealthBreakerSettings, Block: blocker.BlockProviderScheduling})
	provider := openAIHealthPoolProvider()

	require.True(t, svc.ApplyAPIKeyHealthFailure(context.Background(), provider, http.StatusBadGateway, []byte(`{"error":"upstream"}`), true))
	require.Equal(t, 1, cache.recordCalls)
	require.Equal(t, 1, cache.setCalls)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, 1, blocker.calls)
	require.NotNil(t, provider.TempUnschedulableUntil)
	require.Contains(t, repo.reason, OpenAIAPIKeyHealthBreakerReason)
}

func TestOpenAIAPIKeyHealthSuccessDoesNotTouchSettingsOrCache(t *testing.T) {
	encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{Enabled: true, WindowMinutes: 1, FailureThreshold: 3, CooldownMinutes: 5})
	require.NoError(t, err)
	settingRepo := &openAIAPIKeyHealthSettingRepo{value: string(encoded)}
	settings := NewRuntimeSettings(settingRepo, nil)
	cache := &openAIAPIKeyHealthCacheStub{}
	svc := NewHealthService(&openAIAPIKeyHealthProviderRepo{}, cache, HealthOptions{APIKeyHealthCounter: cache, APIKeyHealthSettings: settings.GetOpenAIAPIKeyHealthBreakerSettings})

	svc.ObserveAPIKeyHealthSuccess(context.Background(), openAIHealthPoolProvider())
	svc.ObserveAPIKeyHealthSuccess(context.Background(), &Record{ID: 43, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey})
	require.Zero(t, settingRepo.getCalls)
	require.Zero(t, cache.recordCalls)
}
