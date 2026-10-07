package wsentry

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestWSTurnPricing 检查后续轮次换型号、按次零价和当前分组的独立定价。
func TestWSTurnPricing(t *testing.T) {
	zero := 0.0
	prices := &admission.ModelPricing{Resolver: testkit.ResolverWithCards(t, billing.NewCalculator(nil, billing.CalculatorOptions{}), []routing.ModelPricingEntry{{
		Models: []string{"priced-review"}, BillingMode: pricing.BillingModePerRequest, PerRequestPrice: &zero,
	}})}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	target := &openAIWSEntryTarget{
		provider: gatewayprovider.NewExecutionProvider(&provider.Record{ID: 1, Platform: "openai", Type: "apikey"}),
		root: &openAIWSEntryAdapter{c: c, key: &apikey.APIKey{GroupID: testkit.GroupID()}, bindings: Bindings{
			Pricing: prices,
			ResolveRouting: func(_ context.Context, _ *int64, _ *gatewayprovider.ExecutionProvider, model string, _ provider.OpenAIEndpointCapability) (string, error) {
				return model, nil
			},
		}},
	}
	for _, model := range []string{"priced-review", "codex-auto-review", "priced-review"} {
		plan := routing.Plan(routing.PlanInput{GroupID: testkit.GroupID(), RequestedModel: model, GroupMapping: routing.GroupMappingResult{MappedModel: model}})
		ctx := requeststate.WithRoutePlan(context.Background(), plan)
		_, err := target.ResolveRouting(ctx, model, false)
		if model == "codex-auto-review" {
			require.ErrorIs(t, err, pricing.ErrModelPricingUnavailable)
			require.Equal(t, admission.ModelPricingUnavailableMessage, gatewayws.EntryLocalRoutingErrorReason(model, err))
			require.False(t, gatewayws.EntryShouldReportFailure(gatewayws.EntryLocalRoutingCause(err)))
		} else {
			require.NoError(t, err)
		}
	}
	otherGroup := int64(101)
	target.root.key.GroupID = &otherGroup
	_, err := target.ResolveRouting(context.Background(), "priced-review", false)
	require.ErrorIs(t, err, pricing.ErrModelPricingUnavailable)
}
