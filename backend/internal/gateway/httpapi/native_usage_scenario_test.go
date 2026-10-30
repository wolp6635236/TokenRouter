package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	httptestkit "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi/testkit"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// native_usage_scenario_test.go exercises text execution, Anthropic output
// readers, result conversion, completion capture, and completion recording.
// Fake upstream transports supply deterministic usage while production pricing
// computes distinct cache-lifetime amounts and the observed processing tier.

// nativeUsageLogWriter captures rows produced by the production completion recorder.
type nativeUsageLogWriter struct {
	rows []*usage.UsageLog // serially captured usage rows
}

// nativeUsageFunds captures the priced command submitted by completion recording.
type nativeUsageFunds struct {
	commands []*billing.UsageBillingCommand // serially captured settlement commands
}

// nativeUsageEffects enables the recorder's financial submission for this fixture.
type nativeUsageEffects struct{}

// Apply captures one command and reports its amount as committed fixture funds.
// Calls run serially, and the returned applied result lets the production recorder
// populate the usage row from the same amount submitted for settlement.
func (f *nativeUsageFunds) Apply(_ context.Context, command *billing.UsageBillingCommand) (*billing.UsageBillingApplyResult, error) {
	f.commands = append(f.commands, command)
	return &billing.UsageBillingApplyResult{Applied: true, BalanceAmountUSD: command.BillableAmountUSD}, nil
}

// ProviderUsed accepts provider activity after this fixture's settlement attempt.
func (nativeUsageEffects) ProviderUsed(int64) {}

// InvalidateAuth accepts quota-expiry effects after this fixture's settlement attempt.
func (nativeUsageEffects) InvalidateAuth(context.Context, string) {}

// Settled accepts post-commit effects after the captured financial command succeeds.
func (nativeUsageEffects) Settled(completion.SettlementInput, *billing.UsageBillingApplyResult) {}

// Create retains the completed row for this serial scenario's assertions.
// The fixture keeps supplied row pointers for synchronous assertions; callers
// finish recording before inspecting them and do not mutate stored rows.
func (w *nativeUsageLogWriter) Create(_ context.Context, row *usage.UsageLog) (bool, error) {
	w.rows = append(w.rows, row)
	return true, nil
}

// TestNativeMessagesUsageReachesRecorder forwards Messages usage through each
// supported client representation. Completion must price uncached input once,
// preserve cache lifetimes and speed, and retain zero or absent usage as zero.
func TestNativeMessagesUsageReachesRecorder(t *testing.T) {
	for _, platform := range []string{capability.PlatformOpenCode, capability.PlatformKimi} {
		for _, client := range []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions} {
			for _, stream := range []bool{false, true} {
				for _, observation := range []struct {
					name    string              // readable usage scenario
					initial string              // initial raw usage object
					delta   string              // optional stream usage update
					want    protocol.TokenUsage // normalized completion evidence
				}{
					{name: "split_fast", initial: `{"input_tokens":100,"output_tokens":10,"cache_creation_input_tokens":12,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":7},"speed":"fast"}`, delta: `{"output_tokens":10}`, want: protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheCreationInputTokens: 12, CacheReadInputTokens: 30, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7, Speed: "fast"}},
					{name: "ttl_only_prompt_total", initial: `{"input_tokens":142,"output_tokens":10,"prompt_tokens":142,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":7}}`, delta: `{"output_tokens":10}`, want: protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 30, CacheCreationInputTokens: 12, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7}},
					{name: "compatible_prompt_total", initial: `{"input_tokens":1200,"output_tokens":30,"prompt_tokens":1200,"prompt_tokens_details":{"cached_tokens":800}}`, delta: `{"output_tokens":30}`, want: protocol.TokenUsage{InputTokens: 400, OutputTokens: 30, CacheReadInputTokens: 800}},
					{name: "zero", initial: `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}`},
					{name: "missing"},
					{name: "cache_only", initial: `{"input_tokens":0,"cache_creation_input_tokens":12,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":7}}`, want: protocol.TokenUsage{CacheCreationInputTokens: 12, CacheReadInputTokens: 30, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7}},
				} {
					t.Run(fmt.Sprintf("%s/%s/stream=%t/%s", platform, client, stream, observation.name), func(t *testing.T) {
						result, target, output, response := forwardNativeUsageScenario(t, platform, client, stream, observation.initial, observation.delta, "")
						require.NotNil(t, result.NativeUsage)
						require.Equal(t, observation.want, *result.NativeUsage)
						require.Equal(t, observation.want.InputTokens+observation.want.CacheCreationInputTokens+observation.want.CacheReadInputTokens, result.Usage.InputTokens)
						if client == protocol.ProtocolAnthropicMessages {
							require.Equal(t, response, output, "native client bytes retain usage and vendor extensions")
						} else {
							require.Contains(t, output, "ok")
							if !stream && observation.name == "split_fast" {
								path := "usage.input_tokens"
								if client == protocol.ProtocolOpenAIChatCompletions {
									path = "usage.prompt_tokens"
								}
								require.Equal(t, int64(142), gjson.Get(output, path).Int())
							}
							if stream && observation.name == "split_fast" {
								usageSeen := false
								for line := range strings.SplitSeq(output, "\n") {
									payload, ok := strings.CutPrefix(line, "data: ")
									if !ok {
										continue
									}
									path := "response.usage.input_tokens"
									if client == protocol.ProtocolOpenAIChatCompletions {
										path = "usage.prompt_tokens"
									}
									if count := gjson.Get(payload, path); count.Exists() {
										require.Equal(t, int64(142), count.Int())
										usageSeen = true
									}
								}
								require.True(t, usageSeen, "converted stream emits inclusive client usage")
							}
						}
						assertNativeUsageRecorded(t, result, target, observation.want)
					})
				}
			}
		}
	}
}

