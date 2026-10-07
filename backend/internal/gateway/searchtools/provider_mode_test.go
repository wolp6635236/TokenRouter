package searchtools_test

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/gateway/searchtools"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGetWebSearchEmulationMode_Enabled(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: "enabled"},
	}
	require.Equal(t, searchtools.ModeEnabled, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_Disabled(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: "disabled"},
	}
	require.Equal(t, searchtools.ModeDisabled, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_Default(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: "default"},
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_UnknownString(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: "unknown"},
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_OldBoolTrue(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: true},
	}
	// bool true → tolerant fallback → enabled (not default)
	require.Equal(t, searchtools.ModeEnabled, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_OldBoolFalse(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: false},
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_NilProvider(t *testing.T) {
	var a *searchtools.ProviderPolicy
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_NilExtra(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    nil,
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_MissingField(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{},
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_NonAnthropicPlatform(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Extra:    map[string]any{searchtools.FeatureKey: "enabled"},
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}

func TestGetWebSearchEmulationMode_NonAPIKeyType(t *testing.T) {
	a := &searchtools.ProviderPolicy{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Extra:    map[string]any{searchtools.FeatureKey: "enabled"},
	}
	require.Equal(t, searchtools.ModeDefault, searchtools.ProviderMode(a).Mode)
}
