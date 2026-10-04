package httpapi

import (
	"strconv"

	"github.com/TokenFlux/TokenRouter/internal/qualityprobe"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/gin-gonic/gin"
)

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
	report, err := h.engine.Run(c.Request.Context(), id, qualityprobe.TriggerManual)
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

// ListLogs GET /api/v1/admin/quality-probe/logs
func (h *Handler) ListLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	logs, err := h.engine.ListLogs(c.Request.Context(), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, logs)
}

// RegisterRoutes 挂到管理员路由组。
func RegisterRoutes(admin *gin.RouterGroup, endpoint *Handler) {
	admin.GET("/quality-probe/settings", endpoint.GetSettings)
	admin.PUT("/quality-probe/settings", endpoint.PutSettings)
	admin.GET("/quality-probe/logs", endpoint.ListLogs)
	admin.GET("/providers/:id/quality-probe", endpoint.Status)
	admin.POST("/providers/:id/quality-probe", endpoint.Run)
}
