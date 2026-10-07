package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestTextPricingCompactModel 核对 Compact 的全局模型、提供商覆盖与最终结算型号。
func TestTextPricingCompactModel(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct {
			name, requested, global, providerCompact, upstream, billingSource string
			ordinary, rejected                                                bool
		}{
			{name: "unpriced_global", requested: "gpt-5.6-luna", global: "private-compact", rejected: true},
			{name: "priced_global", requested: "private-model", global: "gpt-5.6-luna", upstream: "gpt-5.6-luna"},
			{name: "provider_overrides_global", requested: "gpt-5.6-luna", global: "private-compact", providerCompact: "gpt-5.6-luna", upstream: "gpt-5.6-luna"},
			{name: "unpriced_provider_override", requested: "gpt-5.6-luna", global: "gpt-5.6-luna", providerCompact: "private-compact", rejected: true},
			{name: "requested_billing", requested: "gpt-5.6-luna", global: "private-compact", upstream: "private-compact", billingSource: routing.BillingModelSourceRequested},
			{name: "ordinary_responses", requested: "gpt-5.6-luna", global: "private-compact", upstream: "gpt-5.6-luna", ordinary: true},
		} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", tc.name, passthrough), func(t *testing.T) {
				prices := textPricingFixture(t)
				groupID := int64(100)
				key := &apikey.APIKey{GroupID: &groupID}
				source := protocol.ProtocolResponsesCompact
				path := "/v1/responses/compact"
				if tc.ordinary {
					source = protocol.ProtocolOpenAIResponses
					path = "/v1/responses"
				}
				billingSource := tc.billingSource
				if billingSource == "" {
					billingSource = routing.BillingModelSourceUpstream
				}
				mapping := routing.GroupMappingResult{MappedModel: tc.requested, BillingModelSource: billingSource}
				ctx := requeststate.WithClientProtocol(context.Background(), source)
				ctx = requeststate.WithRoutePlan(ctx, routing.Plan(routing.PlanInput{GroupID: &groupID, ClientProtocol: source, RequestedModel: tc.requested, GroupMapping: mapping}))
				response := fmt.Sprintf(`{"id":"resp-compact","object":"response","status":"completed","model":%q,"output":[],"usage":{"input_tokens":100,"output_tokens":10}}`, tc.upstream)
				transport := &auxiliaryHTTPRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}}
				executor := &UnifiedTextExecutor{Pricing: prices, OpenAI: newResponsesFixture(responsesFixtureInputs{transport: transport, compactModel: tc.global})}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
				c.Set("gateway_effective_key", key)
				target := gatewayprovider.NewExecutionProvider(&provider.Record{
					ID: 1, Platform: provider.PlatformOpenAI, Type: "apikey",
					Extra:       map[string]any{"openai_passthrough": passthrough},
					Credentials: map[string]any{"api_key": "test-key", provider.UpstreamProtocolsKey: []string{"openai_responses", "openai_responses_compact"}},
				})
				if tc.providerCompact != "" {
					target.Record.Credentials["compact_model_mapping"] = map[string]any{tc.requested: tc.providerCompact}
				}
				body := []byte(fmt.Sprintf(`{"model":%q,"input":"compact this"}`, tc.requested))
				result, err := executor.Responses(ctx, c, target, body)
				if tc.rejected {
					require.ErrorIs(t, err, admission.ErrModelPricingRejected)
					require.Nil(t, result)
					require.Nil(t, transport.lastReq)
					require.Equal(t, http.StatusBadRequest, recorder.Code)
					require.Contains(t, recorder.Body.String(), "model_pricing_unavailable")
					return
				}
				require.NoError(t, err)
				require.NotNil(t, transport.lastReq)
				require.Equal(t, tc.upstream, gjson.GetBytes(transport.lastBody, "model").String())
				// 透传在型号未改写时省略 UpstreamModel，此时从 Model 读取实际型号。
				require.Equal(t, tc.upstream, completion.ForwardResultBillingModel(result.UpstreamModel, result.Model))
				billingModel := completion.OpenAIUsageBillingModel(&completion.Result{Model: result.Model, BillingModel: result.BillingModel, UpstreamModel: result.UpstreamModel}, mapping.ToUsageFields(tc.requested, result.UpstreamModel))
				require.NoError(t, prices.Check(ctx, &groupID, billingModel))
			})
		}
	}
}

