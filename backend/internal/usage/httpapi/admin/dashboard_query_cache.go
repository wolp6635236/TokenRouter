package admin

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// snapshotCache 缓存 HTTP 快照及其 ETag。
type snapshotCache = httpx.SnapshotCache

func (h *DashboardHandler) getUsageTrendCached(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	userID, apiKeyID, providerID, groupID, teamID int64,
	model string,
	requestType *int16,
	stream *bool,
	billingType *int8,
	nativeCompactionV2 *bool,
) ([]usage.TrendDataPoint, bool, error) {
	return h.dashboardService.GetUsageTrendCached(ctx, startTime, endTime, granularity, userID, apiKeyID, providerID, groupID, teamID, model, requestType, stream, billingType, nativeCompactionV2)
}

func (h *DashboardHandler) getModelStatsCached(
	ctx context.Context,
	startTime, endTime time.Time,
	userID, apiKeyID, providerID, groupID, teamID int64,
	modelSource string,
	requestType *int16,
	stream *bool,
	billingType *int8,
	nativeCompactionV2 *bool,
) ([]usage.ModelStat, bool, error) {
	return h.dashboardService.GetModelStatsCached(ctx, startTime, endTime, userID, apiKeyID, providerID, groupID, teamID, modelSource, requestType, stream, billingType, nativeCompactionV2)
}

func (h *DashboardHandler) getGroupStatsCached(
	ctx context.Context,
	startTime, endTime time.Time,
	userID, apiKeyID, providerID, groupID, teamID int64,
	requestType *int16,
	stream *bool,
	billingType *int8,
	nativeCompactionV2 *bool,
) ([]usage.GroupStat, bool, error) {
	return h.dashboardService.GetGroupStatsCached(ctx, startTime, endTime, userID, apiKeyID, providerID, groupID, teamID, requestType, stream, billingType, nativeCompactionV2)
}

func (h *DashboardHandler) getAPIKeyUsageTrendCached(ctx context.Context, startTime, endTime time.Time, granularity string, limit int) ([]usage.APIKeyUsageTrendPoint, bool, error) {
	return h.dashboardService.GetAPIKeyUsageTrendCached(ctx, startTime, endTime, granularity, limit)
}

func (h *DashboardHandler) getUserUsageTrendCached(ctx context.Context, startTime, endTime time.Time, granularity string, limit int) ([]usage.UserUsageTrendPoint, bool, error) {
	return h.dashboardService.GetUserUsageTrendCached(ctx, startTime, endTime, granularity, limit)
}

func cacheStatusValue(hit bool) string {
	if hit {
		return "hit"
	}
	return "miss"
}

// newSnapshotCache 创建指定有效期的 HTTP 快照缓存。
func newSnapshotCache(ttl time.Duration) *snapshotCache { return httpx.NewSnapshotCache(ttl) }

// parseBoolQueryWithDefault 解析布尔查询参数，空值使用默认值。
func parseBoolQueryWithDefault(raw string, def bool) bool {
	return httpx.ParseBoolQueryWithDefault(raw, def)
}

// normalizeInt64IDList 规范化批量查询的 ID 列表。
func normalizeInt64IDList(v []int64) []int64 { return httpx.NormalizeInt64IDList(v) }

// ifNoneMatchMatched 判断请求的 If-None-Match 是否匹配 ETag。
func ifNoneMatchMatched(raw, tag string) bool { return httpx.IfNoneMatchMatched(raw, tag) }
