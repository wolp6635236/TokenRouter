package httpapi

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/qualityprobe"
	"github.com/TokenFlux/TokenRouter/internal/server/adminapi"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

const manualProbeTimeout = 3 * time.Minute

// handler.go exposes administrator settings, manual runs, provider status, and
// paginated quality-probe logs. Manual requests keep their work after a browser
// cancels the response so completed probes still update provider Extra history.

// Handler serves administrator quality-probe HTTP requests through the engine.
type Handler struct {
	engine *qualityprobe.Engine
}

// New retains the engine used by registered quality-probe administrator routes.
func New(engine *qualityprobe.Engine) *Handler {
	return &Handler{engine: engine}
}

// GetSettings returns the current quality-probe settings to an administrator.
func (h *Handler) GetSettings(c *gin.Context) {
	cfg, err := h.engine.LoadSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, qualityprobe.SettingsDTO(cfg))
}

// PutSettings validates, persists, and returns quality-probe settings for an administrator.
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

// ModelCandidates returns requestable model identifiers from active selected
// groups. The optional group_ids query repeats group IDs; an empty list covers
// every active group, matching automatic probe scope.
func (h *Handler) ModelCandidates(c *gin.Context) {
	values := append(c.QueryArray("group_ids"), c.QueryArray("group_ids[]")...)
	groupIDs := make([]int64, 0, len(values))
	for _, rawID := range values {
		groupID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || groupID <= 0 {
			response.BadRequest(c, "Invalid group ID")
			return
		}
		groupIDs = append(groupIDs, groupID)
	}
	models, err := h.engine.ModelCandidates(c.Request.Context(), groupIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"models": models})
}

// Run executes one manual probe for a provider and returns its report. Browser
// cancellation does not stop the detached request because history persistence
// must reflect a probe that already reached its upstream account.
func (h *Handler) Run(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	// The browser can close the modal before the probe finishes. Detach request
	// cancellation so the completed result still reaches provider Extra history.
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

// Status returns one provider's quality-probe state and recent history.
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

// ListLogs returns quality-probe history in pages of at most one hundred records.
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

// RegisterRoutes mounts quality-probe endpoints beneath an authenticated administrator group.
func RegisterRoutes(admin *gin.RouterGroup, endpoint *Handler) {
	adminapi.Register(admin, ManagementOperations["quality_probe.settings.get"], endpoint.GetSettings)
	adminapi.Register(admin, ManagementOperations["quality_probe.settings.update"], endpoint.PutSettings)
	adminapi.Register(admin, ManagementOperations["quality_probe.models.list"], endpoint.ModelCandidates)
	adminapi.Register(admin, ManagementOperations["quality_probe.logs.list"], endpoint.ListLogs)
	adminapi.Register(admin, ManagementOperations["quality_probe.provider.get"], endpoint.Status)
	adminapi.Register(admin, ManagementOperations["quality_probe.provider.run"], endpoint.Run)
}
