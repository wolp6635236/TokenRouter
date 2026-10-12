package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing"
)

func TestPricingRoutesRejectPolicyFieldsAndRemoveOldEndpoints(t *testing.T) {
	router := gin.New()
	handler := NewPricingHandler(nil, &routing.PricingCatalog{})
	RegisterPricingRoutes(router.Group("/api/v1/admin"), handler)
	for _, field := range []string{"model_mapping", "restrict_models", "features_config", "features", "apply_pricing_to_provider_stats"} {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/pricing/configs", strings.NewReader(`{"name":"price","`+field+`":null}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		require.Equal(t, http.StatusBadRequest, response.Code, field)
	}
	for _, path := range []string{"/channels", "/channels/model-pricing", "/channels/pricing/sync-models", "/pricing/defaults/models"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin"+path, nil))
		require.Equal(t, http.StatusNotFound, response.Code, path)
	}
}