// TestNativeMessagesUsageTTLZeroUpdate clears an earlier one-hour observation
// through a later stream event. Every client output mode must retain that zero
// while omitted prompt and five-minute data keep their previous observations.
func TestNativeMessagesUsageTTLZeroUpdate(t *testing.T) {
	for _, client := range []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions} {
		for _, stream := range []bool{false, true} {
			if client == protocol.ProtocolAnthropicMessages && !stream {
				continue
			}
			t.Run(fmt.Sprintf("%s/stream=%t", client, stream), func(t *testing.T) {
				result, target, _, _ := forwardNativeUsageScenario(t, capability.PlatformOpenCode, client, stream, `{"input_tokens":100,"cache_creation_input_tokens":12,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":7},"speed":"fast"}`, `{"output_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":12,"ephemeral_1h_input_tokens":0}}`, "")
				want := protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheCreationInputTokens: 12, CacheReadInputTokens: 30, CacheCreation5mTokens: 12, Speed: "fast"}
				require.Equal(t, want, *result.NativeUsage)
				assertNativeUsageRecorded(t, result, target, want)
			})
		}
	}
}

// TestNativeMessagesUsagePartialFailures interrupts streams after usage arrives.
// Native incomplete streams and converted idle timeouts retain evidence through
// capture and recording, while their execution errors remain visible to callers.
func TestNativeMessagesUsagePartialFailures(t *testing.T) {
	initial := `{"input_tokens":100,"cache_creation_input_tokens":12,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":7},"speed":"fast"}`
	delta := `{"output_tokens":10}`
	want := protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheCreationInputTokens: 12, CacheReadInputTokens: 30, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7, Speed: "fast"}
	t.Run("native_incomplete", func(t *testing.T) {
		result, target, _, _ := forwardNativeUsageScenario(t, capability.PlatformOpenCode, protocol.ProtocolAnthropicMessages, true, initial, delta, "incomplete")
		require.Equal(t, want, *result.NativeUsage)
		assertNativeUsageRecorded(t, result, target, want)
	})
	for _, client := range []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions} {
		for _, stream := range []bool{false, true} {
			if client == protocol.ProtocolAnthropicMessages && !stream {
				continue
			}
			t.Run(fmt.Sprintf("%s/timeout/stream=%t", client, stream), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					result, target, _, _ := forwardNativeUsageScenario(t, capability.PlatformOpenCode, client, stream, initial, delta, "timeout")
					require.Equal(t, want, *result.NativeUsage)
					assertNativeUsageRecorded(t, result, target, want)
				})
			})
		}
	}
}

// TestNativeMessagesUsageClientDisconnect drains final usage after downstream
// writing fails. Each streaming client must return the complete cache evidence
// and mark disconnection while the recorder receives the final output count.
func TestNativeMessagesUsageClientDisconnect(t *testing.T) {
	for _, client := range []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions} {
		t.Run(string(client), func(t *testing.T) {
			result, target, _, _ := forwardNativeUsageScenario(t, capability.PlatformOpenCode, client, true, `{"input_tokens":100,"cache_creation_input_tokens":12,"cache_read_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":7},"speed":"fast"}`, `{"output_tokens":10}`, "disconnect")
			want := protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheCreationInputTokens: 12, CacheReadInputTokens: 30, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7, Speed: "fast"}
			require.True(t, result.ClientDisconnect)
			require.Equal(t, want, *result.NativeUsage)
			assertNativeUsageRecorded(t, result, target, want)
		})
	}
}

