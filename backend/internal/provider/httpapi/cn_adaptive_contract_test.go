package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
	upstreamcore "github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func adaptiveCNProviderTestProvider(id int64, platform string) *providercore.Record {
	return &providercore.Record{
		ID:          id,
		Name:        "adaptive-cn-test",
		Platform:    platform,
		Type:        capability.ProviderTypeAPIKey,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "sk-adaptive-test",
			"api_protocol": providercore.APIProtocolAdaptive,
			"api_base_urls": map[string]any{
				providercore.APIProtocolChatCompletions: "http://chat.example/v1",
				providercore.APIProtocolAnthropic:       "http://anthropic.example",
				providercore.APIProtocolResponses:       "http://responses.example",
			},
		},
	}
}

func adaptiveCNProviderTestService(provider *providercore.Record, responses ...*http.Response) (*provideradapter.OpenAIProviderTest, *openAIProbeTransport) {
	repo := &openAIProbeStore{
		openAIProbeRecords: openAIProbeRecords{
			providersByID: map[int64]*providercore.Record{provider.ID: provider},
		},
	}
	upstream := &openAIProbeTransport{responses: responses}
	return &provideradapter.OpenAIProviderTest{
		Store:       repo,
		Transport:   upstream,
		ValidateURL: (egress.OperatorURLPolicy{AllowInsecureHTTP: true}).Validate,
	}, upstream
}

func adaptiveCNChatTestResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(`data: {"choices":[{"delta":{"content":"chat ok"},"finish_reason":"stop"}]}

data: [DONE]

`)),
	}
}

func adaptiveCNAnthropicTestResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(`data: {"type":"content_block_delta","delta":{"text":"anthropic ok"}}

data: {"type":"message_stop"}

`)),
	}
}

func adaptiveCNResponsesTestResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"responses ok"}

data: {"type":"response.completed"}

`)),
	}
}

func TestProviderTestService_AdaptiveChatOnlyProvidersTestChatAndAnthropicEndpoints(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(301, capability.PlatformZhipu)
	svc, upstream := adaptiveCNProviderTestService(
		provider,
		adaptiveCNChatTestResponse(),
		adaptiveCNAnthropicTestResponse(),
	)
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "glm-4.7", "hello", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "http://chat.example/v1/chat/completions", upstream.requests[0].URL.String())
	require.Equal(t, "http://anthropic.example/v1/messages", upstream.requests[1].URL.String())
	require.Equal(t, "Bearer sk-adaptive-test", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "sk-adaptive-test", anthropic.GetHeaderRaw(upstream.requests[1].Header, "x-api-key"))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_start"`))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
	require.Contains(t, recorder.Body.String(), "已通过原生 /v1/messages 验证")
}

func TestProviderTestService_AdaptiveDeepSeekAlsoTestsResponsesEndpoint(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(302, capability.PlatformDeepseek)
	svc, upstream := adaptiveCNProviderTestService(
		provider,
		adaptiveCNChatTestResponse(),
		adaptiveCNAnthropicTestResponse(),
		adaptiveCNResponsesTestResponse(),
	)
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "deepseek-chat", "", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "http://responses.example/responses", upstream.requests[2].URL.String())
	require.Equal(t, upstreamcore.HTTPUpstreamProfileOpenAI, upstreamcore.HTTPUpstreamProfileFromContext(upstream.requests[2].Context()))
	require.Equal(t, "Bearer sk-adaptive-test", upstream.requests[2].Header.Get("Authorization"))
	require.True(t, gjson.GetBytes(upstream.bodies[2], "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.bodies[2], "store").Bool())
	require.False(t, gjson.GetBytes(upstream.bodies[2], "instructions").Exists())
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
	require.Contains(t, recorder.Body.String(), "已通过原生 /responses 验证")
}

func TestProviderTestService_AdaptiveKimiAlsoTestsResponsesEndpoint(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(306, capability.PlatformKimi)
	svc, upstream := adaptiveCNProviderTestService(
		provider,
		adaptiveCNChatTestResponse(),
		adaptiveCNAnthropicTestResponse(),
		adaptiveCNResponsesTestResponse(),
	)
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "k3-256k", "", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "http://responses.example/v1/responses", upstream.requests[2].URL.String())
	require.Equal(t, upstreamcore.HTTPUpstreamProfileOpenAI, upstreamcore.HTTPUpstreamProfileFromContext(upstream.requests[2].Context()))
	require.Equal(t, "Bearer sk-adaptive-test", upstream.requests[2].Header.Get("Authorization"))
	require.True(t, gjson.GetBytes(upstream.bodies[2], "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.bodies[2], "store").Bool())
	require.False(t, gjson.GetBytes(upstream.bodies[2], "instructions").Exists())
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
	require.Contains(t, recorder.Body.String(), "已通过原生 /responses 验证")
}

func TestProviderTestService_AdaptiveStopsAndNamesFailingEndpoint(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(303, capability.PlatformDeepseek)
	svc, upstream := adaptiveCNProviderTestService(
		provider,
		adaptiveCNChatTestResponse(),
		newJSONResponse(http.StatusNotFound, `{"error":{"message":"missing messages route"}}`),
	)
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "deepseek-chat", "", providercore.ProviderTestModeDefault)

	require.Error(t, err)
	require.Contains(t, err.Error(), "Adaptive Anthropic endpoint returned 404")
	require.Len(t, upstream.requests, 2)
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.NotContains(t, recorder.Body.String(), `"type":"test_complete"`)
}

func TestProviderTestService_AdaptiveRejectsInvalidAnthropicSuccessBody(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(305, capability.PlatformKimi)
	svc, upstream := adaptiveCNProviderTestService(
		provider,
		adaptiveCNChatTestResponse(),
		newJSONResponse(http.StatusOK, `<html>not an Anthropic stream</html>`),
	)
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "kimi-k2.5", "", providercore.ProviderTestModeDefault)

	require.Error(t, err)
	require.Contains(t, err.Error(), "Adaptive Anthropic stream ended before message_stop")
	require.Len(t, upstream.requests, 2)
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.NotContains(t, recorder.Body.String(), `"type":"test_complete"`)
}

