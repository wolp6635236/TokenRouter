package httpapi

import (
	"context"
	"strconv"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/qualityprobe"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/gin-gonic/gin"
)

const manualProbeTimeout = 3 * time.Minute

// Handler 管理降智探测设置和手动探测。
type Handler struct {
	engine *qualityprobe.Engine
}

func New(engine *qualityprobe.Engine) *Handler {
	return &Handler{engine: engine}
}

// GetSettings GET /api/v1/admin/quality-probe/settings
func (h *Handler) GetSettings(c *gin.Context) {
	cfg, err := h.engine.LoadSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, qualityprobe.SettingsDTO(cfg))
}

// PutSettings PUT /api/v1/admin/quality-probe/settings
func (h *Handler) PutSettings(c *gin.Context) {
	var payload qualityprobe.SettingsPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.engine.SaveSettings(c.Request.Context(), payload.Settings()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	cfg, err := h.engine.LoadSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, qualityprobe.SettingsDTO(cfg))
}

// Run POST /api/v1/admin/providers/:id/quality-probe
func (h *Handler) Run(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	// 关掉弹窗会取消 HTTP 请求。探测和写 Extra 继续跑完，记录页才能看到这一轮。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), manualProbeTimeout)
	defer cancel()
	var payload struct {
		Model string `json:"model"`
	}
	_ = c.ShouldBindJSON(&payload)
	report, err := h.engine.RunWithModel(ctx, id, qualityprobe.TriggerManual, payload.Model)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, report)
}

// Status GET /api/v1/admin/providers/:id/quality-probe
func (h *Handler) Status(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	state, err := h.engine.Status(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

// ListLogs GET /api/v1/admin/quality-probe/logs，按 page、page_size 分页，单页最多 100 条。
func (h *Handler) ListLogs(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > 100 {
		pageSize = 100
	}
	logs, total, err := h.engine.ListLogs(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, logs, int64(total), page, pageSize)
}

// RegisterRoutes 挂到管理员路由组。
func RegisterRoutes(admin *gin.RouterGroup, endpoint *Handler) {
	admin.GET("/quality-probe/settings", endpoint.GetSettings)
	admin.PUT("/quality-probe/settings", endpoint.PutSettings)
	admin.GET("/quality-probe/logs", endpoint.ListLogs)
	admin.GET("/providers/:id/quality-probe", endpoint.Status)
	admin.POST("/providers/:id/quality-probe", endpoint.Run)
}
