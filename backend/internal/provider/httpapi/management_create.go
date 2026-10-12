package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// BatchCreate 按幂等 scope 接收批量输入，首次执行时创建提供商并派发后置任务。
func (h *ManagementHandler) BatchCreate(c *gin.Context) {
	var req struct {
		Providers []CreateProviderRequest `json:"providers" binding:"required,min=1"`
	}
	if err := response.BindJSONStrict(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	for i := range req.Providers {
		provider.DiscardDeprecatedProviderExtra(req.Providers[i].Extra)
	}
	h.ExecuteAdminIdempotentJSON(c, "admin.providers.batch_create", req, h.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		inputs := make([]provider.CreateProviderInput, 0, len(req.Providers))
		for _, item := range req.Providers {
			inputs = append(inputs, provider.CreateProviderInput{Name: item.Name, Notes: item.Notes, Platform: item.Platform, Type: item.Type, Credentials: item.Credentials, Extra: item.Extra, ProxyID: item.ProxyID, Concurrency: item.Concurrency, Priority: item.Priority, RateMultiplier: item.RateMultiplier, LoadFactor: item.LoadFactor, GroupIDs: item.GroupIDs, ExpiresAt: item.ExpiresAt, AutoPauseOnExpired: item.AutoPauseOnExpired})
		}
		result, err := h.batch.Create(ctx, inputs)
		if err != nil {
			return nil, err
		}
		items := make([]gin.H, 0, len(result.Results))
		for _, item := range result.Results {
			value := gin.H{"name": item.Name, "success": item.Success}
			if item.Success {
				value["id"] = item.ID
			} else {
				value["error"] = item.Error
			}
			items = append(items, value)
		}
		return gin.H{"success": result.Success, "failed": result.Failed, "results": items}, nil
	})
}
