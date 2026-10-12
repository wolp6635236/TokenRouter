package httpapi

// 本文件检查 routes_user.go 和 routes_admin.go 注册的路由对平台额度接口返回 404。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestRemovedPlatformQuotaRoutesReturnNotFound 检查用户与管理员的平台额度接口返回 404。
func TestRemovedPlatformQuotaRoutesReturnNotFound(t *testing.T) {
	router := gin.New()
	RegisterUserRoutes(router.Group("/api/v1"), &RedeemHandler{}, &SubscriptionHandler{})
	RegisterSubscriptionRoutes(router.Group("/api/v1/admin"), &AdminSubscriptionHandler{})
	for _, item := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/user/platform-quotas"},
		{http.MethodGet, "/api/v1/admin/users/1/platform-quotas"},
		{http.MethodPut, "/api/v1/admin/users/1/platform-quotas"},
		{http.MethodPost, "/api/v1/admin/users/1/platform-quotas/reset"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(item.method, item.path, nil))
		require.Equal(t, http.StatusNotFound, recorder.Code, "%s %s", item.method, item.path)
	}
}
