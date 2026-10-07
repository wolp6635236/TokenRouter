package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	time "time"

	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	testkit "github.com/TokenFlux/TokenRouter/internal/apikey/testkit"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	authctx "github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	apikey "github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"

	identity "github.com/TokenFlux/TokenRouter/internal/identity"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	httpclient "github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	routing "github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// openAIResponsesFailoverProviderRepo 为 failover 用例提供按平台选号和提供商回读。
type openAIResponsesFailoverProviderRepo struct {
	gatewayprovider.ExecutionProviderStore

	providers []gatewayprovider.ExecutionProvider
}

func (r openAIResponsesFailoverProviderRepo) GetByID(_ context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	for i := range r.providers {
		if r.providers[i].Record.ID == id {
			provider := r.providers[i]
			return &provider, nil
		}
	}
	return nil, scheduler.ErrNoAvailableProviders
}

func (r openAIResponsesFailoverProviderRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.providersForPlatform(platform), nil
}

func (r openAIResponsesFailoverProviderRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.providersForPlatform(platform), nil
}

func (r openAIResponsesFailoverProviderRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.providersForPlatform(platform), nil
}

func (r openAIResponsesFailoverProviderRepo) providersForPlatform(platform string) []gatewayprovider.ExecutionProvider {
	out := make([]gatewayprovider.ExecutionProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		if platform == "" || provider.Record.Platform == platform {
			out = append(out, provider)
		}
	}
	return out
}

// openAIResponsesFailoverCancelUpstream 固定返回 HTTP 520，可在首次上游调用时
// 触发回调（用于模拟“上游在途期间客户端断开”）。
type openAIResponsesFailoverCancelUpstream struct {
	httpclient.
		UpstreamTransport
	mu          sync.Mutex
	providerIDs []int64
	onFirstDo   func()
}

func (u *openAIResponsesFailoverCancelUpstream) Do(_ *http.Request, _ string, providerID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.providerIDs = append(u.providerIDs, providerID)
	first := len(u.providerIDs) == 1
	u.mu.Unlock()
	if first && u.onFirstDo != nil {
		u.onFirstDo()
	}
	return &http.Response{
		StatusCode: 520,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(bytes.NewBufferString("<html>520: unknown error</html>")),
	}, nil
}

func (u *openAIResponsesFailoverCancelUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	providerID int64,
	providerConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, providerConcurrency)
}

func (u *openAIResponsesFailoverCancelUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.providerIDs...)
}

func newOpenAIResponsesFailoverTestHandler(t *testing.T, upstream httpclient.UpstreamTransport) *gatewayHTTPEndpointsFixture {
	t.Helper()
	proxyID := int64(11)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 1,
				Name:        "responses-provider-1",
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 0,
				Priority:    0,
				Credentials: map[string]any{"access_token": "token-1"},
				ProxyID:     &proxyID,
				Proxy: &egress.Proxy{
					ID:       proxyID,
					Name:     "responses-proxy",
					Protocol: "http",
					Host:     "proxy.example.com",
					Port:     8080,
					Username: "proxy-user-secret",
					Password: "proxy-password-secret",
				},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 2,
				Name:        "responses-provider-2",
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 0,
				Priority:    1,
				Credentials: map[string]any{"access_token": "token-2"},
			},
		},
	}
	providerRepo := openAIResponsesFailoverProviderRepo{providers: providers}
	cfg := &config.Config{}
	gatewayService, gatewayServiceChoices, gatewayServiceCredentialPort := newOpenAIExecutionAndSelectionFixture(
		providerRepo,
		nil,
		cfg,
		nil,
		nil,

		nil,

		upstream,
		nil,
		nil, newOpenAIExecutionCredentialsForTest(providerRepo,

			nil), nil,
		nil,
		nil,

		nil,
		nil, responseHeaderFilterForTest(cfg), nil, nil, nil,
	)
	gatewayService.Recorder = newHTTPCompletionFixture(cfg, nil,

		nil,

		nil,

		nil,

		nil, nil, true)

	billingService := newBillingEligibilityFixture(cfg)
	billingService.Start()
	t.Cleanup(billingService.Stop)
	concurrencyService := scheduler.NewConcurrencyService(nil, scheduler.Diagnostics{
		Logf:  logging.LegacyPrintf,
		Event: logging.Event,
	},
	)
	handler := newGatewayHTTPEndpointsFromDeps(
		gatewayService, gatewayServiceCredentialPort,
		concurrencyService, newFundingAdmissionFixture(billingService, cfg), testkit.NewService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		cfg, nil, newExecutionAvailabilityForTest(providerRepo,

			nil, cfg), gatewayServiceChoices,
	)
	handler.Input.MaxSwitches = 10
	return handler
}

