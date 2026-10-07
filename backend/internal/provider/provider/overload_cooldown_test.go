package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type overloadProviderRepoStub struct {
	providercore.HealthStore
	overloadCalls   int
	errorCalls      int
	lastOverloadID  int64
	lastOverloadEnd time.Time
}

func (r *overloadProviderRepoStub) SetError(_ context.Context, _ int64, _ string) error {
	r.errorCalls++
	return nil
}

func (r *overloadProviderRepoStub) SetOverloaded(_ context.Context, id int64, until time.Time) error {
	r.overloadCalls++
	r.lastOverloadID = id
	r.lastOverloadEnd = until
	return nil
}

func TestHandle529_EnabledFromDB_PausesProvider(t *testing.T) {
	providerRepo := &overloadProviderRepoStub{}
	settingRepo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.OverloadCooldownSettings{Enabled: true, CooldownMinutes: 15})
	require.NoError(t, err)
	settingRepo.data[providercore.SettingKeyOverloadCooldownSettings] = string(data)

	settingSvc := providercore.NewRuntimeSettings(settingRepo, errCooldownSettingMissing)
	svc := newOverloadObserver(providerRepo, providercore.HealthOptions{OverloadSettings: settingSvc.GetOverloadCooldownSettings})

	provider := &providercore.Record{ID: 42, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	before := time.Now()
	svc.Core.ApplyOverload(context.Background(), provider)

	require.Equal(t, 1, providerRepo.overloadCalls)
	require.Equal(t, int64(42), providerRepo.lastOverloadID)
	require.WithinDuration(t, before.Add(15*time.Minute), providerRepo.lastOverloadEnd, 2*time.Second)
}

func TestHandle529_DisabledFromDB_SkipsProvider(t *testing.T) {
	providerRepo := &overloadProviderRepoStub{}
	settingRepo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.OverloadCooldownSettings{Enabled: false, CooldownMinutes: 15})
	require.NoError(t, err)
	settingRepo.data[providercore.SettingKeyOverloadCooldownSettings] = string(data)

	settingSvc := providercore.NewRuntimeSettings(settingRepo, errCooldownSettingMissing)
	svc := newOverloadObserver(providerRepo, providercore.HealthOptions{OverloadSettings: settingSvc.GetOverloadCooldownSettings})

	provider := &providercore.Record{ID: 42, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	svc.Core.ApplyOverload(context.Background(), provider)

	require.Equal(t, 0, providerRepo.overloadCalls, "should NOT pause when disabled")
}

func TestHandle529_NilSettingService_FallsBackToConfig(t *testing.T) {
	providerRepo := &overloadProviderRepoStub{}
	minutes := 20
	svc := newOverloadObserver(providerRepo, providercore.HealthOptions{OverloadMinutes: minutes})
	// 设置读取器留空。

	provider := &providercore.Record{ID: 77, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	before := time.Now()
	svc.Core.ApplyOverload(context.Background(), provider)

	require.Equal(t, 1, providerRepo.overloadCalls)
	require.WithinDuration(t, before.Add(20*time.Minute), providerRepo.lastOverloadEnd, 2*time.Second)
}

func TestHandle529_NilSettingService_ZeroConfig_DefaultsTen(t *testing.T) {
	providerRepo := &overloadProviderRepoStub{}
	svc := newOverloadObserver(providerRepo, providercore.HealthOptions{})

	provider := &providercore.Record{ID: 88, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	before := time.Now()
	svc.Core.ApplyOverload(context.Background(), provider)

	require.Equal(t, 1, providerRepo.overloadCalls)
	require.WithinDuration(t, before.Add(10*time.Minute), providerRepo.lastOverloadEnd, 2*time.Second)
}

func TestHandle529_DBReadError_FallsBackToConfig(t *testing.T) {
	providerRepo := &overloadProviderRepoStub{}
	errRepo := &errSettingRepo{readErr: context.DeadlineExceeded}
	errRepo.data = make(map[string]string)

	minutes := 7
	settingSvc := providercore.NewRuntimeSettings(errRepo, errCooldownSettingMissing)
	svc := newOverloadObserver(providerRepo, providercore.HealthOptions{OverloadMinutes: minutes, OverloadSettings: settingSvc.GetOverloadCooldownSettings})

	provider := &providercore.Record{ID: 99, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	before := time.Now()
	svc.Core.ApplyOverload(context.Background(), provider)

	require.Equal(t, 1, providerRepo.overloadCalls)
	require.WithinDuration(t, before.Add(7*time.Minute), providerRepo.lastOverloadEnd, 2*time.Second)
}

func TestHandleUpstreamError_529RespectsProviderPolicies(t *testing.T) {
	tests := []struct {
		name        string
		credentials map[string]any
	}{
		{
			name:        "pool mode",
			credentials: map[string]any{"pool_mode": true},
		},
		{
			name: "custom code filter excludes 529",
			credentials: map[string]any{
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(429)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &overloadProviderRepoStub{}
			svc := newOverloadObserver(repo, providercore.HealthOptions{})
			provider := &providercore.Record{
				ID:          101,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeAPIKey,
				Credentials: tt.credentials,
			}

			shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), 529, nil, []byte(`{"error":{"message":"overloaded"}}`))).StopScheduling

			require.False(t, shouldDisable)
			require.Zero(t, repo.overloadCalls)
			require.Zero(t, repo.errorCalls)
		})
	}
}

func TestHandleUpstreamError_529CustomCodeDisablesInsteadOfOverloadCooldown(t *testing.T) {
	repo := &overloadProviderRepoStub{}
	svc := newOverloadObserver(repo, providercore.HealthOptions{})
	provider := &providercore.Record{
		ID:       102,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(529)},
		},
	}

	shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), 529, nil, []byte(`{"error":{"message":"overloaded"}}`))).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.errorCalls)
	require.Zero(t, repo.overloadCalls)
}

// newOverloadObserver 为过载测试绑定健康状态组件。
func newOverloadObserver(repo providercore.HealthStore, options providercore.HealthOptions) *UpstreamHealth {
	return &UpstreamHealth{Core: providercore.NewHealthService(repo, nil, options)}
}

type errSettingRepo struct {
	cooldownSettingsStore
	readErr error
}

func (s *errSettingRepo) GetValue(context.Context, string) (string, error) { return "", s.readErr }
