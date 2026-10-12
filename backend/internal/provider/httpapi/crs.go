package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// CRSHandler 只解析管理请求，六类同步及逐条结果由提供商用例拥有。
type CRSHandler struct{ core *provider.CRSSync }

type SyncFromCRSRequest struct {
	BaseURL             string   `json:"base_url" binding:"required"`
	Username            string   `json:"username" binding:"required"`
	Password            string   `json:"password" binding:"required"`
	SyncProxies         *bool    `json:"sync_proxies"`
	SelectedProviderIDs []string `json:"selected_provider_ids"`
}
type PreviewFromCRSRequest struct {
	BaseURL  string `json:"base_url" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func NewCRSHandler(core *provider.CRSSync) *CRSHandler { return &CRSHandler{core: core} }

// SyncFromCRS 执行管理员同步。
// POST /api/v1/admin/providers/sync/crs
func (h *CRSHandler) SyncFromCRS(c *gin.Context) {
	var req SyncFromCRSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 缺省同步代理，传入 false 时关闭同步。
	syncProxies := true
	if req.SyncProxies != nil {
		syncProxies = *req.SyncProxies
	}

	result, err := h.core.SyncFromCRS(c.Request.Context(), provider.SyncFromCRSInput{
		BaseURL:             req.BaseURL,
		Username:            req.Username,
		Password:            req.Password,
		SyncProxies:         syncProxies,
		SelectedProviderIDs: req.SelectedProviderIDs,
	})
	if err != nil {
		// 按 CRS 接口格式返回错误内容。
		response.InternalError(c, "CRS sync failed: "+err.Error())
		return
	}

	response.Success(c, result)
}

// PreviewFromCRS 返回同步前预览。
// POST /api/v1/admin/providers/sync/crs/preview
func (h *CRSHandler) PreviewFromCRS(c *gin.Context) {
	var req PreviewFromCRSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.core.PreviewFromCRS(c.Request.Context(), provider.SyncFromCRSInput{
		BaseURL:  req.BaseURL,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		response.InternalError(c, "CRS preview failed: "+err.Error())
		return
	}

	response.Success(c, result)
}
