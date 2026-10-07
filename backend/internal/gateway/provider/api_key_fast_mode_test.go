package provider_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	billingcore "github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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

func TestAPIKeyFastModeIgnoresUnsupportedProviderAdapters(t *testing.T) {
	openAISvc := newFastPolicyContract(t, tierpolicy.Default())
	openAISvc.Prices = fastModeTestResolver()
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "gpt-5.5")
	body, err := tierpolicy.ApplyBody([]byte(`{"model":"gpt-5.5"}`), openAISvc.Input(ctx, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey}}, "gpt-5.5"))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "service_tier").Exists())

	claudePrices := fastModeTestResolver()
	body, headers, err := gatewayprovider.
		ApplyAnthropicFastMode(ctx, claudePrices, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeBedrock}}, "claude-opus-4-8", []byte(`{"model":"claude-opus-4-8"}`), http.Header{})
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "speed").Exists())
	require.Empty(t, claude.GetHeaderRaw(headers, "anthropic-beta"))
}

func TestClaudeUsageSpeedDrivesFastBilling(t *testing.T) {
	billing := billingtestkit.Calculator(nil, nil)
	svc := completion.NewRecorder(completion.Dependencies{Calculator: billing, Prices: billingtestkit.PriceResolver(nil, billing)}, completion.RecorderOptions{DefaultMultiplier: 1})

	groupID := int64(11)
	apiKey := &apikey.APIKey{GroupID: &groupID, Group: &routing.Group{ID: groupID}}
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}
	base := &forwardcore.MessagesResult{Usage: upstream.TokenUsage{InputTokens: 1000, OutputTokens: 100}, Model: "claude-opus-4-8"}
	fast := *base
	fast.Usage.Speed = "fast"

	baseCost := svc.CalculateTokenCost(context.Background(), gatewayprovider.ProjectMessagesCompletionResult(base, gatewayprovider.ExecutionCompletionRecord(provider)), gatewayprovider.ProjectCompletionKey(apiKey), gatewayprovider.ProjectCompletionProvider(gatewayprovider.ExecutionCompletionRecord(provider)), "claude-opus-4-8", "claude-opus-4-8", "", "", 1, nil)
	fastCost := svc.CalculateTokenCost(context.Background(), gatewayprovider.ProjectMessagesCompletionResult(&fast, gatewayprovider.ExecutionCompletionRecord(provider)), gatewayprovider.ProjectCompletionKey(apiKey), gatewayprovider.ProjectCompletionProvider(gatewayprovider.ExecutionCompletionRecord(provider)), "claude-opus-4-8", "claude-opus-4-8", "", "", 1, nil)
	require.InDelta(t, baseCost.ActualCost*2, fastCost.ActualCost, 1e-12)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, completion.ClaudeServiceTier(fast.Usage.Speed))
}
