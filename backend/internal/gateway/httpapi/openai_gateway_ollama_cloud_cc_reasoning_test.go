package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	time "time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/ollama"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func ollamaCloudRawChatCompletionsTestProvider() *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 143,
			Name:     "DeepSeek Ollama",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"api_key":  "sk-test",
				"base_url": "https://ollama.com",
			},
			Extra: map[string]any{
				providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions),
			},
		},
	}
}

func TestIsOllamaCloudRawChatCompletionsProvider(t *testing.T) {
	t.Parallel()

	t.Run("ollama.com + force_chat_completions", func(t *testing.T) {
		t.Parallel()
		require.True(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(ollamaCloudRawChatCompletionsTestProvider()))
	})

	t.Run("ollama.com + historical probe is ignored", func(t *testing.T) {
		t.Parallel()
		provider := ollamaCloudRawChatCompletionsTestProvider()
		provider.Record.Extra = map[string]any{
			providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModePreserveClientProtocol),
		}
		require.False(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(provider))
	})

	t.Run("extra usage signal without ollama host", func(t *testing.T) {
		t.Parallel()
		provider := rawChatCompletionsTestProvider()
		provider.Record.Credentials["base_url"] = "https://example.invalid/v1"
		provider.Record.Extra = map[string]any{
			providercore.ExtraKeyTextRouteMode:            string(providercore.TextRouteModeForceChatCompletions),
			providercore.OllamaCloudUsageSnapshotExtraKey: map[string]any{"status": "ok"},
		}
		require.True(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(provider))
	})

	t.Run("official DeepSeek", func(t *testing.T) {
		t.Parallel()
		provider := rawChatCompletionsTestProvider()
		provider.Record.Name = "DeepSeek"
		provider.Record.Credentials["base_url"] = "https://api.deepseek.com"
		provider.Record.Extra = map[string]any{
			providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions),
		}
		require.False(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(provider))
	})

	t.Run("OpenCode Go extra", func(t *testing.T) {
		t.Parallel()
		provider := rawChatCompletionsTestProvider()
		provider.Record.Credentials["base_url"] = "https://opencode.ai/zen/go/v1"
		provider.Record.Extra = map[string]any{
			providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions),
			"opencode_go_usage_auto_refresh":   true,
		}
		require.False(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(provider))
	})

	t.Run("ollama.com without force_chat_completions", func(t *testing.T) {
		t.Parallel()
		provider := ollamaCloudRawChatCompletionsTestProvider()
		provider.Record.Extra = nil
		require.False(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(provider))
	})

	t.Run("anthropic ollama.com", func(t *testing.T) {
		t.Parallel()
		provider := ollamaCloudRawChatCompletionsTestProvider()
		provider.Record.Platform = capability.PlatformAnthropic
		require.False(t, gatewayprovider.IsOllamaCloudRawChatCompletionsProvider(provider))
	})
}

func TestNormalizeOllamaCloudChatCompletionsResponseJSON(t *testing.T) {
	t.Parallel()

	t.Run("copies delta.reasoning to reasoning_content", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"choices":[{"delta":{"reasoning":"abc"}}]}`)
		out := ollama.NormalizeOllamaCloudChatCompletionsResponseJSON(in)
		require.Equal(t, "abc", gjson.GetBytes(out, "choices.0.delta.reasoning").String())
		require.Equal(t, "abc", gjson.GetBytes(out, "choices.0.delta.reasoning_content").String())
	})

	t.Run("copies message.thinking to reasoning_content", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"choices":[{"message":{"thinking":"abc"}}]}`)
		out := ollama.NormalizeOllamaCloudChatCompletionsResponseJSON(in)
		require.Equal(t, "abc", gjson.GetBytes(out, "choices.0.message.thinking").String())
		require.Equal(t, "abc", gjson.GetBytes(out, "choices.0.message.reasoning_content").String())
	})

	t.Run("does not overwrite existing reasoning_content", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"choices":[{"delta":{"reasoning":"new","reasoning_content":"old"}}]}`)
		out := ollama.NormalizeOllamaCloudChatCompletionsResponseJSON(in)
		require.Equal(t, string(in), string(out))
		require.Equal(t, "old", gjson.GetBytes(out, "choices.0.delta.reasoning_content").String())
	})

	t.Run("empty reasoning does not open reasoning_content", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"choices":[{"delta":{"reasoning":""}}]}`)
		out := ollama.NormalizeOllamaCloudChatCompletionsResponseJSON(in)
		require.Equal(t, string(in), string(out))
		require.False(t, gjson.GetBytes(out, "choices.0.delta.reasoning_content").Exists())
	})

	t.Run("empty thinking does not open reasoning_content", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"choices":[{"message":{"thinking":""}}]}`)
		out := ollama.NormalizeOllamaCloudChatCompletionsResponseJSON(in)
		require.Equal(t, string(in), string(out))
		require.False(t, gjson.GetBytes(out, "choices.0.message.reasoning_content").Exists())
	})

	t.Run("tool call chunk is unchanged", func(t *testing.T) {
		t.Parallel()
		in := []byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`)
		out := ollama.NormalizeOllamaCloudChatCompletionsResponseJSON(in)
		require.Equal(t, string(in), string(out))
	})
}

