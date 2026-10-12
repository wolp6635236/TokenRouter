package settings_test

import (
	"context"
	"maps"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	settingscore "github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/settings/composite"
	settingskit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
)

type mockSettingRepo struct {
	mu            sync.Mutex
	data          map[string]string
	getValueErr   error
	getValueCalls int
}

func newSettingServiceForPlatformThresholdTest(seed map[string]string) *settingskit.Composite {
	svc, _ := newSettingServiceAndRepoForPlatformThresholdTest(seed)
	return svc
}

func newSettingServiceAndRepoForPlatformThresholdTest(seed map[string]string) (*settingskit.Composite, *mockSettingRepo) {
	repo := newMockSettingRepo()
	maps.Copy(repo.data, seed)
	return settingskit.NewComposite(repo, &config.Config{}), repo
}

func TestPlatformSchedulingThresholds_RoundTrip_DefaultsAndStoredValues(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	got := composite.Parse(map[string]string{}, svc.Read)
	require.Equal(t, map[string]int{
		capability.PlatformOpenAI:    100,
		capability.PlatformAnthropic: 100,
		capability.PlatformGrok:      100,
	}, got.ProviderSchedulingThresholds)

	got = composite.Parse(map[string]string{provider.SettingKeyProviderSchedulingThresholds: `{"openai":91,"grok":77,"gemini":85,"kiro":99}`}, svc.Read)
	require.Equal(t, 91, got.ProviderSchedulingThresholds[capability.PlatformOpenAI])
	require.Equal(t, 100, got.ProviderSchedulingThresholds[capability.PlatformAnthropic])
	require.Equal(t, 77, got.ProviderSchedulingThresholds[capability.PlatformGrok])
	require.NotContains(t, got.ProviderSchedulingThresholds, capability.PlatformGemini)
	require.NotContains(t, got.ProviderSchedulingThresholds, "kiro")
}

func TestBuildSystemSettingsUpdates_PersistsProviderSchedulingThresholds(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	updates, err := composite.Prepare(context.Background(), &composite.Snapshot{
		ProviderSchedulingThresholds: map[string]int{
			capability.PlatformOpenAI:    91,
			capability.PlatformAnthropic: 88,
			capability.PlatformGrok:      77,
		},
	}, svc.Prepare)
	require.NoError(t, err)
	require.JSONEq(t, `{"openai":91,"anthropic":88,"grok":77}`, updates[provider.SettingKeyProviderSchedulingThresholds])
}

func TestValidateAndNormalizeProviderSchedulingThresholds_FillsMissingPlatforms(t *testing.T) {
	normalized, err := provider.ValidateAndNormalizeProviderSchedulingThresholds(map[string]int{
		capability.PlatformOpenAI: 91,
	})
	require.NoError(t, err)
	require.Equal(t, 91, normalized[capability.PlatformOpenAI])
	require.Equal(t, 100, normalized[capability.PlatformAnthropic])
	require.Equal(t, 100, normalized[capability.PlatformGrok])
	require.NotContains(t, normalized, capability.PlatformGemini)
	require.NotContains(t, normalized, "kiro")
	require.NotContains(t, normalized, capability.PlatformAntigravity)
}

func TestValidateAndNormalizeProviderSchedulingThresholds_RejectsUnsupportedPlatforms(t *testing.T) {
	_, err := provider.ValidateAndNormalizeProviderSchedulingThresholds(map[string]int{
		capability.PlatformGemini: 85,
	})
	require.Error(t, err)
}

func TestUpdateSettings_StoresProviderSchedulingThresholds(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	err := svc.Save(context.Background(), &composite.Snapshot{
		ProviderSchedulingThresholds: map[string]int{
			capability.PlatformOpenAI:    92,
			capability.PlatformAnthropic: 89,
			capability.PlatformGrok:      76,
		},
	})
	require.NoError(t, err)

	stored, err := svc.Store.GetValue(context.Background(), provider.SettingKeyProviderSchedulingThresholds)
	require.NoError(t, err)
	got := composite.Parse(map[string]string{provider.SettingKeyProviderSchedulingThresholds: stored}, svc.Read)
	require.Equal(t, 92, got.ProviderSchedulingThresholds[capability.PlatformOpenAI])
	require.Equal(t, 89, got.ProviderSchedulingThresholds[capability.PlatformAnthropic])
	require.Equal(t, 76, got.ProviderSchedulingThresholds[capability.PlatformGrok])
	require.NotContains(t, got.ProviderSchedulingThresholds, "kiro")
}

func TestGetProviderSchedulingThresholds_ReadsStoredValue(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{provider.SettingKeyProviderSchedulingThresholds: `{"openai":93,"grok":88,"kiro":87}`})

	got := svc.Provider.GetProviderSchedulingThresholds(context.Background())

	require.Equal(t, 93, got[capability.PlatformOpenAI])
	require.Equal(t, 100, got[capability.PlatformAnthropic])
	require.Equal(t, 88, got[capability.PlatformGrok])
	require.NotContains(t, got, "kiro")
}

func TestUpdateSettings_OmittedProviderSchedulingThresholdsDoesNotCacheDefaults(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{provider.SettingKeyProviderSchedulingThresholds: `{"openai":85,"grok":88,"kiro":87}`})

	err := svc.Save(context.Background(), &composite.Snapshot{
		FrontendURL: "https://example.test",
	})
	require.NoError(t, err)

	got := svc.Provider.GetProviderSchedulingThresholds(context.Background())
	require.Equal(t, 85, got[capability.PlatformOpenAI])
	require.Equal(t, 88, got[capability.PlatformGrok])
	require.NotContains(t, got, "kiro")
}

func TestProviderSchedulingThresholds_InvalidStoredValueUsesSameDefaultsInSettingsAndCache(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{provider.SettingKeyProviderSchedulingThresholds: `{"openai":0,"grok":88,"kiro":87}`})

	settings := composite.Parse(map[string]string{provider.SettingKeyProviderSchedulingThresholds: `{"openai":0,"grok":88,"kiro":87}`}, svc.Read)
	cached := svc.Provider.GetProviderSchedulingThresholds(context.Background())

	require.Equal(t, settings.ProviderSchedulingThresholds, cached)
	require.Equal(t, 100, cached[capability.PlatformOpenAI])
	require.Equal(t, 88, cached[capability.PlatformGrok])
	require.NotContains(t, cached, "kiro")
}

func TestGetProviderSchedulingThresholds_NilRepoReturnsDefaults(t *testing.T) {
	svc := provider.NewRuntimeSettings(nil, nil)
	got := svc.GetProviderSchedulingThresholds(context.Background())
	require.Equal(t, map[string]int{
		capability.PlatformOpenAI:    100,
		capability.PlatformAnthropic: 100,
		capability.PlatformGrok:      100,
	}, got)
}

func newMockSettingRepo() *mockSettingRepo {
	return &mockSettingRepo{data: make(map[string]string)}
}

func (m *mockSettingRepo) Get(_ context.Context, key string) (*settingscore.Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, settingscore.ErrSettingNotFound
	}
	return &settingscore.Setting{Key: key, Value: v}, nil
}

func (m *mockSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getValueCalls++
	if m.getValueErr != nil {
		return "", m.getValueErr
	}
	v, ok := m.data[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

func (m *mockSettingRepo) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			result[k] = v
		}
	}
	return result, nil
}

func (m *mockSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	maps.Copy(m.data, settings)
	return nil
}

func (m *mockSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string, len(m.data))
	maps.Copy(result, m.data)
	return result, nil
}

func (m *mockSettingRepo) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
