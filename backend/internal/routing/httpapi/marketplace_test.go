package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// marketplaceHTTPGroups 提供带可见模型的公开分组。
type marketplaceHTTPGroups struct{}

// marketplaceHTTPCapacity 记录 HTTP 参数是否触发容量聚合。
type marketplaceHTTPCapacity struct{ calls int }

// marketplaceHTTPModels 提供真实目录解析形状，测试容量查询参数。
type marketplaceHTTPModels struct{}

func (marketplaceHTTPGroups) ListActive(context.Context) ([]routing.Group, error) {
	return []routing.Group{{ID: 1, ActiveProviderCount: 1}}, nil
}

func (s *marketplaceHTTPCapacity) GetGroupCapacityByIDs(context.Context, []int64) (map[int64]routing.GroupCapacitySummary, error) {
	s.calls++
	return map[int64]routing.GroupCapacitySummary{1: {}}, nil
}

// TestMarketplaceCapacityQuery 保证默认响应附带容量，页面可跳过聚合，非法参数返回 400。
func TestMarketplaceCapacityQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query  string
		status int
		calls  int
	}{
		{"", 200, 1},
		{"?include_capacity=true", 200, 1},
		{"?include_capacity=false", 200, 0},
		{"?include_capacity=invalid", 400, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			capacity := &marketplaceHTTPCapacity{}
			core := routing.NewMarketplace(marketplaceHTTPGroups{}, nil, marketplaceHTTPModels{}, routing.RequestableResolver{}, nil, capacity, nil, routing.MarketplaceOptions{})
			router := gin.New()
			RegisterPublicMarketplaceRoutes(router.Group("/api/v1"), NewMarketplaceHandler(core, nil))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/models"+tc.query, nil))
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, tc.calls, capacity.calls)
			if tc.calls > 0 {
				require.Contains(t, response.Body.String(), `"capacity":`)
			} else {
				require.NotContains(t, response.Body.String(), `"capacity":`)
			}
			if tc.status == 200 {
				require.Contains(t, response.Body.String(), `"id":"model"`)
			}
		})
	}
}

func (marketplaceHTTPModels) ResolveRequestableModels(context.Context, *int64, string) routing.RequestableModelsResult {
	return routing.RequestableModelsResult{Models: []routing.RequestableModel{{ID: "model"}}}
}

func (marketplaceHTTPModels) Prefetch(context.Context) ([]routing.CatalogueProvider, bool, error) {
	return nil, false, nil
}
