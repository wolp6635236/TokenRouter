package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	routinghttp "github.com/TokenFlux/TokenRouter/internal/routing/httpapi"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupModelDefaultPricingRouter(billingSvc *billing.Calculator) *gin.Engine {
	router := gin.New()
	h := routinghttp.NewPricingHandler(nil, &routing.PricingCatalog{Prices: billingSvc})
	router.GET("/pricing/defaults/model", h.GetModelDefaultPricing)
	return router
}

// TestGetModelDefaultPricing_QoderMatchesOtherPlatforms 验证同一模型的默认价不受平台影响，Qoder 别名也可以读取内置价。
func TestGetModelDefaultPricing_QoderMatchesOtherPlatforms(t *testing.T) {
	router := setupModelDefaultPricingRouter(billingtestkit.Calculator(nil, nil))
	for _, model := range []string{"claude-opus-4-6", "CLAUDE-OPUS-4-6", "qwen3.8-max", "qmodel"} {
		t.Run(model, func(t *testing.T) {
			var responses []string
			for _, platform := range []string{"qoder", "anthropic"} {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pricing/defaults/model?platform="+platform+"&model="+model, nil))
				require.Equal(t, http.StatusOK, w.Code)
				responses = append(responses, w.Body.String())
			}
			require.JSONEq(t, responses[0], responses[1])
			if strings.Contains(strings.ToLower(model), "claude-opus") {
				require.Contains(t, responses[0], `"found":true`)
			}
		})
	}
}

func TestGetModelDefaultPricing_Fable51ReturnsCacheTTLs(t *testing.T) {
	billingSvc := billingtestkit.Calculator(nil, nil)
	router := setupModelDefaultPricingRouter(billingSvc)
	req := httptest.NewRequest(http.MethodGet, "/pricing/defaults/model?model=claude-fable-5-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Found             bool     `json:"found"`
			CacheWritePrice   float64  `json:"cache_write_price"`
			CacheWrite1hPrice *float64 `json:"cache_write_1h_price"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Data.Found)
	require.InDelta(t, 12.5e-6, body.Data.CacheWritePrice, 1e-12)
	require.NotNil(t, body.Data.CacheWrite1hPrice)
	require.InDelta(t, 20e-6, *body.Data.CacheWrite1hPrice, 1e-12)
}

func TestGetModelDefaultPricing_UnknownQoderRouteKeysRemainUnpriced(t *testing.T) {
	billingSvc := billingtestkit.Calculator(nil, nil)
	router := setupModelDefaultPricingRouter(billingSvc)

	for _, model := range []string{"qmodel", "qmodel_38max", "ultimate", "q35model", "gmodel"} {
		req := httptest.NewRequest(http.MethodGet, "/pricing/defaults/model?platform=qoder&model="+model, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var body struct {
			Data struct {
				Found      bool    `json:"found"`
				InputPrice float64 `json:"input_price"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

		require.False(t, body.Data.Found, "model=%s", model)
		require.Zero(t, body.Data.InputPrice, "model=%s", model)
	}
}
