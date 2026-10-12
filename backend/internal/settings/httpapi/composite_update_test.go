package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestRemovedCrossClientModelMappingField 检查废弃字段在读取或修改设置前被拒绝。
func TestRemovedCrossClientModelMappingField(t *testing.T) {
	for _, value := range []string{"true", "false", "null"} {
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"grok_cross_client_model_map_enabled":`+value+`}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		(&Handler{}).UpdateSettings(ctx)
		require.Equal(t, http.StatusBadRequest, writer.Code)
		require.Contains(t, writer.Body.String(), "model_mapping")
	}
}

// TestUpdateSettingsRejectsRemovedPlatformQuotas 检查平台额度字段（包括 null）在读取或保存其他设置前被拒绝。
func TestUpdateSettingsRejectsRemovedPlatformQuotas(t *testing.T) {
	for _, name := range []string{"default_platform_quotas", "auth_source_default_email_platform_quotas", "auth_source_default_linuxdo_platform_quotas", "auth_source_default_oidc_platform_quotas", "auth_source_default_wechat_platform_quotas", "auth_source_default_github_platform_quotas", "auth_source_default_google_platform_quotas", "auth_source_default_dingtalk_platform_quotas"} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.PUT("/settings", (&Handler{}).UpdateSettings)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"`+name+`":null}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), "REMOVED_SETTING_FIELD")
			require.Contains(t, recorder.Body.String(), name)
		})
	}
}

// TestUpdateSettingsRejectsUngroupedScheduling 检查含未选组调度开关的请求在访问设置存储前被拒绝。
func TestUpdateSettingsRejectsUngroupedScheduling(t *testing.T) {
	for _, value := range []string{"true", "false", "null"} {
		router := gin.New()
		router.PUT("/settings", (&Handler{}).UpdateSettings)
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"allow_ungrouped_key_scheduling":`+value+`}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "REMOVED_SETTING_FIELD")
	}
}
