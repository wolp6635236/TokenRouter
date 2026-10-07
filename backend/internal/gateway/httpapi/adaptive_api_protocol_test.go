package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	time "time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func adaptiveProtocolTestProvider(platform string, baseURLs map[string]any) *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 701,
			Name:        "adaptive-cn",
			Platform:    platform,
			Type:        capability.ProviderTypeAPIKey,
			Concurrency: 1,
			Credentials: map[string]any{
				"api_key":       "sk-test",
				"api_protocol":  providercore.APIProtocolAdaptive,
				"provider_mode": providercore.ProviderModePayG,
				"api_base_urls": baseURLs,
			},
		},
	}
}

func adaptiveProtocolTestContext(path string, body []byte) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

type cnProtocolIngressCase struct {
	name    string
	path    string
	body    []byte
	forward func(*OpenAIResponsesExecutor, *gin.Context, *gatewayprovider.ExecutionProvider, []byte) error
}

func cnProtocolIngressCases() []cnProtocolIngressCase {
	return []cnProtocolIngressCase{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}],"stream":false}`),
			forward: func(svc *OpenAIResponsesExecutor, c *gin.Context, provider *gatewayprovider.ExecutionProvider, body []byte) error {
				_, err := svc.Text.Chat(context.Background(), c, provider, body, "", "")
				return err
			},
		},
		{
			name: "messages",
			path: "/v1/messages",
			body: []byte(`{"model":"deepseek-chat","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`),
			forward: func(svc *OpenAIResponsesExecutor, c *gin.Context, provider *gatewayprovider.ExecutionProvider, body []byte) error {
				_, err := svc.Text.Messages(context.Background(), c, provider, body, "", "")
				return err
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: []byte(`{"model":"deepseek-chat","input":"hello","stream":false}`),
			forward: func(svc *OpenAIResponsesExecutor, c *gin.Context, provider *gatewayprovider.ExecutionProvider, body []byte) error {
				_, err := svc.Forward(context.Background(), c, provider, body)
				return err
			},
		},
	}
}

