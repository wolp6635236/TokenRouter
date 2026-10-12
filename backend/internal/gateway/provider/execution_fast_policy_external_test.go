package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	billingcore "github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

func TestOpenAIAPIKeyFastModeForceOnAndOff(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	svc.Prices = fastModeTestResolver()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	forceOnCtx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "gpt-5.5")
	updated, err := tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), svc.Input(forceOnCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())

	forceOffCtx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "gpt-5.5")
	updated, err = tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5","service_tier":"priority"}`), svc.Input(forceOffCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

// TestOpenAIGroupFastForcesHTTPAndWS 验证没有客户端输入时，组级策略会同时注入 HTTP body 和 WS response.create 帧。
func TestOpenAIGroupFastForcesHTTPAndWS(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	group := &routing.Group{ID: 12, Status: billingcore.StatusActive, Hydrated: true, ForceOpenAIFast: true}
	ctx := requeststate.WithGroup(context.Background(), group)

	body, err := tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), svc.Input(ctx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(body, "service_tier").String())

	frame, blocked, err := gatewayws.ApplyServiceTierFrame([]byte(`{"type":"response.create","model":"gpt-5.5"}`), "gpt-5.5", svc.Input(ctx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(frame, "service_tier").String())
}

// TestOpenAIGroupFastStillHonorsGlobalAndKeyPolicy 验证组级强制不会绕过全局过滤或 API Key ForceOff 策略。
func TestOpenAIGroupFastStillHonorsGlobalAndKeyPolicy(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	group := &routing.Group{ID: 13, Status: billingcore.StatusActive, Hydrated: true, ForceOpenAIFast: true}
	base := requeststate.WithGroup(context.Background(), group)

	filtered := newFastPolicyContract(t, openAIFastFilterPriorityPolicy())
	body, err := tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), filtered.Input(base, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "service_tier").Exists())

	forceOff := apikey.WithFastModePolicy(base, apikey.APIKeyFastModePolicyForceOff)
	passed := newFastPolicyContract(t, tierpolicy.Default())
	body, err = tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), passed.Input(forceOff, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "service_tier").Exists())
}

// TestOpenAIGroupFastRequiresTrustedContextAndCapableProvider 验证分组可信状态与实际提供商能力分别校验，分组没有平台限制。
func TestOpenAIGroupFastRequiresTrustedContextAndCapableProvider(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	for _, tc := range []struct {
		hydrated bool
		platform string
		want     bool
	}{
		{false, capability.PlatformOpenAI, false},
		{true, capability.PlatformOpenAI, true},
		{true, capability.PlatformGrok, false},
	} {
		group := &routing.Group{ID: 14, Status: billingcore.StatusActive, Hydrated: tc.hydrated, ForceOpenAIFast: true}
		ctx := requeststate.WithGroup(context.Background(), group)
		body, err := tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), svc.Input(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: tc.platform, Type: capability.ProviderTypeAPIKey}}, "gpt-5.5"))
		require.NoError(t, err)
		require.Equal(t, tc.want, gjson.GetBytes(body, "service_tier").Exists())
	}
}

