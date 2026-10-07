package provider_test

import (
	"context"
	"encoding/json"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestGetRateLimit429CooldownSettings_DefaultsWhenNotSet(t *testing.T) {
	repo := newCooldownSettingsStore()
	svc := providercore.NewRuntimeSettings(repo, errCooldownSettingMissing)

	settings, err := svc.GetRateLimit429CooldownSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, 5, settings.CooldownSeconds)
}

func TestGetRateLimit429CooldownSettings_ReadsFromDB(t *testing.T) {
	repo := newCooldownSettingsStore()
	data, err := json.Marshal(providercore.RateLimit429CooldownSettings{Enabled: false, CooldownSeconds: 12})
	require.NoError(t, err)
	repo.data[providercore.SettingKeyRateLimit429CooldownSettings] = string(data)
	svc := providercore.NewRuntimeSettings(repo, errCooldownSettingMissing)

	settings, err := svc.GetRateLimit429CooldownSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 12, settings.CooldownSeconds)
}

func TestSetRateLimit429CooldownSettings_EnabledRejectsOutOfRange(t *testing.T) {
	svc := providercore.NewRuntimeSettings(newCooldownSettingsStore(), errCooldownSettingMissing)

	for _, seconds := range []int{0, -1, 7201, 99999} {
		err := svc.SetRateLimit429CooldownSettings(context.Background(), &providercore.RateLimit429CooldownSettings{
			Enabled: true, CooldownSeconds: seconds,
		})
		require.Error(t, err, "should reject enabled=true + cooldown_seconds=%d", seconds)
		require.Contains(t, err.Error(), "cooldown_seconds must be between 1-7200")
	}
}
