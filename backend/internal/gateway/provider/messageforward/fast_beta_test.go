package messageforward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	billingcore "github.com/TokenFlux/TokenRouter/internal/billing"
	billingpricing "github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	catalogprovider "github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeAPIKeyFastModeCannotBypassSystemFilter(t *testing.T) {
	svc := NewRuntime(Dependencies{Prices: fastModeTestResolver()}, Options{Configured: true})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}
	ctx := fastModeTestContext(apikey.APIKeyFastModePolicyForceOn, "claude-opus-4-8")
	c := &requestBoundaryFixture{}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	// 模拟系统 Beta 策略已将 Claude Fast token 标记为过滤。
	state := &AttemptState{BetaEvaluated: true, BetaFilters: map[string]struct{}{claude.BetaFastMode: {}}}

	req, wireBody, err := svc.buildRequest(ctx, c, state, provider, []byte(`{"model":"claude-opus-4-8","messages":[]}`), "test-key", "apikey", "claude-opus-4-8", false, false)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(wireBody, "speed").Exists())
	require.False(t, claude.ContainsBetaToken(claude.GetHeaderRaw(req.Header, "anthropic-beta"), claude.BetaFastMode))
}

func fastModeTestContext(policy, model string) context.Context {
	ctx := apikey.WithFastModePolicy(context.Background(), policy)
	ctx = requeststate.WithGroup(ctx, &routing.Group{ID: 11})
	return context.WithValue(ctx, telemetry.Model, model)
}

func fastModeTestResolver() *billingcore.PriceResolver {
	pricing := catalogprovider.NewServiceFromSnapshot(catalogprovider.Options{ModelLookupCandidates: modelidentity.CandidatesFactory}, nil, catalogprovider.Snapshot{Data: map[string]*billingpricing.CatalogModelPricing{
		"gpt-5.5": {
			InputCostPerToken:     5e-6,
			OutputCostPerToken:    30e-6,
			SupportsServiceTier:   true,
			SupportsPromptCaching: true,
		},
		"claude-opus-4-8": {
			InputCostPerToken:     5e-6,
			OutputCostPerToken:    25e-6,
			SupportsServiceTier:   true,
			SupportsPromptCaching: true,
		},
	}})
	billing := billingtestkit.Calculator(pricing, nil)
	return billingtestkit.PriceResolver(nil, billing)
}
