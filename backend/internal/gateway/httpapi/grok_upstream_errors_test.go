package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// handleGrokProviderUpstreamError 保留测试中的布尔断言写法；生产代码统一使用完整决策。
func (s *wsExecutionFixture) handleGrokProviderUpstreamError(
	ctx context.Context,
	provider *gatewayprovider.ExecutionProvider,
	statusCode int,
	headers http.Header,
	responseBody []byte,
	requestedModel ...string,
) bool {
	return gatewayprovider.ApplyGrokExecutionHealth(ctx, s.Output.GrokHealth, provider, statusCode, headers, responseBody, "", requestedModel...).StopScheduling
}

func TestIsGrokContentPolicyRejection(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "new sensitive code",
			status: http.StatusForbidden,
			body:   `{"error":{"code":"new_sensitive","message":"image is sensitive"}}`,
			want:   true,
		},
		{
			name:   "content policy violation code",
			status: http.StatusForbidden,
			body:   `{"response":{"error":{"code":"content_policy_violation"}}}`,
			want:   true,
		},
		{
			name:   "cyber policy code",
			status: http.StatusForbidden,
			body:   `{"error":{"code":"cyber_policy","message":"request rejected"}}`,
			want:   true,
		},
		{
			name:   "moderation feature unavailable",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"The moderation feature is not available for this request"}}`,
			want:   true,
		},
		{
			name:   "explicit prompt moderation rejection",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"request rejected by content moderation"}}`,
			want:   true,
		},
		{
			name:   "entitlement forbidden",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"subscription required"}}`,
			want:   false,
		},
		{
			name:   "provider policy suspension is not request policy",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"account suspended due to policy violation"}}`,
			want:   false,
		},
		{
			name:   "structured provider suspension overrides policy reason",
			status: http.StatusForbidden,
			body:   `{"error":{"code":"account_suspended","reason":"policy_violation","message":"account suspended due to policy violation"}}`,
			want:   false,
		},
		{
			name:   "ambiguous policy violation code is not enough",
			status: http.StatusForbidden,
			body:   `{"error":{"code":"policy_violation","message":"policy violation"}}`,
			want:   false,
		},
		{
			name:   "policy violation with request scoped message",
			status: http.StatusForbidden,
			body:   `{"error":{"code":"policy_violation","message":"request blocked by policy"}}`,
			want:   true,
		},
		{
			name:   "permission-denied usage guidelines is request scoped",
			status: http.StatusForbidden,
			body:   `{"code":"permission-denied","error":"Content violates usage guidelines. "}`,
			want:   true,
		},
		{
			name:   "permission-denied entitlement stays on the provider path",
			status: http.StatusForbidden,
			body:   `{"code":"permission-denied","error":"Access to the chat endpoint is denied"}`,
			want:   false,
		},
		{
			name:   "structured provider code overrides usage guidelines phrase",
			status: http.StatusForbidden,
			body:   `{"error":{"code":"account_suspended","message":"Content violates usage guidelines."}}`,
			want:   false,
		},
		{
			name:   "wrong status",
			status: http.StatusBadRequest,
			body:   `{"error":{"code":"new_sensitive"}}`,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, grok.IsGrokContentPolicyRejection(tt.status, []byte(tt.body)))
		})
	}
}

func TestGrokContentPolicy403SharedErrorFallbackDoesNotMutate(t *testing.T) {
	body := []byte(`{"error":{"code":"content_filter","message":"prohibited content"}}`)
	repo := &grokQuotaProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{providers: repo})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 4719,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(http.StatusTooManyRequests)},
			},
		},
	}

	newContext := func() (*gin.Context, *httptest.ResponseRecorder) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		return c, recorder
	}

	c, recorder := newContext()
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
	_, err := svc.Output.ResponseError(context.Background(), resp, c, provider, nil, "grok-4.5")
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_request_error")

	c, recorder = newContext()
	resp = &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
	_, err = svc.Output.CompatError(resp, c, provider, WriteForwardChatError, WriteForwardChatErrorBody, "grok-4.5")
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_request_error")

	require.Zero(t, repo.tempUnschedCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.Zero(t, repo.updateCalls)
}

func TestGrokContentPolicy403MediaResponseBypassesCustomErrorCodes(t *testing.T) {
	body := `{"error":{"code":"new_sensitive","message":"image is sensitive"}}`
	repo := &grokQuotaProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{providers: repo})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 4720,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(http.StatusTooManyRequests)},
			},
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, err := mediaErrorResponseFixture(svc.Grok, context.Background(), resp, c, provider, "request-id", "grok-imagine")
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_request_error")
	require.Zero(t, repo.tempUnschedCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.Zero(t, repo.updateCalls)
}

func TestGrokContentPolicySSEErrorDoesNotMutateOrFailover(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	upstream := &auxiliaryHTTPRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"error\",\"error\":{\"code\":\"new_sensitive\",\"message\":\"text is sensitive\"}}\n\n",
		)),
	}}
	svc := newWSFixture(wsFixtureInputs{providers: repo, transport: upstream})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 4721, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth, Concurrency: 1}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	payload := []byte(`{"type":"response.create","model":"grok-4.5","input":"hi"}`)
	var writes [][]byte

	result, err := svc.proxyOpenAIWSHTTPBridgeTurn(
		context.Background(), c, provider, "access-token", payload, len(payload),
		"grok-4.5", "grok-4.5", "", "", "", "cache-id", 1,
		func(message []byte) error {
			writes = append(writes, append([]byte(nil), message...))
			return nil
		},
	)

	require.Error(t, err)
	require.NotNil(t, result)
	var failoverErr *forwardcore.UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Len(t, writes, 1)
	require.Contains(t, string(writes[0]), "new_sensitive")
	require.Zero(t, repo.tempUnschedCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.Zero(t, repo.updateCalls)
	require.False(t, wsFixtureProviderBlocked(svc, provider))
}

func TestGrokPermissionDeniedContentRefusalDoesNotMutateOrFailover(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{providers: repo})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 4785, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"code":"permission-denied","error":"Content violates usage guidelines. "}`)

	svc.handleGrokProviderUpstreamError(context.Background(), provider, http.StatusForbidden, nil, body)

	require.Zero(t, repo.tempUnschedCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.Zero(t, repo.updateCalls)
	require.False(t, wsFixtureProviderBlocked(svc, provider))
	require.False(t, gatewayprovider.ShouldFailoverGrokResponse(http.StatusForbidden, body))
}
