package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// BatchRefreshTierRequest represents batch tier refresh request.
type BatchRefreshTierRequest struct {
	ProviderIDs []int64 `json:"provider_ids"`
}

// RefreshTier 校验提供商 ID，缺失时返回 404，成功时返回档位数据。
func (h *ManagementHandler) RefreshTier(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	v, err := h.adminService.GetProvider(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "Provider not found")
		return
	}
	result, err := h.tier.Refresh(c.Request.Context(), v)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	extra := result.StorageInfo
	response.Success(c, gin.H{"tier_id": result.TierID, "storage_info": extra, "drive_storage_limit": extra["drive_storage_limit"], "drive_storage_usage": extra["drive_storage_usage"], "updated_at": extra["drive_tier_updated_at"]})
}

// BatchRefreshTier 保留损坏/空输入回落全量 Google One 提供商的历史行为。
func (h *ManagementHandler) BatchRefreshTier(c *gin.Context) {
	var req BatchRefreshTierRequest
	if err := response.BindJSONStrict(c, &req); err != nil {
		req = BatchRefreshTierRequest{}
	}
	result, err := h.tier.Batch(c.Request.Context(), req.ProviderIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var failures []gin.H
	for _, v := range result.Errors {
		failures = append(failures, gin.H{"provider_id": v.ProviderID, "error": v.Error})
	}
	response.Success(c, gin.H{"total": result.Total, "success": result.Success, "failed": result.Failed, "errors": failures})
}