func newOpenAIResponsesFailoverTestContext(t *testing.T, ctx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	groupID := int64(3131)
	body := []byte(`{"model":"gpt-5.4","stream":false,"input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(keyhttp.ContextKeyAPIKey), &apikey.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &routing.Group{
			ID: groupID,
		},
		User: &identity.User{ID: 100},
	})
	c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: 100, Concurrency: 0})
	return c, rec
}

// TestOpenAIGatewayHandlerResponses_FailoverAbortsWhenClientDisconnected 复现
// #4257：客户端在上游请求在途期间断开，上游随后返回可 failover 的 520。
// 客户端断开后结束提供商选择，将请求归类为 499，
// 提供商 2 的调用次数保持为零。
func TestOpenAIGatewayHandlerResponses_FailoverAbortsWhenClientDisconnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &openAIResponsesFailoverCancelUpstream{onFirstDo: cancel}
	handler := newOpenAIResponsesFailoverTestHandler(t, upstream)
	c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)

	handler.Responses(c)

	require.Equal(t, []int64{1}, upstream.calls(), "客户端断开后不应再切换到提供商 2")
	require.Equal(t, gatewayhttp.StatusClientClosedRequest, c.Writer.Status(), "应按 499 归类")
	require.Zero(t, rec.Body.Len(), "不应写入 502 错误响应体")

	_, hasFinalUpstreamErr := c.Get(gatewayhttp.OpsUpstreamStatusCodeKey)
	require.False(t, hasFinalUpstreamErr, "不应记录 failover 耗尽的上游错误终态")

	// 上游返回的 520 保留为 failover 事件，执行组件在返回错误前记录。
	rawEvents, ok := c.Get(gatewayhttp.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*ops.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, 520, events[0].UpstreamStatusCode)
}

// TestOpenAIGatewayHandlerResponses_FailoverContinuesForConnectedClient 检查客户端在线时，
// 提供商 1 返回 520 后切换到提供商 2，两个提供商都返回 520 时，
// 按提供商耗尽返回 502。
func TestOpenAIGatewayHandlerResponses_FailoverContinuesForConnectedClient(t *testing.T) {
	logSink, restore := captureHandlerStructuredLog(t)
	defer restore()

	upstream := &openAIResponsesFailoverCancelUpstream{}
	handler := newOpenAIResponsesFailoverTestHandler(t, upstream)
	c, rec := newOpenAIResponsesFailoverTestContext(t, nil)

	handler.Responses(c)

	require.Equal(t, []int64{1, 2}, upstream.calls(), "在线客户端应正常切换提供商")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.True(t, logSink.ContainsMessageAtLevel("openai.upstream_failover_switching", "warn"))
	require.True(t, logSink.ContainsFieldValue("proxy_id", "11"))
	require.True(t, logSink.ContainsFieldValue("proxy_name", "responses-proxy"))
	require.True(t, logSink.ContainsFieldValue("proxy_host", "proxy.example.com"))
	require.True(t, logSink.ContainsFieldValue("proxy_port", "8080"))
	require.False(t, logSink.ContainsFieldValue("proxy_username", "proxy-user-secret"))
	require.False(t, logSink.ContainsFieldValue("proxy_password", "proxy-password-secret"))
}