// TestNativeMessagesUsageMalformedSuccessKeepsReaderPolicy supplies valid JSON
// with an unusable token field. Native JSON retains its empty observation;
// buffered conversion rejects the undecodable start event and returns no result.
func TestNativeMessagesUsageMalformedSuccessKeepsReaderPolicy(t *testing.T) {
	malformed := `{"input_tokens":"unavailable","output_tokens":null}`
	t.Run("native_json", func(t *testing.T) {
		result, target, output, response := forwardNativeUsageScenario(t, capability.PlatformOpenCode, protocol.ProtocolAnthropicMessages, false, malformed, "", "")
		require.Equal(t, response, output)
		require.Equal(t, protocol.TokenUsage{}, *result.NativeUsage)
		assertNativeUsageRecorded(t, result, target, protocol.TokenUsage{})
	})
	for _, client := range []protocol.ProtocolID{protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions} {
		t.Run(string(client), func(t *testing.T) {
			result, _, _, _ := forwardNativeUsageScenario(t, capability.PlatformOpenCode, client, false, malformed, "", "invalid_usage")
			require.Nil(t, result)
		})
	}
}

// forwardNativeUsageScenario executes a configured Messages route with a fake
// response transport. Converted clients consume SSE upstream even when their
// downstream response is buffered; native buffered clients receive raw JSON.
// Timeout cases leave a pipe open after complete usage frames so the production
// idle timer owns read cancellation and result publication. The execution caller
// closes that reader, while this fixture closes the writer after return. The
// invalid-usage case expects buffered decoding to fail before a message can be
// reconstructed; successful native bodies retain their supplied client bytes.
func forwardNativeUsageScenario(t *testing.T, platform string, client protocol.ProtocolID, stream bool, initial, delta, failure string) (*forward.OpenAIResult, *gatewayprovider.ExecutionProvider, string, string) {
	t.Helper()
	model := "kimi-k2.5"
	target := gatewayprovider.NewExecutionProvider(&providercore.Record{ID: 17, Platform: platform, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", providercore.UpstreamProtocolsKey: []string{"anthropic_messages"}, providercore.OpenCodeModelProtocolsKey: map[string]any{"payg": map[string]any{model: "anthropic_messages"}}}})
	path := "/v1/messages"
	body := fmt.Sprintf(`{"model":%q,"stream":%t,"max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`, model, stream)
	if client == protocol.ProtocolOpenAIResponses {
		path = "/v1/responses"
		body = fmt.Sprintf(`{"model":%q,"stream":%t,"input":"hello"}`, model, stream)
	}
	if client == protocol.ProtocolOpenAIChatCompletions {
		path = "/v1/chat/completions"
		body = strings.TrimSuffix(body, "}") + `,"stream_options":{"include_usage":true}}`
	}
	ctx := requeststate.WithClientProtocol(t.Context(), client)
	ctx = requeststate.WithRoutePlan(ctx, routing.Plan(routing.PlanInput{RequestedModel: model, ClientProtocol: client}))
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	if failure == "disconnect" {
		c.Writer = &httptestkit.FailingWriter{ResponseWriter: c.Writer, FailAfter: 0}
	}
	usageField := ""
	if initial != "" {
		usageField = `,"usage":` + initial
	}
	message := fmt.Sprintf(`{"id":"msg-usage","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","vendor_extension":"retained"%s}`, model, usageField)
	response := message
	contentType := "application/json"
	if stream || client != protocol.ProtocolAnthropicMessages {
		contentType = "text/event-stream"
		streamMessage := strings.Replace(message, `"content":[{"type":"text","text":"ok"}]`, `"content":[]`, 1)
		response = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + streamMessage + "}\n\n"
		response += "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
		response += "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"
		response += "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
		if delta != "" {
			response += "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":" + delta + "}\n\n"
		}
		if failure == "" || failure == "disconnect" {
			response += "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		}
	}
	transport := &auxiliaryHTTPRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}, "X-Request-Id": []string{"native-usage"}}, Body: io.NopCloser(strings.NewReader(response))}}
	options := protocolHTTPOptions()
	if failure == "timeout" {
		options.Response = OpenAIResponseOptions{Configured: true, StreamDataIntervalTimeout: 1}
		reader, writer := io.Pipe()
		transport.resp.Body = reader
		// Keep the pipe open after complete usage frames, allowing the production
		// idle timer to close the reader while the writer has no terminal event.
		go func() { _, _ = io.WriteString(writer, response) }()
		defer func() {
			require.NoError(t, writer.Close())
		}()
	}
	svc := newResponsesFixture(responsesFixtureInputs{options: options, transport: transport})
	var result *forward.OpenAIResult
	var err error
	switch client {
	case protocol.ProtocolAnthropicMessages:
		result, err = svc.Text.Messages(ctx, c, target, []byte(body), "", "")
	case protocol.ProtocolOpenAIResponses:
		result, err = svc.Forward(ctx, c, target, []byte(body))
	case protocol.ProtocolOpenAIChatCompletions:
		result, err = svc.Text.Chat(ctx, c, target, []byte(body), "", "")
	}
	if failure != "" && failure != "disconnect" {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}
	if failure == "invalid_usage" {
		require.Nil(t, result)
	} else {
		require.NotNil(t, result)
	}
	require.NotNil(t, transport.lastReq)
	require.True(t, strings.HasSuffix(transport.lastReq.URL.Path, "/messages"))
	return result, target, writer.Body.String(), response
}