func TestProviderTestService_FixedCNChatProtocolStillTestsOnlyChatEndpoint(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(304, capability.PlatformZhipu)
	provider.Credentials["api_protocol"] = providercore.APIProtocolChatCompletions
	provider.Credentials["base_url"] = "http://fixed-chat.example/v1"
	svc, upstream := adaptiveCNProviderTestService(provider, adaptiveCNChatTestResponse())
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "glm-4.7", "", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "http://fixed-chat.example/v1/chat/completions", upstream.requests[0].URL.String())
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
}

func TestProviderTestService_FixedCNAnthropicUsesNativeEndpointWithoutBetaQuery(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(306, capability.PlatformZhipu)
	provider.Credentials["api_protocol"] = providercore.APIProtocolAnthropic
	provider.Credentials["base_url"] = "https://open.bigmodel.cn/api/anthropic"
	svc, upstream := adaptiveCNProviderTestService(provider, adaptiveCNAnthropicTestResponse())
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "glm-4.7", "", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://open.bigmodel.cn/api/anthropic/v1/messages", upstream.requests[0].URL.String())
	require.Empty(t, upstream.requests[0].URL.RawQuery)
	require.Equal(t, "sk-adaptive-test", anthropic.GetHeaderRaw(upstream.requests[0].Header, "x-api-key"))
	require.Contains(t, recorder.Body.String(), `"type":"test_complete"`)
}

func TestProviderTestService_FixedCNAnthropicUsesProviderDefault(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(307, capability.PlatformZhipu)
	provider.Credentials["api_protocol"] = providercore.APIProtocolAnthropic
	delete(provider.Credentials, "api_base_urls")
	svc, upstream := adaptiveCNProviderTestService(provider, adaptiveCNAnthropicTestResponse())
	c, _ := newTestContext()

	err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "glm-4.7", "", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://open.bigmodel.cn/api/anthropic/v1/messages", upstream.requests[0].URL.String())
}

func TestProviderTestService_FixedCNAnthropicRejectsOpenAIBaseURLAndMarksAuthErrors(t *testing.T) {
	t.Run("misconfigured base URL", func(t *testing.T) {
		provider := adaptiveCNProviderTestProvider(308, capability.PlatformZhipu)
		provider.Credentials["api_protocol"] = providercore.APIProtocolAnthropic
		provider.Credentials["base_url"] = "https://open.bigmodel.cn/api/paas/v4"
		svc, upstream := adaptiveCNProviderTestService(provider)
		c, recorder := newTestContext()

		err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "glm-4.7", "", providercore.ProviderTestModeDefault)

		require.Error(t, err)
		require.Empty(t, upstream.requests)
		require.Contains(t, recorder.Body.String(), "looks like an OpenAI-compatible endpoint")
	})

	t.Run("forbidden marks provider error", func(t *testing.T) {
		provider := adaptiveCNProviderTestProvider(309, capability.PlatformZhipu)
		provider.Credentials["api_protocol"] = providercore.APIProtocolAnthropic
		provider.Credentials["base_url"] = "https://open.bigmodel.cn/api/anthropic"
		svc, _ := adaptiveCNProviderTestService(provider, newJSONResponse(http.StatusForbidden, `{"error":{"message":"invalid key"}}`))
		c, _ := newTestContext()

		err := executeOpenAIProbeRequest(t, svc, c, provider.ID, "glm-4.7", "", providercore.ProviderTestModeDefault)

		require.Error(t, err)
		repo := testassert.MustType[*openAIProbeStore](svc.Store)
		require.Equal(t, provider.ID, repo.setErrorID)
	})
}

func TestProviderTestService_AdaptiveSelectedProtocolTestsOnlyThatEndpoint(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(310, capability.PlatformDeepseek)
	svc, upstream := adaptiveCNProviderTestService(provider, adaptiveCNAnthropicTestResponse())
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequestType(t, svc, c, provider.ID, "deepseek-chat", "", providercore.ProviderTestTypeText, providercore.ProviderTestModeDefault, providercore.APIProtocolAnthropic)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "http://anthropic.example/v1/messages", upstream.requests[0].URL.String())
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_start"`))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
}

func TestProviderTestService_SelectedProtocolMustBeEnabled(t *testing.T) {
	provider := adaptiveCNProviderTestProvider(311, capability.PlatformZhipu)
	provider.Credentials[providercore.UpstreamProtocolsKey] = []any{string(protocol.ProtocolOpenAIChatCompletions)}
	svc, upstream := adaptiveCNProviderTestService(provider)
	c, recorder := newTestContext()

	err := executeOpenAIProbeRequestType(t, svc, c, provider.ID, "glm-4.7", "", providercore.ProviderTestTypeText, providercore.ProviderTestModeDefault, providercore.APIProtocolAnthropic)

	require.Error(t, err)
	require.Empty(t, upstream.requests)
	require.Contains(t, recorder.Body.String(), "Test protocol anthropic is not supported for this provider")
}