func TestAdaptiveProtocolRoutesChatCompletionsToNativeChat(t *testing.T) {
	body := []byte(`{"model":"glm-4.7","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformZhipu, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
	})

	_, err := svc.Text.Chat(context.Background(), adaptiveProtocolTestContext("/v1/chat/completions", body), provider, body, "", "")
	require.Error(t, err)
	require.Equal(t, "http://chat.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "messages").IsArray())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
}

func TestAdaptiveProtocolRoutesResponsesShapedChatToNativeResponses(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4","input":"hello","max_output_tokens":32,"stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformDeepseek, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
		providercore.APIProtocolResponses:       "http://responses.example",
	})

	_, err := svc.Text.Chat(context.Background(), adaptiveProtocolTestContext("/v1/chat/completions", body), provider, body, "", "")
	require.Error(t, err)
	require.Equal(t, "http://responses.example/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
}

func TestAdaptiveProtocolConvertsResponsesShapedChatForChatOnlyProvider(t *testing.T) {
	body := []byte(`{"model":"glm-4.7","input":"hello","max_output_tokens":32,"stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformZhipu, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
	})

	_, err := svc.Text.Chat(context.Background(), adaptiveProtocolTestContext("/v1/chat/completions", body), provider, body, "", "")
	require.Error(t, err)
	require.Equal(t, "http://chat.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "messages").IsArray())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
}

func TestAdaptiveProtocolRoutesKimiResponsesShapedChatToNativeResponses(t *testing.T) {
	body := []byte(`{"model":"k3-256k","input":"hello","max_output_tokens":32,"stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformKimi, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
		providercore.APIProtocolResponses:       "http://responses.example/v1",
	})

	_, err := svc.Text.Chat(context.Background(), adaptiveProtocolTestContext("/v1/chat/completions", body), provider, body, "", "")
	require.Error(t, err)
	require.Equal(t, "http://responses.example/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
}

func TestAdaptiveProtocolRoutesMessagesToNativeAnthropic(t *testing.T) {
	body := []byte(`{"model":"glm-4.7","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformZhipu, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
	})

	_, err := svc.Text.Messages(context.Background(), adaptiveProtocolTestContext("/v1/messages", body), provider, body, "", "")
	require.Error(t, err)
	require.Equal(t, "http://anthropic.example/v1/messages", upstream.lastReq.URL.String())
	require.Equal(t, "glm-4.7", gjson.GetBytes(upstream.lastBody, "model").String())
}

func TestAdaptiveProtocolRoutesKimiResponsesToNativeResponses(t *testing.T) {
	body := []byte(`{"model":"k3-256k","input":"hello","reasoning":{"effort":"none"},"store":true,"previous_response_id":"resp_old","stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformKimi, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
		providercore.APIProtocolResponses:       "http://responses.example/v1",
	})

	_, err := svc.Forward(context.Background(), adaptiveProtocolTestContext("/v1/responses", body), provider, body)
	require.Error(t, err)
	require.Equal(t, "http://responses.example/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "reasoning").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "previous_response_id").Exists())
}

func TestAdaptiveProtocolRoutesKimiCodingResponsesToNativeResponses(t *testing.T) {
	body := []byte(`{"model":"k3-256k","input":"hello","stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformKimi, map[string]any{
		providercore.APIProtocolChatCompletions: "https://api.kimi.com/coding/v1",
		providercore.APIProtocolAnthropic:       "https://api.kimi.com/coding",
		providercore.APIProtocolResponses:       "https://api.kimi.com/coding/v1",
	})
	provider.Record.Credentials["provider_mode"] = providercore.ProviderModeCoding

	_, err := svc.Forward(context.Background(), adaptiveProtocolTestContext("/v1/responses", body), provider, body)
	require.Error(t, err)
	require.Equal(t, "https://api.kimi.com/coding/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
}

func TestAdaptiveProtocolRoutesDeepSeekResponsesToNativeResponses(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4","input":"hello","max_output_tokens":32,"store":true,"previous_response_id":"resp_old","stream":false}`)
	upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
	provider := adaptiveProtocolTestProvider(capability.PlatformDeepseek, map[string]any{
		providercore.APIProtocolChatCompletions: "http://chat.example",
		providercore.APIProtocolAnthropic:       "http://anthropic.example",
		providercore.APIProtocolResponses:       "http://responses.example",
	})

	_, err := svc.Forward(context.Background(), adaptiveProtocolTestContext("/v1/responses", body), provider, body)
	require.Error(t, err)
	require.Equal(t, "http://responses.example/responses", upstream.lastReq.URL.String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "previous_response_id").Exists())
	require.Equal(t, int64(32), gjson.GetBytes(upstream.lastBody, "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.lastBody, "instructions").Exists())
}

func TestFixedCNChatProtocolOverridesStaleResponsesMode(t *testing.T) {
	for _, tc := range cnProtocolIngressCases() {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
			svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
			provider := adaptiveProtocolTestProvider(capability.PlatformDeepseek, nil)
			provider.Record.Credentials["api_protocol"] = providercore.APIProtocolChatCompletions
			provider.Record.Credentials["base_url"] = "http://chat.example"
			provider.Record.Extra = map[string]any{
				providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceResponses),
			}

			err := tc.forward(svc, adaptiveProtocolTestContext(tc.path, tc.body), provider, tc.body)

			require.Error(t, err)
			require.Equal(t, "http://chat.example/v1/chat/completions", upstream.lastReq.URL.String())
		})
	}
}

func TestFixedCNResponsesProtocolOverridesStaleChatMode(t *testing.T) {
	for _, tc := range cnProtocolIngressCases() {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &auxiliaryHTTPRecorder{err: errors.New("stop after capture")}
			svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions(), transport: upstream})
			provider := adaptiveProtocolTestProvider(capability.PlatformDeepseek, nil)
			provider.Record.Credentials["api_protocol"] = providercore.APIProtocolResponses
			provider.Record.Credentials["base_url"] = "http://responses.example"
			provider.Record.Extra = map[string]any{
				providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions),
			}

			err := tc.forward(svc, adaptiveProtocolTestContext(tc.path, tc.body), provider, tc.body)

			require.Error(t, err)
			require.Equal(t, "http://responses.example/responses", upstream.lastReq.URL.String())
		})
	}
}
