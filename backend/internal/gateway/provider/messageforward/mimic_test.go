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

func TestApplyClaudeCodeOAuthMimicryToBody_HaikuRewritesSystem(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 405, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"model":"claude-haiku-4-5","system":"Pi project instructions","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewRuntime(Dependencies{}, Options{Configured: true})

	out := svc.mimic(
		context.Background(), (*requestBoundaryFixture)(nil), &AttemptState{},

		provider, body, "Pi project instructions", "claude-haiku-4-5",
	)

	system := gjson.GetBytes(out, "system").Array()
	require.Len(t, system, 3)
	require.Contains(t, system[0].Get("text").String(), "x-anthropic-billing-header:")
	require.Equal(t, claude.ClaudeCodeSystemPrompt, system[1].Get("text").String())
	require.Contains(t, gjson.GetBytes(out, "messages.0.content.0.text").String(), "Pi project instructions")
	require.Equal(t, "claude-haiku-4-5", gjson.GetBytes(out, "model").String())
}

func TestApplyClaudeCodeOAuthMimicryToBody_FableOmitsRefusedExpansion(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 406, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"model":"claude-fable-5","system":"Project instructions","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewRuntime(Dependencies{}, Options{Configured: true})

	out := svc.mimic(
		context.Background(), (*requestBoundaryFixture)(nil), &AttemptState{},

		provider, body, "Project instructions", "claude-fable-5",
	)

	system := gjson.GetBytes(out, "system").Array()
	require.Len(t, system, 2)
	require.Contains(t, system[0].Get("text").String(), "x-anthropic-billing-header:")
	require.Equal(t, claude.ClaudeCodeSystemPrompt, system[1].Get("text").String())
	require.NotContains(t, string(out), claude.ClaudeCodeSystemPromptExpansion)
	require.Contains(t, gjson.GetBytes(out, "messages.0.content.0.text").String(), "Project instructions")
	require.Equal(t, "Understood. I will follow these instructions.", gjson.GetBytes(out, "messages.1.content.0.text").String())
	require.Equal(t, "hello", gjson.GetBytes(out, "messages.2.content").String())
}

// TestGatewayClientDatelineNormalization_Scope 覆盖提供商类型与开关组合：
// 只有开关开启时 Anthropic OAuth/SetupToken 才会通过，API-Key 与非 Anthropic
// 平台始终跳过。
func TestGatewayClientDatelineNormalization_Scope(t *testing.T) {
	repo := &betaSettingsFixture{values: map[string]string{}}
	svc := NewRuntime(Dependencies{Settings: newBetaRuntime(repo.values)}, Options{})
	ctx := context.Background()

	// 默认缺省：parseSettings 与缓存加载器的 fallback 都是 true。
	require.True(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}))
	require.True(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeSetupToken}}))
	require.False(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}))
	require.False(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}))

	// 关闭开关：任何提供商都不归一化。
	repo.values[gateway.SettingKeyEnableClientDatelineNormalization] = "false"
	svc.dependencies.Settings.InvalidateForwarding()
	require.False(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}))
	require.False(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeSetupToken}}))

	// 重新开启开关：OAuth 再次通过。
	repo.values[gateway.SettingKeyEnableClientDatelineNormalization] = "true"
	svc.dependencies.Settings.InvalidateForwarding()
	require.True(t, svc.shouldNormalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}))
}

// TestGatewayClientDatelineNormalization_HelperNoRewrite 覆盖 Forward 使用的辅助路径：
// 开关关闭、API-Key、空提供商或请求体没有指纹 dateline 时返回 ok=false；
// 开关开启且提供商为 Anthropic OAuth/SetupToken 并实际改写时返回 ok=true 和新请求体。
func TestGatewayClientDatelineNormalization_HelperNoRewrite(t *testing.T) {
	repo := &betaSettingsFixture{values: map[string]string{
		gateway.SettingKeyEnableClientDatelineNormalization: "true",
	}}
	svc := NewRuntime(Dependencies{Settings: newBetaRuntime(repo.values)}, Options{})
	ctx := context.Background()

	dirty := []byte(`{"messages":[{"role":"user","content":"<system-reminder>\nToday’s date is 2026/07/01.\n</system-reminder>"}]}`)
	clean := []byte(`{"messages":[{"role":"user","content":"just hello"}]}`)

	// API-Key 提供商：即使请求体包含指纹也不改写。
	next, ok := svc.normalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}, dirty)
	require.False(t, ok)
	require.Nil(t, next)

	// 空提供商：安全跳过。
	next, ok = svc.normalizeDateline(ctx, nil, dirty)
	require.False(t, ok)
	require.Nil(t, next)

	// OAuth 提供商 + 干净请求体：没有变化，ok=false。
	next, ok = svc.normalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}, clean)
	require.False(t, ok)
	require.Nil(t, next)

	// OAuth 提供商 + 带指纹请求体：完成改写，ok=true。
	next, ok = svc.normalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}, dirty)
	require.True(t, ok)
	require.NotNil(t, next)
	require.Contains(t, string(next), "Today's date is 2026-07-01.")
	require.NotContains(t, string(next), "2026/07/01")
	require.NotContains(t, string(next), "Today’s date is")

	// SetupToken 提供商 + 带指纹请求体：完成改写，ok=true。
	next, ok = svc.normalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeSetupToken}}, dirty)
	require.True(t, ok)
	require.Contains(t, string(next), "Today's date is 2026-07-01.")

	// 关闭开关：即使 OAuth 提供商也不改写。
	repo.values[gateway.SettingKeyEnableClientDatelineNormalization] = "false"
	svc.dependencies.Settings.InvalidateForwarding()
	next, ok = svc.normalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}, dirty)
	require.False(t, ok)
	require.Nil(t, next)
}

// TestGatewayClientDatelineNormalization_LeavesUserProseUntouched 检查日期规范化后 <system-reminder> 外的用户文本逐字节保持一致。
func TestGatewayClientDatelineNormalization_LeavesUserProseUntouched(t *testing.T) {
	repo := &betaSettingsFixture{values: map[string]string{
		gateway.SettingKeyEnableClientDatelineNormalization: "true",
	}}
	svc := NewRuntime(Dependencies{Settings: newBetaRuntime(repo.values)}, Options{})
	ctx := context.Background()

	// 用户文本如果只是在 <system-reminder> 外碰巧包含类似指纹的句子，必须逐字节保留。
	body := []byte(`{"messages":[{"role":"user","content":"I wrote: Today’s date is 2026/07/01. What do you think?"}]}`)
	next, ok := svc.normalizeDateline(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}, body)
	require.False(t, ok, "must not rewrite user prose outside <system-reminder>")
	require.Nil(t, next)

	// 日期转换仅扫描 <system-reminder> 内的内容。
	out, hits, changed := claude.NormalizeDateline(body)
	require.False(t, changed)
	require.Empty(t, hits)
	require.Equal(t, body, out)
}
