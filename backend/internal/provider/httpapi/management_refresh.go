package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// ApplyOAuthCredentialsRequest 是重新授权后保存 OAuth 凭据的专用请求体。
type ApplyOAuthCredentialsRequest struct {
	Type        string         `json:"type" binding:"required,oneof=oauth setup-token"`
	Credentials map[string]any `json:"credentials" binding:"required"`
	Extra       map[string]any `json:"extra"`
}

// Refresh 返回管理刷新结果和原项目缺失警告。
// POST /api/v1/admin/providers/:id/refresh
func (h *ManagementHandler) Refresh(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	// 读取提供商，缺失时返回 404。
	provider, err := h.adminService.GetProvider(c.Request.Context(), providerID)
	if err != nil {
		response.NotFound(c, "Provider not found")
		return
	}

	updatedProvider, warning, err := h.managed.Refresh(c.Request.Context(), provider)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if warning == "missing_project_id_temporary" {
		response.Success(c, gin.H{
			"message": "Token refreshed successfully, but project_id could not be retrieved (will retry automatically)",
			"warning": "missing_project_id_temporary",
		})
		return
	}

	response.Success(c, h.presenter.Present(c.Request.Context(), updatedProvider))
}

// ApplyOAuthCredentials 保存重新授权得到的新凭据，并增量合并 Extra。
// POST /api/v1/admin/providers/:id/apply-oauth-credentials
//
// 该接口刻意不复用通用 Update：
// - 接收 type、credentials 和 extra 字段。
// - Extra 按 JSONB 键合并，base_rpm、quota_*、privacy_mode 等其他配置保持当前值。
// - 清理错误状态并使 token 缓存失效，后续请求读取新凭据。
func (h *ManagementHandler) ApplyOAuthCredentials(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	var req ApplyOAuthCredentialsRequest
	if err := response.BindJSONStrict(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	providercore.DiscardDeprecatedProviderExtra(req.Extra)

	ctx := c.Request.Context()
	existing, err := h.adminService.GetProvider(ctx, providerID)
	if err != nil {
		response.NotFound(c, "Provider not found")
		return
	}
	core := h.managed
	updated, err := core.Reauthorize(ctx, existing, providercore.ManagedReauthorizationInput{Type: req.Type, Credentials: req.Credentials, Extra: req.Extra})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.presenter.Present(ctx, updated))
}
