package httpapi

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// ProviderReportOptions 提供详细用量查询函数。
type ProviderReportOptions struct {
	Now        func() time.Time
	StartOfDay func(time.Time) time.Time
	Query      func(context.Context, int64, time.Time, time.Time) (*usage.ProviderUsageStatsResponse, error)
}

// GetStats handles getting provider statistics
// GET /api/v1/admin/providers/:id/stats
func (h *ManagementHandler) GetStats(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	// 请求范围为 1 到 90 天，默认 30 天。
	days := 30
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}

	// 日界仍按应用装配的同一时区计算。
	now := h.reports.Now()
	endTime := h.reports.StartOfDay(now.AddDate(0, 0, 1))
	startTime := h.reports.StartOfDay(now.AddDate(0, 0, -days+1))

	stats, err := h.reports.Query(c.Request.Context(), providerID, startTime, endTime)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, stats)
}
