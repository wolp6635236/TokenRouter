package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func TestOllamaCloudUsageHandlersValidateRequestsAndDependencies(t *testing.T) {
	svc := newOllamaCloudUsageHandlerTestService(t)

	t.Run("invalid provider id", func(t *testing.T) {
		ctx, recorder := newOllamaCloudUsageHandlerContext(http.MethodGet, "/admin/providers/not-an-id/ollama-cloud-usage", "", "not-an-id")
		NewOllamaUsageHandler(svc).GetOllamaCloudUsage(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("empty session", func(t *testing.T) {
		ctx, recorder := newOllamaCloudUsageHandlerContext(http.MethodPut, "/admin/providers/7/ollama-cloud-usage/session", `{"session":""}`, "7")
		NewOllamaUsageHandler(svc).SaveOllamaCloudUsageSession(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("missing enabled", func(t *testing.T) {
		ctx, recorder := newOllamaCloudUsageHandlerContext(http.MethodPut, "/admin/providers/7/ollama-cloud-usage/auto-refresh", `{}`, "7")
		NewOllamaUsageHandler(svc).SetOllamaCloudUsageAutoRefresh(ctx)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("service unavailable", func(t *testing.T) {
		ctx, recorder := newOllamaCloudUsageHandlerContext(http.MethodGet, "/admin/providers/7/ollama-cloud-usage", "", "7")
		NewOllamaUsageHandler(nil).GetOllamaCloudUsage(ctx)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Contains(t, recorder.Body.String(), "OLLAMA_CLOUD_USAGE_UNAVAILABLE")
	})
}

func TestGetOllamaCloudUsageSettingsHandlerSuccess(t *testing.T) {
	ctx, recorder := newOllamaCloudUsageHandlerContext(http.MethodGet, "/admin/providers/ollama-cloud-usage/settings", "", "")
	handler := NewOllamaUsageHandler(newOllamaCloudUsageHandlerTestService(t))

	handler.GetOllamaCloudUsageSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"enabled":false`)
	require.Contains(t, recorder.Body.String(), `"interval_minutes":60`)
	require.Contains(t, recorder.Body.String(), `"debounce_minutes":1`)
}

func newOllamaCloudUsageHandlerTestService(t *testing.T) *providercore.OllamaCloudUsageService {
	t.Helper()
	svc := providercore.NewOllamaCloudUsageService(nil, nil, nil, providercore.OllamaUsageOptions{})
	t.Cleanup(svc.Stop)
	return svc
}

func newOllamaCloudUsageHandlerContext(method, target, body, id string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	return ctx, recorder
}
