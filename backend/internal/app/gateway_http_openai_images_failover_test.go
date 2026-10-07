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

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	routing "github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type openAIImagesFailoverProviderRepo struct {
	gatewayprovider.ExecutionProviderStore

	providers []gatewayprovider.ExecutionProvider
}

func (r openAIImagesFailoverProviderRepo) GetByID(_ context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	for i := range r.providers {
		if r.providers[i].Record.ID == id {
			provider := r.providers[i]
			return &provider, nil
		}
	}
	return nil, scheduler.ErrNoAvailableProviders
}

func (r openAIImagesFailoverProviderRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.providersForPlatform(platform), nil
}

func (r openAIImagesFailoverProviderRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.providersForPlatform(platform), nil
}

func (r openAIImagesFailoverProviderRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.providersForPlatform(platform), nil
}

func (r openAIImagesFailoverProviderRepo) providersForPlatform(platform string) []gatewayprovider.ExecutionProvider {
	out := make([]gatewayprovider.ExecutionProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		if platform == "" || provider.Record.Platform == platform {
			out = append(out, provider)
		}
	}
	return out
}

type openAIImagesFailoverHTTPUpstream struct {
	mu          sync.Mutex
	providerIDs []int64
}

func (u *openAIImagesFailoverHTTPUpstream) Do(_ *http.Request, _ string, providerID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.providerIDs = append(u.providerIDs, providerID)
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_img_failover"},
		},
		Body: io.NopCloser(bytes.NewBufferString(
			"data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"image backend unavailable\"}}\n\n",
		)),
	}, nil
}

func (u *openAIImagesFailoverHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, providerConcurrency)
}

func (u *openAIImagesFailoverHTTPUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.providerIDs...)
}

func TestOpenAIGatewayHandlerImages_ServerErrorFailsOverAndReturnsClearErrorWhenExhausted(t *testing.T) {
	groupID := int64(3130)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 1,
				Name:        "image-provider-1",
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Concurrency: 0,
				Priority:    0,
				Credentials: map[string]any{"access_token": "token-1"},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 2,
				Name:        "image-provider-2",
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
	providerRepo := openAIImagesFailoverProviderRepo{providers: providers}
	upstream := &openAIImagesFailoverHTTPUpstream{}
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
	concurrencyService := scheduler.NewConcurrencyService(&fakeConcurrencyCache{}, scheduler.Diagnostics{
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

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","quality":"high","size":"1536x1024"}`)
	core, observedLogs := observer.New(zap.DebugLevel)
	requestCtx := logging.IntoContext(context.Background(), zap.New(core))
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body)).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(keyhttp.ContextKeyAPIKey), &apikey.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &routing.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &identity.User{ID: 100},
	})
	c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: 100, Concurrency: 0})

	handler.Images(c)
	providerSelectingLogs := observedLogs.FilterMessage("openai.images.provider_selecting").All()
	require.NotEmpty(t, providerSelectingLogs)
	loggedFields := make(map[string]string)
	for _, field := range providerSelectingLogs[0].Context {
		loggedFields[field.Key] = field.String
	}
	require.Equal(t, "high", loggedFields["img_quality"])
	require.Equal(t, "1536x1024", loggedFields["img_size"])
	require.NotContains(t, loggedFields, "prompt")

	require.Equal(t, []int64{1, 2}, upstream.calls())
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, "Upstream service temporarily unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.message").String())

	rawEvents, ok := c.Get(gatewayhttp.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*ops.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 2)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "failover", events[1].Kind)
}
