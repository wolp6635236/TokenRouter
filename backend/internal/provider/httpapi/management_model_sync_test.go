package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type syncUpstreamHTTPUpstream struct {
	resp *http.Response
	err  error
}

func TestProviderHandlerSyncUpstreamModels_ConfigErrorReturnsBadRequest(t *testing.T) {
	svc := &availableModelsAdminService{
		managementMutationFixture: newManagementMutationFixture(),
		provider: providercore.Record{
			ID:       44,
			Name:     "openai-apikey-missing-key",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	router := setupSyncUpstreamModelsRouter(svc, &syncUpstreamHTTPUpstream{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/44/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "No OpenAI API key is available")
}

func TestProviderHandlerSyncUpstreamModels_UpstreamErrorDoesNotExposeBody(t *testing.T) {
	svc := &availableModelsAdminService{
		managementMutationFixture: newManagementMutationFixture(),
		provider: providercore.Record{
			ID:       45,
			Name:     "openai-apikey-upstream-error",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"api_key":  "openai-key",
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/45/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Upstream model list request failed with HTTP 502")
	require.NotContains(t, rec.Body.String(), "SECRET_TOKEN")
}

func TestProviderHandlerSyncUpstreamModelsPreview_UsesProvidedCredentials(t *testing.T) {
	router := setupSyncUpstreamModelsRouter(newManagementMutationFixture(), &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"gpt-5.1"},{"id":"gpt-5.1"},{"id":"o3"}]}`)),
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/providers/models/sync-upstream-preview",
		strings.NewReader(`{"platform":"openai","type":"apikey","base_url":"https://openai.example.com/v1","api_key":"openai-key"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data struct {
			Models []string `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"gpt-5.1", "o3"}, resp.Data.Models)
}

func TestProviderHandlerSyncUpstreamModelsPreview_ConfigErrorReturnsBadRequest(t *testing.T) {
	router := setupSyncUpstreamModelsRouter(newManagementMutationFixture(), &syncUpstreamHTTPUpstream{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/providers/models/sync-upstream-preview",
		strings.NewReader(`{"platform":"openai","type":"apikey","base_url":"https://openai.example.com/v1"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "required")
}

func (u *syncUpstreamHTTPUpstream) Do(req *http.Request, proxyURL string, providerID int64, providerConcurrency int) (*http.Response, error) {
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}

func (u *syncUpstreamHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, providerConcurrency)
}

func setupSyncUpstreamModelsRouter(adminSvc ProviderManagement, upstream provideradapter.QoderTransport) *gin.Engine {
	router := gin.New()
	modelPolicy := egress.OperatorURLPolicy{}
	catalogue := &provideradapter.ModelCatalogue{Transport: upstream, Options: provideradapter.ModelCatalogueOptions{ValidateURL: modelPolicy.Validate, OperatorValidator: modelPolicy.Validate, BodyLimit: 8 * 1024 * 1024, CodexModelsURL: provideradapter.DefaultCodexModelsURL}}
	models := providercore.NewModelSyncService(catalogue.FetchUpstreamSupportedModels)
	handler := NewManagementHandler(adminSvc, ManagementOptions{Models: models})
	router.POST("/api/v1/admin/providers/:id/models/sync-upstream", handler.SyncUpstreamModels)
	router.POST("/api/v1/admin/providers/models/sync-upstream-preview", handler.SyncUpstreamModelsPreview)
	return router
}
