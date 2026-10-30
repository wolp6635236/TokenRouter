package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// management_model_sync_test.go checks stored and preview model-query HTTP
// responses. Errors distinguish invalid local credentials from upstream
// failures and return safe messages. management_model_sync.go is tested through
// the production model catalogue adapter with fixed consumable responses,
// including preview parsing and duplicate-ID normalization.

// syncUpstreamHTTPUpstream returns a controlled model-list response or injected
// transport error.
type syncUpstreamHTTPUpstream struct {
	// resp is returned by pointer when err is nil. Its body is consumed by the
	// production query, so callers supply a fresh configured response for each
	// request.
	resp     *http.Response  // borrowed consumable upstream response
	err      error           // injected upstream transport failure
	requests []*http.Request // captured upstream query requests
}

// TestProviderHandlerSyncUpstreamModels_ConfigErrorReturnsBadRequest loads an
// API-key provider without its required key. The handler returns 400 with the
// configuration message before a usable upstream model query can complete.
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

// TestProviderHandlerSyncUpstreamModels_UpstreamErrorDoesNotExposeBody supplies
// an upstream 502 response containing secret-shaped body text. The
// administrator sees the safe status message and that body secret remains
// absent from the HTTP response.
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

// TestProviderHandlerSyncUpstreamModelsPreview_UsesProvidedCredentials supplies
// temporary API-key configuration and a model list with duplicates. Preview
// succeeds and returns the de-duplicated model order produced by the production
// catalogue adapter.
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
		Data struct { // decoded HTTP success envelope
			Models []string `json:"models"` // returned normalized model identifiers
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"gpt-5.1", "o3"}, resp.Data.Models)
}

// TestProviderHandlerSyncUpstreamModelsPreview_ConfigErrorReturnsBadRequest
// omits the required temporary API key from preview JSON. Binding rejects the
// request with 400 and exposes the required-field message before querying
// models.
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

// Do returns the configured response or injected transport error for this
// model-query fixture. Response bodies are consumable and reused by pointer, so
// each test supplies a fresh response for its request.
func (u *syncUpstreamHTTPUpstream) Do(req *http.Request, proxyURL string, providerID int64, providerConcurrency int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}

// DoWithTLS delegates to the fixed response path while ignoring profile
// selection. The returned response and body are shared with Do; callers supply
// a fresh consumable response for each request.
func (u *syncUpstreamHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, providerConcurrency)
}

// setupSyncUpstreamModelsRouter mounts stored and preview model queries using
// the production catalogue parser and URL policy. The supplied transport
// controls responses, and an eight-MiB body limit bounds parsed fixture
// results.
func setupSyncUpstreamModelsRouter(adminSvc ProviderManagement, upstream provideradapter.QoderTransport) *gin.Engine {
	router := gin.New()
	modelPolicy := egress.OperatorURLPolicy{}
	catalogue := &provideradapter.ModelCatalogue{Transport: upstream, Options: provideradapter.ModelCatalogueOptions{ValidateURL: modelPolicy.Validate, OperatorValidator: modelPolicy.Validate, BodyLimit: 8 * 1024 * 1024, CodexModelsURL: provideradapter.DefaultCodexModelsURL}}
	models := providercore.NewModelSyncService(catalogue.FetchUpstreamSupportedModels)
	models.Suggest = providercore.OpenCodeProtocolSuggestions(nil)
	handler := NewManagementHandler(adminSvc, ManagementOptions{Models: models})
	router.POST("/api/v1/admin/providers/:id/models/sync-upstream", handler.SyncUpstreamModels)
	router.POST("/api/v1/admin/providers/models/sync-upstream-preview", handler.SyncUpstreamModelsPreview)
	return router
}

// TestProviderHandlerSyncUpstreamModelsRejectsOverrides attempts to redirect a
// saved OpenCode credential through the synchronization operation. Every override
// body is rejected before transport, including unknown-length requests and suffixes.
func TestProviderHandlerSyncUpstreamModelsRejectsOverrides(t *testing.T) {
	for _, body := range []string{
		`{"base_url":"https://collector.example"}`,
		`{"provider_mode":"go"}`,
		`{"api_key":"replacement"}`,
		`{"base_url":""}`,
		`{"provider_mode":""}`,
		`{} {"base_url":"https://collector.example"}`,
		`null`,
		`[]`,
	} {
		for _, unknownLength := range []bool{false, true} {
			t.Run(body+strconv.FormatBool(unknownLength), func(t *testing.T) {
				svc := &availableModelsAdminService{
					managementMutationFixture: newManagementMutationFixture(),
					provider: providercore.Record{
						ID: 44, Platform: capability.PlatformOpenCode, Type: capability.ProviderTypeAPIKey,
						Credentials: map[string]any{"api_key": "saved-secret", "base_url": "https://saved.example"},
					},
				}
				upstream := &syncUpstreamHTTPUpstream{}
				router := setupSyncUpstreamModelsRouter(svc, upstream)
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/44/models/sync-upstream", strings.NewReader(body))
				if unknownLength {
					req.ContentLength = -1
				}
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Empty(t, upstream.requests)
				require.Equal(t, "https://saved.example", svc.provider.GetCredential("base_url"))
			})
		}
	}
}

