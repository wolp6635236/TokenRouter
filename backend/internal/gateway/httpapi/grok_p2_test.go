package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaysession "github.com/TokenFlux/TokenRouter/internal/gateway/session"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestIsGrokModelSpecificFreeUsage(t *testing.T) {
	require.True(t, providercore.IsGrokModelSpecificFreeUsage(
		"you've used all the included free usage for model grok-4.5", "grok-4.5"))
	require.True(t, providercore.IsGrokModelSpecificFreeUsage("模型额度用完 grok-4.3", "grok-4.3"))
	require.False(t, providercore.IsGrokModelSpecificFreeUsage("free usage exhausted", "grok-4.5"))
}

func TestGrokStickyAffinitySeed_ScopesByModel(t *testing.T) {
	a := gatewaysession.GrokStickyAffinitySeed("session-1", []byte(`{"model":"grok-4.5"}`))
	b := gatewaysession.GrokStickyAffinitySeed("session-1", []byte(`{"model":"grok-4.3"}`))
	c := gatewaysession.GrokStickyAffinitySeed("session-1", []byte(`{"model":"grok-4.5"}`))
	require.NotEqual(t, a, b)
	require.Equal(t, a, c)
	require.Contains(t, a, "grok-affinity:v1:")
}

func TestExtractGrokModelIDsFromModelsBody(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"grok-4.5"},{"id":"grok-4.3"},{"id":"grok-4.5"}]}`)
	ids := grok.ExtractModelIDs(body)
	require.Equal(t, []string{"grok-4.5", "grok-4.3"}, ids)
}

func TestApplyGrokUpstreamFailure_ModelSpecificFreeUsage(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{providers: repo})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9109, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"You've used all the included free usage for model grok-4.5. Usage resets over a rolling 24-hour window."}}`)

	svc.handleGrokProviderUpstreamError(context.Background(), provider, 400, nil, body)

	require.Zero(t, repo.tempUnschedCalls, "model-scoped free usage must not cool sibling models")
	require.True(t, providercore.IsGrokModelQuotaBlocked(provider.Record.ID, "grok-4.5", time.Now()))
	require.False(t, providercore.IsGrokModelQuotaBlocked(provider.Record.ID, "grok-4.3", time.Now()))
}

func TestApplyGrokUpstreamFailure_SpendingLimitRemainsRecoverable(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{providers: repo})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9110, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"code":"personal-team-blocked:spending-limit","error":"spending limit reached"}`)

	svc.handleGrokProviderUpstreamError(context.Background(), provider, 403, nil, body)

	require.Equal(t, 1, repo.rateLimitedCalls)
	require.Zero(t, repo.tempUnschedCalls)
	// 缺少账期快照时使用可恢复的短期探测冷却。
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.lastRateLimitResetAt, 2*time.Second)
}