// assertNativeUsageRecorded captures execution evidence and records it through
// production pricing. Distinct five-minute/hour rates and a fast multiplier
// expose either discarded evidence or repeated subtraction of cache tokens.
// Input costs three dollars per million tokens, output costs four, and cache
// reads cost half a dollar. Five-minute writes cost one dollar and hourly
// writes cost two; fast usage doubles those amounts. The captured settlement
// command and persisted row must match the independently calculated total.
func assertNativeUsageRecorded(t *testing.T, result *forward.OpenAIResult, target *gatewayprovider.ExecutionProvider, want protocol.TokenUsage) {
	t.Helper()
	payer := &identity.User{ID: 1}
	groupID := int64(1)
	input := gatewayprovider.CaptureOpenAI(t.Context(), &gatewayprovider.OpenAICapture{Result: result, APIKey: &apikey.APIKey{ID: 2, User: payer, GroupID: &groupID, Group: &routing.Group{ID: groupID, RateMultiplier: 1}}, User: payer, Provider: &target.Record})
	require.True(t, input.Result.NativeUsage)
	require.Equal(t, want.CacheCreation1hTokens, input.Result.Usage.CacheCreation1hTokens)
	require.Equal(t, want.Speed, input.Result.Usage.Speed)
	result.NativeUsage.InputTokens = 999
	require.Equal(t, want.InputTokens, input.Result.Usage.InputTokens)
	fast := 2.0
	calculator := billingtestkit.Calculator(nil, map[string]*pricing.ModelPricing{result.Model: {InputPricePerToken: 3e-6, OutputPricePerToken: 4e-6, CacheReadPricePerToken: 0.5e-6, CacheCreation5mPrice: 1e-6, CacheCreation1hPrice: 2e-6, SupportsCacheBreakdown: true, FastMultiplier: &fast}})
	resolver := billingtestkit.SharedPriceResolver(calculator, groupID, pricing.DefaultBillingSettings(), nil)
	logs := &nativeUsageLogWriter{}
	funds := &nativeUsageFunds{}
	recorder := completion.NewRecorder(completion.Dependencies{Calculator: calculator, Prices: resolver, Logs: logs, Funds: funds, Effects: nativeUsageEffects{}}, completion.RecorderOptions{DefaultMultiplier: 1})
	require.NoError(t, recorder.Record(t.Context(), input, true))
	require.Len(t, logs.rows, 1)
	row := logs.rows[0]
	require.Equal(t, want.InputTokens, row.InputTokens)
	require.Equal(t, want.OutputTokens, row.OutputTokens)
	require.Equal(t, want.CacheReadInputTokens, row.CacheReadTokens)
	require.Equal(t, want.CacheCreationInputTokens, row.CacheCreationTokens)
	require.Equal(t, want.CacheCreation5mTokens, row.CacheCreation5mTokens)
	require.Equal(t, want.CacheCreation1hTokens, row.CacheCreation1hTokens)
	multiplier := 1.0
	if want.Speed == "fast" {
		multiplier = fast
		require.NotNil(t, row.ServiceTier)
		require.Equal(t, "priority", *row.ServiceTier)
	}
	// Expected prices use USD per token; tolerance permits floating-point arithmetic rounding.
	cacheCost := float64(want.CacheCreation5mTokens)*1e-6 + float64(want.CacheCreation1hTokens)*2e-6
	wantCost := (float64(want.InputTokens)*3e-6 + float64(want.OutputTokens)*4e-6 + float64(want.CacheReadInputTokens)*0.5e-6 + cacheCost) * multiplier
	require.InDelta(t, wantCost, row.TotalCost, 1e-12)
	require.Len(t, funds.commands, 1)
	require.InDelta(t, wantCost, funds.commands[0].BillableAmountUSD, 1e-12)
	require.InDelta(t, wantCost, row.ActualCost, 1e-12)
	require.InDelta(t, cacheCost*multiplier, row.CacheCreationCost, 1e-12)
	if wantCost > 0 {
		require.NotNil(t, row.ProviderStatsCost)
		require.InDelta(t, wantCost, *row.ProviderStatsCost, 1e-12)
	} else {
		require.Nil(t, row.ProviderStatsCost)
	}
	require.Equal(t, target.Record.Platform, row.Platform)
}