func TestOpenAIAPIKeyFastModeIgnoresUnsupportedModel(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	svc.Prices = fastModeTestResolver()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "unknown-provider-model")

	updated, err := tierpolicy.ApplyBody([]byte(`{"model":"unknown-provider-model"}`), svc.Input(ctx, provider, "unknown-provider-model"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

// TestOpenAIAPIKeyFastModeForceOffIgnoresMissingCapabilityMetadata 验证强制关闭是请求净化策略，不应受模型定价能力元数据影响。
func TestOpenAIAPIKeyFastModeForceOffIgnoresMissingCapabilityMetadata(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	svc.Prices = fastModeTestResolver()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "unknown-provider-model")

	updated, err := tierpolicy.ApplyBody([]byte(`{"model":"unknown-provider-model","service_tier":"priority"}`), svc.Input(ctx, provider, "unknown-provider-model"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())

	updated, blocked, err := gatewayws.ApplyServiceTierFrame([]byte(`{"type":"response.create","model":"unknown-provider-model","service_tier":"priority"}`), "unknown-provider-model", svc.Input(ctx, provider, "unknown-provider-model"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

// TestOpenAIAPIKeyFastModeForceOffPreservesNonFastTiers 检查强制关闭 Fast 时，其他官方服务层级是否原样保留。
func TestOpenAIAPIKeyFastModeForceOffPreservesNonFastTiers(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "unknown-provider-model")

	for _, tier := range []string{"flex", "auto", "default", "scale"} {
		t.Run(tier, func(t *testing.T) {
			body := []byte(`{"model":"unknown-provider-model","service_tier":"` + tier + `"}`)
			updated, err := tierpolicy.ApplyBody(body, svc.Input(ctx, provider, "unknown-provider-model"))
			require.NoError(t, err)
			require.Equal(t, tier, gjson.GetBytes(updated, "service_tier").String())

			wsBody := []byte(`{"type":"response.create","model":"unknown-provider-model","service_tier":"` + tier + `"}`)
			updated, blocked, err := gatewayws.ApplyServiceTierFrame(wsBody, "unknown-provider-model", svc.Input(ctx, provider, "unknown-provider-model"))
			require.NoError(t, err)
			require.Nil(t, blocked)
			require.Equal(t, tier, gjson.GetBytes(updated, "service_tier").String())
		})
	}
}

// TestOpenAIAPIKeyFastModeForceOffRemovesFastAlias 验证客户端别名 fast 归一化后仍属于 priority，强制关闭必须将其删除。
func TestOpenAIAPIKeyFastModeForceOffRemovesFastAlias(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "unknown-provider-model")

	updated, err := tierpolicy.ApplyBody([]byte(`{"model":"unknown-provider-model","service_tier":"fast"}`), svc.Input(ctx, provider, "unknown-provider-model"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

func TestOpenAIAPIKeyFastModeCannotBypassSystemPolicy(t *testing.T) {
	svc := newFastPolicyContract(t, openAIFastFilterPriorityPolicy())
	svc.Prices = fastModeTestResolver()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "gpt-5.5")

	updated, err := tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), svc.Input(ctx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())

	blockSvc := newFastPolicyContract(t, &tierpolicy.OpenAIFastPolicySettings{Rules: []tierpolicy.OpenAIFastPolicyRule{{
		ServiceTier: tierpolicy.OpenAIFastTierPriority,
		Action:      claude.BetaPolicyActionBlock,
		Scope:       claude.BetaPolicyScopeAll,
	}}})
	blockSvc.Prices = fastModeTestResolver()
	_, err = tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), blockSvc.Input(ctx, provider, "gpt-5.5"))
	var blocked *tierpolicy.BlockedError
	require.ErrorAs(t, err, &blocked)

	// 系统强制 priority 命中原始 flex 后，单 Key force_off 不能删除它。
	svc = newFastPolicyContract(t, &tierpolicy.OpenAIFastPolicySettings{Rules: []tierpolicy.OpenAIFastPolicyRule{{
		ServiceTier: tierpolicy.OpenAIFastTierFlex,
		Action:      tierpolicy.OpenAIFastPolicyActionForcePriority,
		Scope:       claude.BetaPolicyScopeAll,
	}}})
	svc.Prices = fastModeTestResolver()
	ctx = fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "gpt-5.5")
	updated, err = tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5","service_tier":"flex"}`), svc.Input(ctx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())
}

func TestOpenAIAPIKeyFastModeAppliesToRealtimeFrames(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	svc.Prices = fastModeTestResolver()
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	forceOnCtx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "gpt-5.5")
	updated, blocked, err := gatewayws.ApplyServiceTierFrame([]byte(`{"type":"response.create","model":"gpt-5.5"}`), "gpt-5.5", svc.Input(forceOnCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())

	forceOffCtx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOff, "gpt-5.5")
	updated, blocked, err = gatewayws.ApplyServiceTierFrame([]byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`), "gpt-5.5", svc.Input(forceOffCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

// TestGroupOpenAIFastPolicyHTTPAndWS 验证分组加速、单 Key 和全局规则在 HTTP/WS 中必须保持一致。
func TestGroupOpenAIFastPolicyHTTPAndWS(t *testing.T) {
	for _, tt := range []struct {
		group, tier, key, action, want string
		blocked                        bool
	}{
		{group: "follow_request", tier: "ultrafast", want: "ultrafast"},
		{group: "force_priority", tier: "ultrafast", want: "priority"},
		{group: "force_ultrafast", want: "ultrafast"},
		{group: "force_ultrafast", key: "force_on", want: "ultrafast"},
		{group: "force_ultrafast", key: "force_off"},
		{group: "force_off", tier: "ultrafast"},
		{group: "force_off", tier: "fast"},
		{group: "force_off", key: "force_on"},
		{group: "force_off", tier: "flex", want: "flex"},
		{group: "force_ultrafast", action: "filter"},
		{group: "force_ultrafast", action: "block", blocked: true},
		{group: "force_off", tier: "priority", action: "force_ultrafast", want: "ultrafast"},
	} {
		t.Run(tt.group+"/"+tt.tier+"/"+tt.key+"/"+tt.action, func(t *testing.T) {
			settings := tierpolicy.Default()
			if tt.action != "" {
				settings.Rules = []tierpolicy.OpenAIFastPolicyRule{{ServiceTier: "all", Scope: "all", Action: tt.action}}
			}
			svc := newFastPolicyContract(t, settings)
			svc.Prices = fastModeTestResolver()
			ctx := requeststate.WithGroup(fastModeTestContext(tt.key, "gpt-5.5"), &routing.Group{ID: 1, Status: billingcore.StatusActive, Hydrated: true, OpenAIFastPolicy: tt.group})
			payload := map[string]any{"model": "gpt-5.5", "type": "response.create"}
			if tt.tier != "" {
				payload["service_tier"] = tt.tier
			}
			body, _ := json.Marshal(payload)
			provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
			httpBody, httpErr := tierpolicy.ApplyBody(body, svc.Input(ctx, provider, "gpt-5.5"))
			wsBody, blocked, err := gatewayws.ApplyServiceTierFrame(body, "gpt-5.5", svc.Input(ctx, provider, "gpt-5.5"))
			require.NoError(t, err)
			if tt.blocked {
				require.Error(t, httpErr)
				require.NotNil(t, blocked)
				return
			}
			require.NoError(t, httpErr)
			require.Nil(t, blocked)
			for _, out := range [][]byte{httpBody, wsBody} {
				require.Equal(t, tt.want, gjson.GetBytes(out, "service_tier").String())
				require.Equal(t, tt.want != "", gjson.GetBytes(out, "service_tier").Exists())
			}
		})
	}
}

func openAIFastFilterPriorityPolicy() *tierpolicy.OpenAIFastPolicySettings {
	return &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionFilter,
			Scope:          claude.BetaPolicyScopeAll,
			ModelWhitelist: []string{},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
}

func TestApplyOpenAIFastPolicyToBody_DefaultPassesPriorityAndFast(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	body := []byte(`{"model":"gpt-5.5","service_tier":"priority","messages":[]}`)
	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))

	body = []byte(`{"model":"gpt-5.5","service_tier":"fast"}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())

	body = []byte(`{"model":"gpt-4","service_tier":"priority"}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-4"))
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))

	// 缺少 service_tier 时返回输入。
	body = []byte(`{"model":"gpt-5.5"}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestApplyOpenAIFastPolicyToBody_ExplicitFilterRemovesField(t *testing.T) {
	svc := newFastPolicyContract(t, openAIFastFilterPriorityPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	body := []byte(`{"model":"gpt-5.5","service_tier":"priority","messages":[]}`)
	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.NotContains(t, string(updated), `"service_tier"`)

	body = []byte(`{"model":"gpt-5.5","service_tier":"fast"}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.NotContains(t, string(updated), `"service_tier"`)
}

func TestApplyOpenAIFastPolicyToBody_UserScopedRuleOverridesGlobalRule(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{
			{
				ServiceTier: tierpolicy.OpenAIFastTierPriority,
				Action:      claude.BetaPolicyActionFilter,
				Scope:       claude.BetaPolicyScopeAll,
			},
			{
				ServiceTier: tierpolicy.OpenAIFastTierPriority,
				Action:      claude.BetaPolicyActionPass,
				Scope:       claude.BetaPolicyScopeAll,
				UserIDs:     []int64{42},
			},
		},
	}
	svc := newFastPolicyContract(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	body := []byte(`{"model":"gpt-5.5","service_tier":"priority"}`)

	allowedUserCtx := apikey.WithAccessSnapshot(context.Background(), apikey.AccessSnapshot{PayerUserID: int64(42)})
	updated, err := tierpolicy.ApplyBody(body, svc.Input(allowedUserCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())

	otherUserCtx := apikey.WithAccessSnapshot(context.Background(), apikey.AccessSnapshot{PayerUserID: int64(43)})
	updated, err = tierpolicy.ApplyBody(body, svc.Input(otherUserCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.NotContains(t, string(updated), `"service_tier"`)
}

func TestApplyOpenAIFastPolicyToBody_PriorityFilterLeavesUltrafast(t *testing.T) {
	svc := newFastPolicyContract(t, openAIFastFilterPriorityPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	body := []byte(`{"model":"gpt-5.6-sol","service_tier":"ultrafast"}`)

	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.6-sol"))
	require.NoError(t, err)
	require.Equal(t, tierpolicy.OpenAIFastTierUltrafast, gjson.GetBytes(updated, "service_tier").String())
}

func TestApplyOpenAIFastPolicyToBody_ForcePriorityRewritesKnownTier(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier: tierpolicy.OpenAIFastTierAny,
			Action:      tierpolicy.OpenAIFastPolicyActionForcePriority,
			Scope:       claude.BetaPolicyScopeAll,
		}},
	}
	svc := newFastPolicyContract(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	for _, tier := range []string{"flex", "auto", "default", "scale", "fast", "priority", "ultrafast"} {
		body := []byte(`{"model":"gpt-5.5","service_tier":"` + tier + `"}`)
		updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
		require.NoError(t, err)
		require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String(),
			"tier %q should be forced to priority", tier)
	}
}

// TestApplyOpenAIFastPolicyToBody_OfficialTiersBypassDefaultRule 验证默认配置
// 下，客户端发送的合法 OpenAI tier 会透传到上游。
func TestApplyOpenAIFastPolicyToBody_OfficialTiersBypassDefaultRule(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	for _, tier := range []string{"auto", "default", "scale"} {
		body := []byte(`{"model":"gpt-5.5","service_tier":"` + tier + `"}`)
		updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
		require.NoError(t, err, "tier %q should pass without error", tier)
		require.Contains(t, string(updated), `"service_tier":"`+tier+`"`,
			"tier %q should be preserved in body under default policy", tier)
	}

	// evaluate 层也应判定为 pass（默认配置没有内置规则）。
	for _, tier := range []string{"auto", "default", "scale"} {
		action, _ := svc.Evaluate(context.Background(), provider, "gpt-5.5", tier)
		require.Equal(t, claude.BetaPolicyActionPass, action, "tier %q should evaluate to pass", tier)
	}
}

// TestApplyOpenAIFastPolicyToBody_AllRuleStripsOfficialTiers 检查管理员配置
// ServiceTier=all + Action=filter 规则后，auto/default/scale 等官方 tier 也会
// 被剥离。首条匹配规则生效，all 匹配所有已识别的 tier。
func TestApplyOpenAIFastPolicyToBody_AllRuleStripsOfficialTiers(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier: tierpolicy.OpenAIFastTierAny,
			Action:      claude.BetaPolicyActionFilter,
			Scope:       claude.BetaPolicyScopeAll,
		}},
	}
	svc := newFastPolicyContract(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	for _, tier := range []string{"auto", "default", "scale", "priority", "flex"} {
		body := []byte(`{"model":"gpt-5.5","service_tier":"` + tier + `"}`)
		updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
		require.NoError(t, err)
		require.NotContains(t, string(updated), `"service_tier"`,
			"tier %q should be stripped under ServiceTier=all + filter rule", tier)
	}
}

// TestApplyOpenAIFastPolicyToBody_UnknownTierStripped 验证真未知 tier 仍被剥离
// （normalize 返回 nil → normalizeResponsesBodyServiceTier 删除字段；
// applyOpenAIFastPolicyToBody 在 normTier 为空时直接 no-op，因为字段已不可能存在
// 于经过前置归一化的请求里。这里直接调 apply 验证它对未识别值不会异常）。
func TestApplyOpenAIFastPolicyToBody_UnknownTierStripped(t *testing.T) {
	svc := newFastPolicyContract(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// normalize 阶段会将未知值剥离
	require.Nil(t, protocolopenai.NormalizeServiceTier("xxx"))

	// applyOpenAIFastPolicyToBody 收到未识别 tier 时不报错，body 透传不变
	// （上层 normalizeResponsesBodyServiceTier 已剥离此字段）
	body := []byte(`{"model":"gpt-5.5","service_tier":"xxx"}`)
	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestApplyOpenAIFastPolicyToBody_BlockReturnsTypedError(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionBlock,
			Scope:          claude.BetaPolicyScopeAll,
			ErrorMessage:   "fast mode is blocked for gpt-5.5",
			ModelWhitelist: []string{"gpt-5.5"},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
	svc := newFastPolicyContract(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	body := []byte(`{"model":"gpt-5.5","service_tier":"priority"}`)
	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.Error(t, err)
	var blocked *tierpolicy.BlockedError
	require.True(t, errors.As(err, &blocked))
	require.Contains(t, blocked.Message, "fast mode is blocked")
	require.Equal(t, string(body), string(updated)) // body not mutated on block
}
