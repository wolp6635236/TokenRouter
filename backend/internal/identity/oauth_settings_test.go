package identity_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/identity/authconfig"
)

// TestWeChatScopeDefaults covers every normalized mode and malformed mode input.
// Both exported default-scope entry points must retain the channel's default
// independently of capability flags or supplied runtime configuration.
func TestWeChatScopeDefaults(t *testing.T) {
	for _, tc := range []struct {
		mode string // supplied channel mode
		want string // expected default scope
	}{
		{"open", "snsapi_login"},
		{" OPEN ", "snsapi_login"},
		{"mp", "snsapi_userinfo"},
		{" Mp ", "snsapi_userinfo"},
		{"mobile", ""},
		{" MoBiLe ", ""},
		{"", "snsapi_login"},
		{"unknown", "snsapi_login"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			require.Equal(t, tc.want, identity.SettingsDefaultWeChatConnectScopeForMode(tc.mode))
			require.Equal(t, tc.want, identity.SettingsDefaultWeChatConnectScopesForMode(tc.mode))
		})
	}
}

// TestSettingsMergeWeChatConnectCapabilitySettings_Presence distinguishes absent
// global settings from present empty or false settings alongside saved switches.
// Startup capabilities may be restored only for the existing absent-global case.
func TestSettingsMergeWeChatConnectCapabilitySettings_Presence(t *testing.T) {
	base := authconfig.WeChatConnectConfig{Enabled: true, OpenEnabled: true, MPEnabled: true, Mode: "mp"}
	for _, tc := range []struct {
		name     string            // test scenario name
		settings map[string]string // supplied saved setting values
		enabled  bool              // resolved global enablement
		want     [3]bool           // expected open/mp/mobile capabilities
	}{
		{"absent", nil, true, [3]bool{true, true, false}},
		{"disabled_absent", nil, false, [3]bool{}},
		{"blank_capability", map[string]string{identity.SettingKeyWeChatConnectOpenEnabled: " "}, true, [3]bool{true, true, false}},
		{"synthetic_false", map[string]string{identity.SettingKeyWeChatConnectOpenEnabled: "false"}, true, [3]bool{true, true, false}},
		{"invalid_case", map[string]string{identity.SettingKeyWeChatConnectMobileEnabled: "TRUE"}, true, [3]bool{true, true, false}},
		{"explicit_global_false", map[string]string{identity.SettingKeyWeChatConnectEnabled: "false", identity.SettingKeyWeChatConnectOpenEnabled: "false"}, false, [3]bool{}},
		{"present_global_empty", map[string]string{identity.SettingKeyWeChatConnectEnabled: "", identity.SettingKeyWeChatConnectOpenEnabled: "false"}, true, [3]bool{}},
		{"disabled_saved_capability", map[string]string{identity.SettingKeyWeChatConnectEnabled: "false", identity.SettingKeyWeChatConnectMobileEnabled: " true "}, false, [3]bool{false, false, true}},
		{"saved_subset", map[string]string{identity.SettingKeyWeChatConnectMPEnabled: "true"}, true, [3]bool{false, true, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open, mp, mobile := identity.SettingsMergeWeChatConnectCapabilitySettings(tc.settings, base, tc.enabled, "")
			require.Equal(t, tc.want, [3]bool{open, mp, mobile})
		})
	}
}

// TestEffectiveWeChatConnectOAuthConfig_CredentialPrecedence mixes saved generic
// values with startup channel values and partial saved channel overrides.
// Each credential member retains its own fallback order and whitespace handling.
func TestEffectiveWeChatConnectOAuthConfig_CredentialPrecedence(t *testing.T) {
	settings := identity.NewOAuthSettings(nil, &identity.OAuthSettingsDefaults{
		WeChat: authconfig.WeChatConnectConfig{
			AppID:         " startup-id ",
			AppSecret:     " startup-secret ",
			OpenAppID:     " startup-open-id ",
			OpenAppSecret: " startup-open-secret ",
			Mode:          "open",
		},
	}, nil)
	got := settings.EffectiveWeChatConnectOAuthConfig(map[string]string{
		identity.SettingKeyWeChatConnectAppID:         " saved-id ",
		identity.SettingKeyWeChatConnectAppSecret:     " ",
		identity.SettingKeyWeChatConnectOpenAppID:     " saved-open-id ",
		identity.SettingKeyWeChatConnectOpenAppSecret: " ",
		identity.SettingKeyWeChatConnectMPAppSecret:   " saved-mp-secret ",
	})
	require.Equal(t, "saved-id", got.LegacyAppID)
	require.Equal(t, "startup-secret", got.LegacyAppSecret)
	require.Equal(t, "saved-open-id", got.OpenAppID)
	require.Equal(t, "startup-open-secret", got.OpenAppSecret)
	require.Equal(t, "saved-id", got.MPAppID)
	require.Equal(t, "saved-mp-secret", got.MPAppSecret)
	require.Equal(t, "saved-id", got.MobileAppID)
	require.Equal(t, "startup-secret", got.MobileAppSecret)
}

