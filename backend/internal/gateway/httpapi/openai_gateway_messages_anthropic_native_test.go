package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	time "time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 这些测试检查 api_protocol=anthropic 的国产供应商如何记录 reasoning_effort。
// /v1/messages 使用 output_config.effort，启用 thinking 且未指定 effort 时使用对应模型的默认值。

func nativeAnthropicTestProvider() *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 702,
			Name:        "kimi-native",
			Platform:    capability.PlatformKimi,
			Type:        capability.ProviderTypeAPIKey,
			Concurrency: 1,
			Credentials: map[string]any{
				"api_key":      "sk-test",
				"api_protocol": providercore.APIProtocolAnthropic,
				"api_base_urls": map[string]any{
					providercore.APIProtocolAnthropic: "http://anthropic.example",
				},
			},
		},
	}
}

func nativeAnthropicGLMTestProvider() *gatewayprovider.ExecutionProvider {
	provider := nativeAnthropicTestProvider()
	provider.Record.Name = "zhipu-native"
	provider.Record.Platform = capability.PlatformZhipu
	return provider
}

func nativeAnthropicBufferedResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"msg_1","type":"message","role":"assistant","model":"k3",` +
				`"content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn",` +
				`"usage":{"input_tokens":93,"output_tokens":16}}`,
		)),
	}
}

func nativeAnthropicStreamResponse() *http.Response {
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"k3","content":[],"stop_reason":null,"usage":{"input_tokens":93,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":16}}

event: message_stop
data: {"type":"message_stop"}

`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
}

func TestNativeAnthropicPassthroughRecordsOutputConfigEffort(t *testing.T) {
	body := []byte(`{"model":"k3","max_tokens":32,"stream":false,` +
		`"output_config":{"effort":"low"},` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &auxiliaryHTTPRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

	result, err := svc.Text.Messages(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestProvider(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "k3", result.UpstreamResponseModel)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "low", *result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughThinkingEnabledFallback(t *testing.T) {
	// 启用 thinking 且省略 effort 时，passback-required 白名单中的 k3 使用 high。
	body := []byte(`{"model":"k3","max_tokens":32,"stream":false,` +
		`"thinking":{"type":"enabled","budget_tokens":1024},` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &auxiliaryHTTPRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

	result, err := svc.Text.Messages(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestProvider(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "k3", result.UpstreamResponseModel)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "high", *result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughStreamRecordsEffort(t *testing.T) {
	body := []byte(`{"model":"k3","max_tokens":32,"stream":true,` +
		`"output_config":{"effort":"max"},` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &auxiliaryHTTPRecorder{resp: nativeAnthropicStreamResponse()}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

	result, err := svc.Text.Messages(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestProvider(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "k3", result.UpstreamResponseModel)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "max", *result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughNoEffortStaysNil(t *testing.T) {
	// 省略 output_config.effort 且未启用 thinking 时，结果为 nil。
	body := []byte(`{"model":"k3","max_tokens":32,"stream":false,` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &auxiliaryHTTPRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

	result, err := svc.Text.Messages(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestProvider(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "k3", result.UpstreamResponseModel)
	require.Nil(t, result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughNormalizesGLM53Thinking(t *testing.T) {
	tests := []struct {
		name       string
		stream     bool
		preference string
		wantEffort string
	}{
		{name: "disabled buffered", preference: `"thinking":{"type":"disabled"},`, wantEffort: "low"},
		{name: "off buffered", preference: `"thinking":{"type":"off"},`, wantEffort: "low"},
		{name: "none buffered", preference: `"thinking":{"type":"none"},`, wantEffort: "low"},
		{name: "enabled buffered", preference: `"thinking":{"type":"enabled"},`, wantEffort: "high"},
		{name: "enabled streaming", stream: true, preference: `"thinking":{"type":"enabled"},`, wantEffort: "high"},
		{name: "adaptive buffered", preference: `"thinking":{"type":"adaptive"},`, wantEffort: "high"},
		{name: "adaptive streaming", stream: true, preference: `"thinking":{"type":"adaptive"},`, wantEffort: "high"},
		{name: "minimal buffered", preference: `"output_config":{"effort":"minimal"},`, wantEffort: "low"},
		{name: "low buffered", preference: `"output_config":{"effort":"low"},`, wantEffort: "low"},
		{name: "medium streaming", stream: true, preference: `"output_config":{"effort":"medium"},`, wantEffort: "high"},
		{name: "high buffered", preference: `"output_config":{"effort":"high"},`, wantEffort: "high"},
		{name: "xhigh buffered", preference: `"output_config":{"effort":"xhigh"},`, wantEffort: "max"},
		{name: "max buffered", preference: `"output_config":{"effort":"max"},`, wantEffort: "max"},
		{name: "ultra buffered", preference: `"output_config":{"effort":"ultra"},`, wantEffort: "max"},
		{name: "output effort wins over thinking", preference: `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"},`, wantEffort: "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"glm-5.3","max_tokens":32,"stream":%t,%s"messages":[{"role":"user","content":"hi"}]}`, tt.stream, tt.preference))
			response := nativeAnthropicBufferedResponse()
			if tt.stream {
				response = nativeAnthropicStreamResponse()
			}
			upstream := &auxiliaryHTTPRecorder{resp: response}
			svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})

			_, err := svc.Text.Messages(context.Background(),
				adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicGLMTestProvider(), body, "", "")
			require.NoError(t, err)
			require.Equal(t, "enabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
			require.Equal(t, tt.wantEffort, gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
		})
	}
}

func TestNativeAnthropicPassthroughLeavesOtherThinkingUntouched(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "glm 5.3 unspecified", body: `{"model":"glm-5.3","max_tokens":32,"stream":false,"messages":[]}`},
		{name: "glm 5.2 disabled", body: `{"model":"glm-5.2","max_tokens":32,"stream":false,"thinking":{"type":"disabled"},"messages":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			upstream := &auxiliaryHTTPRecorder{resp: nativeAnthropicBufferedResponse()}
			svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
			_, err := svc.Text.Messages(context.Background(),
				adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicGLMTestProvider(), body, "", "")
			require.NoError(t, err)
			require.JSONEq(t, tt.body, string(upstream.lastBody))
		})
	}
}