// TestProviderHandlerSyncUpstreamModelsUsesSavedSettings exercises both accepted
// body forms through the real catalogue adapter. The saved root, mode, and normalized
// credential determine transport, while protocol suggestions exclude saved overrides.
func TestProviderHandlerSyncUpstreamModelsUsesSavedSettings(t *testing.T) {
	for _, body := range []string{"", "{}"} {
		t.Run(body, func(t *testing.T) {
			svc := &availableModelsAdminService{
				managementMutationFixture: newManagementMutationFixture(),
				provider: providercore.Record{
					ID: 44, Platform: capability.PlatformOpenCode, Type: capability.ProviderTypeAPIKey,
					Credentials: map[string]any{
						"api_key": " \nsaved-secret\r\n", "base_url": "https://saved.example", "provider_mode": "go",
						providercore.OpenCodeModelProtocolsKey: map[string]any{"go": map[string]any{"minimax-m3": "systemone"}},
					},
				},
			}
			upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"minimax-m3"}]}`)),
			}}
			router := setupSyncUpstreamModelsRouter(svc, upstream)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodPost, "/api/v1/admin/providers/44/models/sync-upstream", strings.NewReader(body),
			)
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "https://saved.example/v1/models", upstream.requests[0].URL.String())
			require.Equal(t, "Bearer saved-secret", upstream.requests[0].Header.Get("Authorization"))
			require.Contains(t, rec.Body.String(), `"minimax-m3":"anthropic_messages"`)
			require.NotContains(t, rec.Body.String(), "saved-secret")
		})
	}
}

// TestProviderHandlerOpenCodePreviewUsesCallerCredentials previews the default
// payg service and explicit Go service. Each request uses the supplied key and
// temporary root, Go suggestions show that the mode reached the query record,
// and neither preview requires reading a saved provider.
func TestProviderHandlerOpenCodePreviewUsesCallerCredentials(t *testing.T) {
	for _, mode := range []string{"", "go"} {
		t.Run(mode, func(t *testing.T) {
			upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"minimax-m3"}]}`)),
			}}
			router := setupSyncUpstreamModelsRouter(newManagementMutationFixture(), upstream)
			body := map[string]any{"platform": "opencode", "type": "apikey", "api_key": "preview-key", "base_url": "https://preview.example"}
			if mode != "" {
				body["provider_mode"] = mode
			}
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodPost, "/api/v1/admin/providers/models/sync-upstream-preview", strings.NewReader(string(encoded)),
			)
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "https://preview.example/v1/models", upstream.requests[0].URL.String())
			require.Equal(t, "Bearer preview-key", upstream.requests[0].Header.Get("Authorization"))
			if mode == "go" {
				require.Contains(t, rec.Body.String(), `"minimax-m3":"anthropic_messages"`)
			}
		})
	}
}

// TestProviderHandlerSyncSerializesEmptyProtocolSuggestions queries an unknown
// model through saved-provider and preview endpoints. OpenCode returns an empty
// object so forms can clear stale routes; other platforms omit protocol suggestions.
func TestProviderHandlerSyncSerializesEmptyProtocolSuggestions(t *testing.T) {
	for _, platform := range []string{capability.PlatformOpenCode, capability.PlatformOpenAI} {
		for _, preview := range []bool{false, true} {
			t.Run(platform+"/preview="+strconv.FormatBool(preview), func(t *testing.T) {
				svc := &availableModelsAdminService{
					managementMutationFixture: newManagementMutationFixture(),
					provider: providercore.Record{
						ID: 44, Platform: platform, Type: capability.ProviderTypeAPIKey,
						Credentials: map[string]any{
							"api_key": "saved-secret", "base_url": "https://saved.example",
							providercore.OpenCodeCatalogProtocolsKey: map[string]any{
								"payg": map[string]any{"custom-model": "openai_responses"},
							},
						},
					},
				}
				upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"custom-model"}]}`)),
				}}
				router := setupSyncUpstreamModelsRouter(svc, upstream)
				path := "/api/v1/admin/providers/44/models/sync-upstream"
				body := "{}"
				if preview {
					path = "/api/v1/admin/providers/models/sync-upstream-preview"
					encoded, err := json.Marshal(map[string]string{
						"platform": platform, "type": "apikey", "api_key": "preview-key",
						"base_url": "https://preview.example",
					})
					require.NoError(t, err)
					body = string(encoded)
				}
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var response struct {
					Data map[string]json.RawMessage `json:"data"` // serialized synchronization payload
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				require.JSONEq(t, `["custom-model"]`, string(response.Data["models"]))
				if platform == capability.PlatformOpenCode {
					require.JSONEq(t, `{}`, string(response.Data["model_protocols"]))
				} else {
					require.NotContains(t, response.Data, "model_protocols")
				}
			})
		}
	}
}
