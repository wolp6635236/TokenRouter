package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/googleforward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/messageforward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/searchtools"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestUnifiedTextDispatchUsesSelectedProvider 只装配实际提供商需要的执行器，错误分派会立即失败。
func TestUnifiedTextDispatchUsesSelectedProvider(t *testing.T) {
	tests := []struct{ platform, model, path, response string }{
		{"anthropic", "claude-sonnet-4-5", "/v1/messages", `{"id":"msg-native","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_creation_input_tokens":7,"cache_read_input_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":5},"speed":"fast"}}`},
		{"gemini", "gemini-2.5-flash", ":generateContent", `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"totalTokenCount":13}}`},
		{"openai", "gpt-5.4", "/v1/responses", `{"id":"resp-native","object":"response","status":"completed","model":"gpt-5.4","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}`},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			transport := &auxiliaryHTTPRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tt.response))}}
			target := gatewayadapter.NewExecutionProvider(&provider.Record{ID: 42, Name: "selected", Platform: tt.platform, Type: "apikey", Concurrency: 1, Credentials: map[string]any{"api_key": "test-key"}})
			executor := &UnifiedTextExecutor{}
			switch tt.platform {
			case "anthropic":
				executor.Anthropic = NewMessagesExecutor(messageforward.NewRuntime(messageforward.Dependencies{Search: searchtools.NewEmulator(unifiedSearchSource{}, nil, nil, nil, nil, nil), Credentials: &provider.MessageCredentialSource{}, Transport: transport, Deferred: &provider.DeferredService{}}, messageforward.Options{Configured: true, ResponseReadLimit: 1 << 20}), nil)
			case "gemini":
				executor.Gemini = &GeminiExecutor{Runtime: &googleforward.Gemini{Transport: transport, Options: googleforward.Options{Configured: true, ResponseReadLimit: 1 << 20}}}
			case "openai":
				executor.OpenAI = newResponsesFixture(responsesFixtureInputs{transport: transport})
			}
			var result *forward.OpenAIResult
			var err error
			if tt.platform == "openai" {
				result, err = executor.Responses(context.Background(), c, target, []byte(`{"model":"`+tt.model+`","input":"hello"}`))
			} else {
				result, err = executor.Messages(context.Background(), c, target, []byte(`{"model":"`+tt.model+`","max_tokens":20,"messages":[{"role":"user","content":"hello"}]}`), "", tt.model)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Contains(t, transport.lastReq.URL.Path, tt.path)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), "ok")
			if tt.platform == "openai" {
				require.Nil(t, result.NativeUsage)
			} else {
				require.NotNil(t, result.NativeUsage)
				require.Equal(t, 10, result.NativeUsage.InputTokens)
			}
		})
	}
}

type unifiedRecordWriter struct{ rows []*usage.UsageLog }

func (w *unifiedRecordWriter) Create(_ context.Context, row *usage.UsageLog) (bool, error) {
	w.rows = append(w.rows, row)
	return true, nil
}

type unifiedRecordEffects struct{}

func (unifiedRecordEffects) ProviderUsed(int64)                                                   {}
func (unifiedRecordEffects) InvalidateAuth(context.Context, string)                               {}
func (unifiedRecordEffects) Settled(completion.SettlementInput, *billing.UsageBillingApplyResult) {}

type unifiedRecordModels struct{}

func (unifiedRecordModels) Candidates(model string, _ ...string) []string { return []string{model} }

// TestUnifiedNativeUsageSurvivesCaptureAndRecord 验证统一结果、异步快照及最终计费仍使用原生输入桶。
func TestUnifiedNativeUsageSurvivesCaptureAndRecord(t *testing.T) {
	tier := "priority"
	native := &forward.MessagesResult{RequestID: "native-usage", Model: "claude-sonnet-4-5", UpstreamModel: "claude-sonnet-4-5", Usage: protocol.TokenUsage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 30, CacheCreationInputTokens: 12, CacheCreation5mTokens: 5, CacheCreation1hTokens: 7, Speed: "fast"}, Stream: true, ClientDisconnect: true, ServiceTier: &tier, Duration: time.Second}
	result := nativeTextResult(native, "/v1/messages")
	require.NotNil(t, result.NativeUsage)
	require.Equal(t, native.Usage, *result.NativeUsage)
	require.True(t, result.ClientDisconnect)
	native.Usage.CacheCreation1hTokens = 99
	require.Equal(t, 7, result.NativeUsage.CacheCreation1hTokens)
	user := &identity.User{ID: 1}
	input := gatewayadapter.CaptureOpenAI(context.Background(), &gatewayadapter.OpenAICapture{Result: result, APIKey: &apikey.APIKey{ID: 2, User: user}, User: user, Provider: &provider.Record{ID: 3, Platform: "anthropic", Type: "apikey"}})
	require.True(t, input.Result.NativeUsage)
	require.Equal(t, 5, input.Result.Usage.CacheCreation5mTokens)
	require.Equal(t, 7, input.Result.Usage.CacheCreation1hTokens)
	require.Equal(t, "fast", input.Result.Usage.Speed)
	result.NativeUsage.InputTokens = 999
	require.Equal(t, 100, input.Result.Usage.InputTokens)
	logs := &unifiedRecordWriter{}
	recorder := completion.NewRecorder(completion.Dependencies{Calculator: billing.NewCalculator(nil, billing.CalculatorOptions{}), Funds: unifiedRecordFunds{}, Logs: logs, Effects: unifiedRecordEffects{}, Models: unifiedRecordModels{}}, completion.RecorderOptions{DefaultMultiplier: 1})
	require.NoError(t, recorder.Record(context.Background(), input, true))
	require.Len(t, logs.rows, 1)
	row := logs.rows[0]
	require.Equal(t, 100, row.InputTokens, "统一OpenAI外壳不能扣减原生缓存桶")
	require.Equal(t, 30, row.CacheReadTokens)
	require.Equal(t, 12, row.CacheCreationTokens)
	require.Equal(t, 5, row.CacheCreation5mTokens)
	require.Equal(t, 7, row.CacheCreation1hTokens)
	require.Equal(t, "anthropic", row.Platform)
	require.Equal(t, "priority", *row.ServiceTier)

	// 同一数值标记为 OpenAI 总输入时，扣除缓存读取用量。
	input.Result.NativeUsage = false
	require.NoError(t, recorder.Record(context.Background(), input, true))
	require.Len(t, logs.rows, 2)
	require.Equal(t, 58, logs.rows[1].InputTokens)
}

type unifiedSearchSource struct{}

func (unifiedSearchSource) Current() searchtools.Searcher { return nil }

// unifiedRecordFunds 在协议用量测试中通过完成流程调用结算接口。
type unifiedRecordFunds struct{}

func (unifiedRecordFunds) Apply(_ context.Context, command *billing.UsageBillingCommand) (*billing.UsageBillingApplyResult, error) {
	return &billing.UsageBillingApplyResult{Applied: true, BalanceAmountUSD: command.BillableAmountUSD}, nil
}
