package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	providerhttp "github.com/TokenFlux/TokenRouter/internal/provider/httpapi"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type upstreamUsageHandlerRepo struct {
	providers map[int64]*providercore.Record
}

type upstreamUsageHandlerHTTP struct {
	body string
}

func (r *upstreamUsageHandlerRepo) GetByID(_ context.Context, id int64) (*providercore.Record, error) {
	provider := r.providers[id]
	if provider == nil {
		return nil, providercore.ErrProviderNotFound
	}
	copy := *provider
	return &copy, nil
}

func (u *upstreamUsageHandlerHTTP) Do(req *http.Request, proxyURL string, providerID int64, concurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, providerID, concurrency, nil)
}

func (u *upstreamUsageHandlerHTTP) DoWithTLS(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func newUpstreamUsageHandlerRouter(body string, providers map[int64]*providercore.Record) *gin.Engine {
	repo := &upstreamUsageHandlerRepo{providers: providers}
	options := provideradapter.UsageHTTPOptions{Available: true, Policy: egress.UsageURLPolicy{Configured: true, AllowInsecureHTTP: true}, Do: (&upstreamUsageHandlerHTTP{body: body}).DoWithTLS}
	usage := providercore.NewUpstreamUsageService(repo, provideradapter.NewUpstreamUsageHTTPExecution(options), providercore.UpstreamUsageOptions{})
	handler := providerhttp.NewUpstreamUsageHandler(usage)
	router := gin.New()
	router.POST("/admin/providers/:id/upstream-usage/query", handler.QueryUpstreamUsage)
	router.POST("/admin/providers/upstream-usage/query/batch", handler.QueryBatchUpstreamUsage)
	return router
}

func TestProviderHandlerQueryUpstreamUsageReturnsNormalizedResult(t *testing.T) {
	provider := &providercore.Record{
		ID: 7, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
		Status: providercore.StatusActive, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-handler-secret", "base_url": "http://upstream.example"},
	}
	router := newUpstreamUsageHandlerRouter(`{"isValid":true,"mode":"unrestricted","unit":"USD","planName":"payg","remaining":12.5,"balance":12.5}`, map[int64]*providercore.Record{7: provider})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/admin/providers/7/upstream-usage/query", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data providercore.UpstreamUsageQueryResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, int64(7), envelope.Data.ProviderID)
	require.Equal(t, providercore.UpstreamUsageAdapterSub2API, envelope.Data.Adapter)
	require.Equal(t, "balance", envelope.Data.Mode)
	require.Equal(t, 12.5, *envelope.Data.Balance.Remaining)
	require.NotContains(t, recorder.Body.String(), `"usage"`)
	require.NotContains(t, recorder.Body.String(), "sk-handler-secret")
}

func TestProviderHandlerQueryBatchUpstreamUsageValidatesIDs(t *testing.T) {
	router := newUpstreamUsageHandlerRouter(`{}`, nil)
	for _, payload := range []string{`{}`, `{"provider_ids":[]}`, `{"provider_ids":[0]}`, `{"provider_ids":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101]}`} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/providers/upstream-usage/query/batch", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code, payload)
	}

	var envelope struct {
		Reason string `json:"reason"`
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/providers/upstream-usage/query/batch", strings.NewReader(`{"provider_ids":[]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "UPSTREAM_USAGE_BATCH_INVALID", envelope.Reason)
}

func TestProviderHandlerQueryUpstreamUsageValidatesProviderID(t *testing.T) {
	router := newUpstreamUsageHandlerRouter(`{}`, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/admin/providers/not-an-id/upstream-usage/query", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var envelope struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "UPSTREAM_USAGE_PROVIDER_INVALID", envelope.Reason)
}

func TestProviderHandlerQueryBatchUpstreamUsageKeepsPerProviderErrors(t *testing.T) {
	provider := &providercore.Record{
		ID: 7, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
		Status: providercore.StatusActive, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
	}
	router := newUpstreamUsageHandlerRouter(`{"isValid":true,"mode":"unrestricted","unit":"USD","planName":"payg","remaining":1,"balance":1}`, map[int64]*providercore.Record{7: provider})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/providers/upstream-usage/query/batch", strings.NewReader(`{"provider_ids":[7,8]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data struct {
			Usage  map[string]json.RawMessage `json:"usage"`
			Errors map[string]struct {
				Code string `json:"code"`
			} `json:"errors"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Contains(t, envelope.Data.Usage, "7")
	require.Equal(t, "UPSTREAM_USAGE_PROVIDER_INVALID", envelope.Data.Errors["8"].Code)
}
