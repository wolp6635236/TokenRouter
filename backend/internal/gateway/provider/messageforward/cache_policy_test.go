package messageforward

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

func TestGatewayCacheTTLGlobalSetting_TargetResolution(t *testing.T) {
	repo := &betaSettingsFixture{values: map[string]string{
		gateway.SettingKeyEnableAnthropicCacheTTL1hInjection: "true",
	}}
	svc := NewRuntime(Dependencies{Settings: newBetaRuntime(repo.values)}, Options{})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}

	target, ok := svc.cacheUsageOverride(context.Background(), provider)
	require.True(t, ok)
	require.Equal(t, "5m", target)

	provider.Record.Extra = map[string]any{
		"cache_ttl_override_enabled": true,
		"cache_ttl_override_target":  "1h",
	}
	target, ok = svc.cacheUsageOverride(context.Background(), provider)
	require.True(t, ok)
	require.Equal(t, claude.CacheTTLTarget1h, target)
}

func TestGatewayCacheTTLGlobalSetting_RequestInjectionScope(t *testing.T) {
	repo := &betaSettingsFixture{values: map[string]string{
		gateway.SettingKeyEnableAnthropicCacheTTL1hInjection: "true",
	}}
	svc := NewRuntime(Dependencies{Settings: newBetaRuntime(repo.values)}, Options{})

	require.True(t, svc.injectTTL(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}))
	require.True(t, svc.injectTTL(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeSetupToken}}))
	require.False(t, svc.injectTTL(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}))
	require.False(t, svc.injectTTL(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}))

	repo.values[gateway.SettingKeyEnableAnthropicCacheTTL1hInjection] = "false"
	svc.dependencies.Settings.InvalidateForwarding()
	require.False(t, svc.injectTTL(context.Background(), &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}))
}

func TestRewriteMessageCacheControlIfEnabled_DefaultKeepsClientAnchors(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":[{"type":"text","text":"stable","cache_control":{"type":"ephemeral","ttl":"1h"}}]},
		{"role":"assistant","content":[{"type":"text","text":"ok"}]},
		{"role":"user","content":[{"type":"text","text":"latest","cache_control":{"type":"ephemeral","ttl":"5m"}}]}
	]}`)

	out := NewRuntime(Dependencies{}, Options{}).rewriteCache(context.Background(), body)

	require.JSONEq(t, string(body), string(out))
	require.Equal(t, "1h", gjson.GetBytes(out, "messages.0.content.0.cache_control.ttl").String())
	require.Equal(t, "5m", gjson.GetBytes(out, "messages.2.content.0.cache_control.ttl").String())
}

func TestRewriteMessageCacheControlIfEnabled_OptInPreservesLegacyRewrite(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":[{"type":"text","text":"stable","cache_control":{"type":"ephemeral","ttl":"1h"}}]},
		{"role":"assistant","content":[{"type":"text","text":"ok"}]},
		{"role":"user","content":[{"type":"text","text":"latest","cache_control":{"type":"ephemeral","ttl":"1h"}}]},
		{"role":"assistant","content":[{"type":"text","text":"done"}]}
	]}`)
	repo := &betaSettingsFixture{values: map[string]string{
		gateway.SettingKeyRewriteMessageCacheControl: "true",
	}}
	svc := NewRuntime(Dependencies{Settings: newBetaRuntime(repo.values)}, Options{})

	out := svc.rewriteCache(context.Background(), body)

	require.Equal(t, "5m", gjson.GetBytes(out, "messages.0.content.0.cache_control.ttl").String())
	require.False(t, gjson.GetBytes(out, "messages.2.content.0.cache_control").Exists())
	require.Equal(t, "5m", gjson.GetBytes(out, "messages.3.content.0.cache_control.ttl").String())
}
