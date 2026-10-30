package googleforward_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/errorpolicy"
	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/googleforward"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

// TestOpenCodeGeminiRetriesUseOuterBudget returns quota failures on repeated
// gateway attempts. Each invocation sends once and carries the provider identity
// and a capped Retry-After delay back to the caller's bounded retry loop, while
// the provider cooldown keeps the full thirty-second deadline.
func TestOpenCodeGeminiRetriesUseOuterBudget(t *testing.T) {
	for _, pool := range []bool{false, true} {
		value := &gatewayprovider.ExecutionProvider{Record: providercore.Record{
			ID: 17, Platform: providercore.PlatformOpenCode, Type: providercore.ProviderTypeAPIKey,
			Credentials: map[string]any{"api_key": "key", "pool_mode": pool, providercore.OpenCodeModelProtocolsKey: map[string]any{"payg": map[string]any{"gemini-2.5-flash": "gemini_generate_content"}}},
		}}
		stub := &geminiCompatHTTPUpstreamStub{}
		repo := &rateLimit429ProviderRepoStub{}
		health := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)
		svc := newGeminiFixture(geminiDependencies{httpUpstream: stub, cfg: &googleforward.Options{}, providerRepo: repo, healthObserver: health})
		for attempt := 1; attempt <= 2; attempt++ {
			stub.response = &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"30"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"busy"}}`))}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil)
			_, err := svc.ForwardNative(context.Background(), gatewayhttp.NewGoogleBoundary(c, svc.Options, false), value, "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
			var failure *forward.UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, attempt, stub.calls)
			require.Equal(t, providercore.PlatformOpenCode, failure.Platform)
			require.Equal(t, 8*time.Second, failure.SameProviderRetryDelay)
			require.Equal(t, pool, failure.RetryableOnSameProvider)
			if pool {
				require.Zero(t, repo.rateLimitCalls)
			} else {
				require.Equal(t, attempt, repo.rateLimitCalls)
				require.WithinDuration(t, time.Now().Add(30*time.Second), repo.lastRateLimitReset, time.Second)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil)
		_, err := svc.ForwardNative(ctx, gatewayhttp.NewGoogleBoundary(c, svc.Options, false), value, "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
		require.True(t, errors.Is(err, context.Canceled))
		require.Equal(t, 2, stub.calls)
	}
}

// TestOpenCodeGeminiErrorRulesUseExplicitProvider passes no selection telemetry
// to direct and exhausted error output. Platform-scoped OpenCode rules must
// match the retained provider identity rather than the Gemini wire format.
func TestOpenCodeGeminiErrorRulesUseExplicitProvider(t *testing.T) {
	rule := newNonFailoverPassthroughRule(422, "invalid schema", 418, "OpenCode rule")
	rule.Platforms = []string{providercore.PlatformOpenCode}
	rules := newErrorRulesTestService([]*errorpolicy.ErrorPassthroughRule{rule})
	body := []byte(`{"error":{"message":"invalid schema"}}`)
	for _, exhausted := range []bool{false, true} {
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		gatewayhttp.BindErrorPassthroughService(c, rules)
		if exhausted {
			gatewayhttp.MessagesErrorOutput{Rules: rules}.GeminiExhausted(c, &forward.UpstreamFailoverError{Platform: providercore.PlatformOpenCode, StatusCode: 422, ResponseBody: body, ResponseHeaders: http.Header{"Retry-After": {"30"}}})
		} else {
			svc := newGeminiFixture(geminiDependencies{})
			value := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Platform: providercore.PlatformOpenCode}}
			require.Error(t, gatewayhttp.NewGoogleBoundary(c, svc.Options, false).GeminiMappedError(value, 422, "", body))
		}
		require.Equal(t, 418, writer.Code)
		require.Contains(t, writer.Body.String(), "OpenCode rule")
		if exhausted {
			require.Equal(t, "30", writer.Header().Get("Retry-After"))
		}
	}
}

// TestOpenCodeGeminiCompatibleRetriesUseOuterBudget exercises each converted
// client protocol with the same quota response. Messages, Responses and Chat
// must return one provider attempt with its Retry-After wait capped at eight seconds.
func TestOpenCodeGeminiCompatibleRetriesUseOuterBudget(t *testing.T) {
	for _, client := range []string{"messages", "responses", "chat"} {
		t.Run(client, func(t *testing.T) {
			value := &gatewayprovider.ExecutionProvider{Record: providercore.Record{
				ID: 19, Platform: providercore.PlatformOpenCode, Type: providercore.ProviderTypeAPIKey,
				Credentials: map[string]any{"api_key": "key", "pool_mode": true, providercore.OpenCodeModelProtocolsKey: map[string]any{"payg": map[string]any{"gemini-2.5-flash": "gemini_generate_content"}}},
			}}
			stub := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"30"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"busy"}}`))}}
			svc := newGeminiFixture(geminiDependencies{httpUpstream: stub, cfg: &googleforward.Options{}})
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			output := gatewayhttp.NewGoogleBoundary(c, svc.Options, false)
			var err error
			switch client {
			case "messages":
				_, err = svc.Forward(context.Background(), output, value, []byte(`{"model":"gemini-2.5-flash","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`))
			case "responses":
				_, err = svc.ForwardAsResponses(context.Background(), output, value, []byte(`{"model":"gemini-2.5-flash","input":"hi"}`), nil)
			case "chat":
				_, err = svc.ForwardAsChatCompletions(context.Background(), output, value, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))
			}
			var failure *forward.UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, 1, stub.calls)
			require.Equal(t, providercore.PlatformOpenCode, failure.Platform)
			require.Equal(t, 8*time.Second, failure.SameProviderRetryDelay)
			require.True(t, failure.RetryableOnSameProvider)
			require.Zero(t, writer.Body.Len())
		})
	}
}

// TestOpenCodeGeminiTransportFailureUsesProviderPolicy returns a transport error
// before response headers. Ordinary and pool providers both dispatch once,
// while only a pool configured for 502 receives an outer same-provider retry.
func TestOpenCodeGeminiTransportFailureUsesProviderPolicy(t *testing.T) {
	for _, pool := range []bool{false, true} {
		value := &gatewayprovider.ExecutionProvider{Record: providercore.Record{
			ID: 21, Platform: providercore.PlatformOpenCode, Type: providercore.ProviderTypeAPIKey,
			Credentials: map[string]any{"api_key": "key", "pool_mode": pool, "pool_mode_retry_status_codes": []any{float64(http.StatusBadGateway)}, providercore.OpenCodeModelProtocolsKey: map[string]any{"payg": map[string]any{"gemini-2.5-flash": "gemini_generate_content"}}},
		}}
		stub := &geminiCompatHTTPUpstreamStub{err: io.ErrUnexpectedEOF}
		svc := newGeminiFixture(geminiDependencies{httpUpstream: stub, cfg: &googleforward.Options{}})
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil)
		_, err := svc.ForwardNative(context.Background(), gatewayhttp.NewGoogleBoundary(c, svc.Options, false), value, "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
		var failure *forward.UpstreamFailoverError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, 1, stub.calls)
		require.Equal(t, providercore.PlatformOpenCode, failure.Platform)
		require.Equal(t, pool, failure.RetryableOnSameProvider)
		require.Zero(t, writer.Body.Len())
	}
}
