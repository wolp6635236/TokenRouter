package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// SetSchedulableRequest represents the request body for setting schedulable status.
type SetSchedulableRequest struct {
	Schedulable bool `json:"schedulable"`
}

// SetSchedulable handles toggling provider schedulable status
// POST /api/v1/admin/providers/:id/schedulable
func (h *ManagementHandler) SetSchedulable(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	var req SetSchedulableRequest
	if err := response.BindJSONStrict(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	provider, err := h.adminService.SetProviderSchedulable(c.Request.Context(), providerID, req.Schedulable)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.presenter.Present(c.Request.Context(), provider))
}

// ResetQuota handles resetting provider quota usage
// POST /api/v1/admin/providers/:id/reset-quota
func (h *ManagementHandler) ResetQuota(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	if err := h.adminService.ResetProviderQuota(c.Request.Context(), providerID); err != nil {
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

// ClearError handles clearing provider error
// POST /api/v1/admin/providers/:id/clear-error
func (h *ManagementHandler) ClearError(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	provider, err := h.managed.ClearError(c.Request.Context(), providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.presenter.Present(c.Request.Context(), provider))
}

// RevertProxyFallback handles reverting provider proxy to original before fallback.
// POST /api/v1/admin/providers/:id/revert-proxy-fallback
func (h *ManagementHandler) RevertProxyFallback(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	if err := h.adminService.RevertProviderProxyFallback(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "reverted"})
}