func TestNormalizeOllamaCloudChatCompletionsRequest(t *testing.T) {
	t.Parallel()

	in := []byte(`{"messages":[{"role":"user","content":"weather"},{"role":"assistant","reasoning_content":"prev","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}]}`)
	out := ollama.NormalizeOllamaCloudChatCompletionsRequest(in)
	require.Equal(t, "prev", gjson.GetBytes(out, "messages.1.reasoning").String())
	require.Equal(t, "prev", gjson.GetBytes(out, "messages.1.reasoning_content").String())
	require.Equal(t, "", gjson.GetBytes(out, "messages.1.content").String())
	require.Equal(t, "get_weather", gjson.GetBytes(out, "messages.1.tool_calls.0.function.name").String())
	require.False(t, gjson.GetBytes(out, "messages.0.reasoning").Exists())
}

func TestApplyOllamaCloudRawChatCompletionsLeavesForeignProvidersUnchanged(t *testing.T) {
	t.Parallel()

	reqBody := []byte(`{"messages":[{"role":"assistant","reasoning_content":"prev","content":""}]}`)
	respBody := []byte(`{"choices":[{"delta":{"reasoning":"abc"}}]}`)
	sseLine := `data: {"choices":[{"delta":{"reasoning":"abc"}}]}`

	official := rawChatCompletionsTestProvider()
	official.Record.Name = "DeepSeek"
	official.Record.Credentials["base_url"] = "https://api.deepseek.com"
	official.Record.Extra = map[string]any{
		providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions),
	}

	opencode := rawChatCompletionsTestProvider()
	opencode.Record.Credentials["base_url"] = "https://opencode.ai/zen/go/v1"
	opencode.Record.Extra = map[string]any{
		providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions),
		"opencode_go_usage_auto_refresh":   true,
	}

	for _, provider := range []*gatewayprovider.ExecutionProvider{official, opencode} {
		require.Equal(t, reqBody, gatewayprovider.ApplyOllamaCloudRawChatCompletionsRequest(provider, reqBody))
		require.Equal(t, respBody, gatewayprovider.ApplyOllamaCloudRawChatCompletionsResponse(provider, respBody))
		require.Equal(t, sseLine, gatewayprovider.ApplyOllamaCloudRawChatCompletionsSSELine(provider, sseLine))
	}
}

func TestNormalizeOllamaCloudChatCompletionsSSELine(t *testing.T) {
	t.Parallel()

	before := `data: {"choices":[{"delta":{"reasoning":"abc"}}]}`
	after := ollama.NormalizeOllamaCloudChatCompletionsSSELine(before)
	require.True(t, strings.HasPrefix(after, "data: "))
	payload := strings.TrimPrefix(after, "data: ")
	require.Equal(t, "abc", gjson.Get(payload, "choices.0.delta.reasoning").String())
	require.Equal(t, "abc", gjson.Get(payload, "choices.0.delta.reasoning_content").String())
	require.Equal(t, "data: [DONE]", ollama.NormalizeOllamaCloudChatCompletionsSSELine("data: [DONE]"))
}

func TestForwardAsRawChatCompletions_OllamaCloudReasoningAliasStreaming(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_ollama","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_ollama","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"reasoning":"abc"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_ollama","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"final answer"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_ollama","object":"chat.completion.chunk","model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":4}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &auxiliaryHTTPRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_ollama_reasoning_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}

	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

	result, err := svc.Text.RawChat(context.Background(), c, ollamaCloudRawChatCompletionsTestProvider(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), `"reasoning":"abc"`)
	require.Contains(t, rec.Body.String(), `"reasoning_content":"abc"`)
	require.Contains(t, rec.Body.String(), `"content":"final answer"`)
	require.Contains(t, rec.Body.String(), `"reasoning_tokens":4`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestForwardAsRawChatCompletions_OllamaCloudThinkingAliasNonStreaming(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"},{"role":"assistant","reasoning_content":"prev","content":""}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamJSON := `{"id":"chatcmpl_ollama","object":"chat.completion","model":"deepseek-v4-pro","choices":[{"index":0,"message":{"role":"assistant","thinking":"abc","content":"final answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":4}}}`
	upstream := &auxiliaryHTTPRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_ollama_thinking_json"}},
		Body:       io.NopCloser(strings.NewReader(upstreamJSON)),
	}}

	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

	result, err := svc.Text.RawChat(context.Background(), c, ollamaCloudRawChatCompletionsTestProvider(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "prev", gjson.GetBytes(upstream.lastBody, "messages.1.reasoning").String())
	require.Equal(t, "prev", gjson.GetBytes(upstream.lastBody, "messages.1.reasoning_content").String())
	require.Equal(t, "abc", gjson.Get(rec.Body.String(), "choices.0.message.thinking").String())
	require.Equal(t, "abc", gjson.Get(rec.Body.String(), "choices.0.message.reasoning_content").String())
	require.Equal(t, "final answer", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
	require.Equal(t, int64(4), gjson.Get(rec.Body.String(), "usage.completion_tokens_details.reasoning_tokens").Int())
}