// TestEffectiveWeChatConnectOAuthConfig_CapabilitiesAndScopes supplies every capability set
// with recognized, unknown, and mixed-case modes and supported or invalid scopes.
// Database readback must select the expected stored mode and scope while
// retaining the caller's explicit capability observations.
func TestEffectiveWeChatConnectOAuthConfig_CapabilitiesAndScopes(t *testing.T) {
	capabilities := []struct {
		name   string    // capability combination name
		open   bool      // supplied open capability
		mp     bool      // supplied official-account capability
		mobile bool      // supplied mobile capability
		modes  [3]string // expected open/mp/mobile selections
	}{
		{"none", false, false, false, [3]string{"open", "mp", "mobile"}},
		{"open", true, false, false, [3]string{"open", "open", "open"}},
		{"mp", false, true, false, [3]string{"mp", "mp", "mp"}},
		{"mobile", false, false, true, [3]string{"mobile", "mobile", "mobile"}},
		{"open_mp", true, true, false, [3]string{"open", "mp", "open"}},
		{"open_mobile", true, false, true, [3]string{"open", "open", "mobile"}},
		{"mp_mobile", false, true, true, [3]string{"mp", "mp", "mobile"}},
		{"all", true, true, true, [3]string{"open", "mp", "mobile"}},
	}
	modes := []struct {
		raw   string // supplied mode text
		index int    // canonical mode table index
	}{
		{"open", 0},
		{"mp", 1},
		{"mobile", 2},
		{" OPEN ", 0},
		{"\tMp\n", 1},
		{" MoBiLe ", 2},
		{"", 0},
		{" \t ", 0},
		{"unknown", 0},
	}
	scopes := []struct {
		raw string // supplied scope text
		mp  string // expected official-account scope
	}{
		{"", "snsapi_userinfo"},
		{" \t ", "snsapi_userinfo"},
		{"snsapi_base", "snsapi_base"},
		{" snsapi_base ", "snsapi_base"},
		{"snsapi_userinfo", "snsapi_userinfo"},
		{" SNSAPI_BASE ", "snsapi_userinfo"},
		{"snsapi_login", "snsapi_userinfo"},
		{"snsapi_base snsapi_userinfo", "snsapi_userinfo"},
		{"invalid", "snsapi_userinfo"},
	}
	for _, capability := range capabilities {
		for _, mode := range modes {
			for _, scope := range scopes {
				t.Run(fmt.Sprintf("%s/mode=%q/scope=%q", capability.name, mode.raw, scope.raw), func(t *testing.T) {
					var settings *identity.OAuthSettings
					effective := settings.EffectiveWeChatConnectOAuthConfig(map[string]string{
						identity.SettingKeyWeChatConnectEnabled:       "false",
						identity.SettingKeyWeChatConnectOpenEnabled:   fmt.Sprint(capability.open),
						identity.SettingKeyWeChatConnectMPEnabled:     fmt.Sprint(capability.mp),
						identity.SettingKeyWeChatConnectMobileEnabled: fmt.Sprint(capability.mobile),
						identity.SettingKeyWeChatConnectMode:          mode.raw,
						identity.SettingKeyWeChatConnectScopes:        scope.raw,
					})
					wantMode := capability.modes[mode.index]
					wantScope := map[string]string{"open": "snsapi_login", "mp": scope.mp, "mobile": ""}[wantMode]
					require.Equal(t, wantMode, effective.Mode)
					require.Equal(t, wantScope, effective.Scopes)
					require.Equal(t, [3]bool{capability.open, capability.mp, capability.mobile}, [3]bool{effective.OpenEnabled, effective.MPEnabled, effective.MobileEnabled})
					require.False(t, effective.Enabled)
				})
			}
		}
	}
}

// TestEffectiveWeChatConnectOAuthConfig_LegacyNormalization supplies globally enabled
// legacy configurations without explicit channel switches. Runtime readback must
// promote the normalized mode and fill each missing credential member independently.
func TestEffectiveWeChatConnectOAuthConfig_LegacyNormalization(t *testing.T) {
	for _, tc := range []struct {
		raw          string  // supplied legacy mode
		mode         string  // expected selected mode
		capabilities [3]bool // expected open/mp/mobile switches
	}{
		{"OPEN", "open", [3]bool{true, false, false}},
		{" Mp ", "mp", [3]bool{false, true, false}},
		{"MoBiLe", "mobile", [3]bool{false, false, true}},
		{"invalid", "open", [3]bool{true, false, false}},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			base := authconfig.WeChatConnectConfig{
				Enabled:         true,
				Mode:            tc.raw,
				OpenAppID:       " open-id ",
				MPAppSecret:     " mp-secret ",
				MobileAppID:     " mobile-id ",
				MobileAppSecret: " mobile-secret ",
			}
			settings := identity.NewOAuthSettings(nil, &identity.OAuthSettingsDefaults{WeChat: base}, nil)
			effective := settings.EffectiveWeChatConnectOAuthConfig(nil)
			require.Equal(t, tc.mode, effective.Mode)
			require.Equal(t, map[string]string{"open": "snsapi_login", "mp": "snsapi_userinfo", "mobile": ""}[tc.mode], effective.Scopes)
			require.Equal(t, tc.capabilities, [3]bool{effective.OpenEnabled, effective.MPEnabled, effective.MobileEnabled})
			require.Equal(t, "open-id", effective.LegacyAppID)
			require.Equal(t, "mp-secret", effective.LegacyAppSecret)
			require.Equal(t, [6]string{"open-id", "mp-secret", "open-id", "mp-secret", "mobile-id", "mobile-secret"},
				[6]string{effective.OpenAppID, effective.OpenAppSecret, effective.MPAppID, effective.MPAppSecret, effective.MobileAppID, effective.MobileAppSecret})
		})
	}
}