// TestTextPricingResolvesResponsesChatRoute 在解析为 Chat 的提供商上核对预检与实际转发。
func TestTextPricingResolvesResponsesChatRoute(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			prices := textPricingFixture(t)
			group := &routing.Group{ID: 100, ProtocolFallbacks: map[protocol.ProtocolID][]protocol.ProtocolID{protocol.ProtocolOpenAIResponses: {protocol.ProtocolOpenAIChatCompletions}}}
			key := &apikey.APIKey{GroupID: &group.ID, Group: group}
			mapping := routing.GroupMappingResult{MappedModel: "codex-auto-review"}
			ctx := requeststate.WithClientProtocol(context.Background(), protocol.ProtocolOpenAIResponses)
			ctx = requeststate.WithRoutePlan(ctx, routing.Plan(routing.PlanInput{Group: group, ClientProtocol: protocol.ProtocolOpenAIResponses, RequestedModel: "codex-auto-review", GroupMapping: mapping}))
			target := gatewayprovider.NewExecutionProvider(&provider.Record{
				ID: 1, Platform: provider.PlatformOpenAI, Type: "apikey",
				Extra:       map[string]any{"openai_passthrough": passthrough},
				Credentials: map[string]any{"api_key": "test-key", provider.UpstreamProtocolsKey: []string{"openai_chat_completions"}, "model_mapping": map[string]any{"codex-auto-review": "gpt-5.6-luna"}},
			})
			transport := &auxiliaryHTTPRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat-review","object":"chat.completion","model":"gpt-5.6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10}}`))}}
			executor := &UnifiedTextExecutor{Pricing: prices, OpenAI: newResponsesFixture(responsesFixtureInputs{transport: transport})}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			c.Set("gateway_effective_key", key)
			result, err := executor.Responses(ctx, c, target, []byte(`{"model":"codex-auto-review","input":"review"}`))
			require.NoError(t, err)
			require.NotNil(t, transport.lastReq)
			require.Equal(t, "/v1/chat/completions", transport.lastReq.URL.Path)
			require.Equal(t, "gpt-5.6-luna", result.BillingModel)
			require.Equal(t, "gpt-5.6-luna", result.UpstreamModel)
			require.Empty(t, target.Route.Protocol())
		})
	}
}

// TestTextPricingRouteErrorPreservesCause 将协议拒绝交给入口的路由错误处理。
func TestTextPricingRouteErrorPreservesCause(t *testing.T) {
	group := &routing.Group{ID: 100, ProtocolFallbacks: map[protocol.ProtocolID][]protocol.ProtocolID{protocol.ProtocolOpenAIResponses: {}}}
	ctx := requeststate.WithClientProtocol(context.Background(), protocol.ProtocolOpenAIResponses)
	ctx = requeststate.WithRoutePlan(ctx, routing.Plan(routing.PlanInput{Group: group, ClientProtocol: protocol.ProtocolOpenAIResponses, RequestedModel: "codex-auto-review"}))
	target := gatewayprovider.NewExecutionProvider(&provider.Record{ID: 1, Platform: provider.PlatformOpenAI, Type: "apikey", Credentials: map[string]any{provider.UpstreamProtocolsKey: []string{"openai_chat_completions"}}})
	executor := &UnifiedTextExecutor{Pricing: textPricingFixture(t)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	_, err := executor.Responses(ctx, c, target, []byte(`{"model":"codex-auto-review","input":"review"}`))
	require.Error(t, err)
	require.NotErrorIs(t, err, admission.ErrModelPricingRejected)
	require.Contains(t, err.Error(), "no enabled route")
	require.False(t, c.Writer.Written())
}
