package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// Usage configuration tests exercise default inheritance, scalar normalization
// and rejection of credential or template fields. The same fixture also checks
// unsafe root formatting and the ordered administrator adapter choices, tying
// effective reads to persisted configuration validation.

// TestEffectiveUpstreamUsageConfigDefaultsAndNormalization exercises absent,
// disabled and malformed usage choices alongside adapter selection. It checks
// normalized fields, structured errors for unsupported and sensitive input,
// unsafe URL rejection and manual catalog order.
func TestEffectiveUpstreamUsageConfigDefaultsAndNormalization(t *testing.T) {
	provider := &Record{Type: capability.ProviderTypeAPIKey}
	config, err := EffectiveUpstreamUsageConfig(provider)
	require.NoError(t, err)
	require.Equal(t, UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterSub2API}, config)

	extra := map[string]any{UpstreamUsageQueryExtraKey: map[string]any{
		"enabled":  false,
		"adapter":  UpstreamUsageAdapterNewAPI,
		"base_url": "https://usage.example/v1",
	}}
	require.NoError(t, NormalizeUpstreamUsageExtra(extra))
	require.Equal(t, map[string]any{
		"enabled":  false,
		"adapter":  UpstreamUsageAdapterNewAPI,
		"base_url": "https://usage.example/v1",
	}, extra[UpstreamUsageQueryExtraKey])

	bad := map[string]any{UpstreamUsageQueryExtraKey: map[string]any{"api_key": "secret"}}
	require.Error(t, NormalizeUpstreamUsageExtra(bad))

	disabledProvider := &Record{Extra: map[string]any{UpstreamUsageQueryExtraKey: map[string]any{"enabled": false}}}
	disabled, err := EffectiveUpstreamUsageConfig(disabledProvider)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.Equal(t, UpstreamUsageAdapterSub2API, disabled.Adapter)

	unknown := map[string]any{UpstreamUsageQueryExtraKey: map[string]any{"adapter": "custom-script"}}
	require.ErrorIs(t, NormalizeUpstreamUsageExtra(unknown), ErrUpstreamUsageConfigInvalid)
	_, err = EffectiveUpstreamUsageConfig(&Record{Extra: unknown})
	require.ErrorIs(t, err, ErrUpstreamUsageUnsupported)

	unsafeURL := map[string]any{UpstreamUsageQueryExtraKey: map[string]any{
		"base_url": "https://user:secret@gateway.example/v1?token=secret",
	}}
	require.ErrorIs(t, NormalizeUpstreamUsageExtra(unsafeURL), ErrUpstreamUsageConfigInvalid)
	_, ok := NormalizedUpstreamUsageConfigValue(map[string]any{"api_key": "secret"})
	require.False(t, ok)

	require.Equal(t, []UpstreamUsageAdapterOption{
		{Name: UpstreamUsageAdapterSub2API, Label: "Sub2API / TokenRouter"},
		{Name: UpstreamUsageAdapterNewAPI, Label: "New API"},
		{Name: UpstreamUsageAdapterZivv, Label: "Zivv"},
		{Name: UpstreamUsageAdapterDerouter, Label: "derouter.ai (Account Key)"},
	}, UpstreamUsageAdapterOptions())
}

// TestEffectiveUpstreamUsageConfigDerouter resolves an omitted management root
// and a custom override. Saved configuration and forwarding credentials remain
// unchanged while the effective query uses the correct account endpoint.
func TestEffectiveUpstreamUsageConfigDerouter(t *testing.T) {
	for _, override := range []string{"", "https://management.example.test/prefix"} {
		extra := map[string]any{UpstreamUsageQueryExtraKey: map[string]any{"adapter": UpstreamUsageAdapterDerouter, "base_url": override}}
		require.NoError(t, NormalizeUpstreamUsageExtra(extra))
		p := &Record{Type: ProviderTypeAPIKey, Platform: PlatformAnthropic, Credentials: map[string]any{"base_url": "https://forward.example.test/anthropic"}, Extra: extra}
		original := CloneRecord(p)
		got, err := EffectiveUpstreamUsageConfig(p)
		require.NoError(t, err)
		want := override
		if want == "" {
			want = DerouterUsageDefaultBaseURL
		}
		require.Equal(t, want, got.BaseURL)
		require.Equal(t, UpstreamUsageAdapterDerouter, got.Adapter)
		require.Equal(t, original, p)
		require.True(t, BalanceAlertSupported(p))
	}
}

// TestEffectiveUpstreamUsageConfigPlatformAdapters resolves Kimi, Zhipu, and
// DeepSeek and OpenCode providers whose saved settings request a generic relay
// format and enable queries. Platform and mode must select the fixed adapter
// and management root, and modes without a format must resolve to an empty
// disabled adapter.
func TestEffectiveUpstreamUsageConfigPlatformAdapters(t *testing.T) {
	const override = "https://usage.example.test/prefix"
	tests := []struct {
		name     string
		platform string
		mode     string
		baseURL  string
		want     UpstreamUsageQueryConfig
		fixed    bool
	}{
		{"kimi coding", PlatformKimi, ProviderModeCoding, "", UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterKimiCoding}, true},
		{"kimi payg", PlatformKimi, ProviderModePayG, "", UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterKimiBalance}, true},
		{"zhipu coding", PlatformZhipu, ProviderModeCoding, "", UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterZhipuCoding}, true},
		{"zhipu payg", PlatformZhipu, ProviderModePayG, "", UpstreamUsageQueryConfig{}, true},
		{"deepseek", PlatformDeepseek, ProviderModePayG, "", UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterDeepseekBalance}, true},
		{"opencode go default root", PlatformOpenCode, ProviderModeGo, "", UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterOpenCodeGo, BaseURL: OpenCodeGoUsageDefaultBaseURL}, true},
		{"opencode go override", PlatformOpenCode, ProviderModeGo, override, UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterOpenCodeGo, BaseURL: override}, true},
		{"opencode payg", PlatformOpenCode, ProviderModePayG, override, UpstreamUsageQueryConfig{BaseURL: override}, true},
		{"relay keeps saved adapter", PlatformOpenAI, "", "", UpstreamUsageQueryConfig{Enabled: true, Adapter: UpstreamUsageAdapterNewAPI}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := &Record{Platform: test.platform, Type: ProviderTypeAPIKey, Credentials: map[string]any{"provider_mode": test.mode}, Extra: map[string]any{
				UpstreamUsageQueryExtraKey: map[string]any{"enabled": true, "adapter": UpstreamUsageAdapterNewAPI, "base_url": test.baseURL},
			}}
			name, fixed := PlatformUsageAdapter(p)
			require.Equal(t, test.fixed, fixed)
			if fixed {
				require.Equal(t, test.want.Adapter, name)
			}
			got, err := EffectiveUpstreamUsageConfig(p)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
