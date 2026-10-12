package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// RecoverState handles unified recovery of recoverable provider runtime state.
// POST /api/v1/admin/providers/:id/recover-state
func (h *ManagementHandler) RecoverState(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	if h.recovery == nil {
		response.Error(c, http.StatusServiceUnavailable, "Rate limit service unavailable")
		return
	}

	if _, err := h.recovery.RecoverProviderState(c.Request.Context(), providerID, providercore.ProviderRecoveryOptions{
		InvalidateToken: true,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	provider, err := h.adminService.GetProvider(c.Request.Context(), providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.presenter.Present(c.Request.Context(), provider))
}

// ClearRateLimit handles clearing provider rate limit status
// POST /api/v1/admin/providers/:id/clear-rate-limit
func (h *ManagementHandler) ClearRateLimit(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	err = h.recovery.ClearRateLimit(c.Request.Context(), providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	provider, err := h.adminService.GetProvider(c.Request.Context(), providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.presenter.Present(c.Request.Context(), provider))
}

// GetTempUnschedulable handles getting temporary unschedulable status
// GET /api/v1/admin/providers/:id/temp-unschedulable
func (h *ManagementHandler) GetTempUnschedulable(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	state, err := h.recovery.GetTempUnschedStatus(c.Request.Context(), providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if state == nil || state.UntilUnix <= time.Now().Unix() {
		response.Success(c, gin.H{"active": false})
		return
	}

	response.Success(c, gin.H{
		"active": true,
		"state":  state,
	})
}

// ClearTempUnschedulable handles clearing temporary unschedulable status
// DELETE /api/v1/admin/providers/:id/temp-unschedulable
func (h *ManagementHandler) ClearTempUnschedulable(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	if err := h.recovery.ClearTempUnschedulable(c.Request.Context(), providerID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Temp unschedulable cleared successfully"})
}
