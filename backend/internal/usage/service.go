package usage

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/pkg/querycache"
)

var ErrUsageLogNotFound = infraerrors.NotFound("USAGE_LOG_NOT_FOUND", "usage log not found")

// UsageService 使用统计服务。
type UsageService struct {
	statsQueryCache *querycache.Cache
	usageRepo       UsageLogRepository
	readers         QueryReaders
}

// NewUsageService 创建使用统计服务实例。
func NewUsageService(usageRepo UsageLogRepository, optional ...QueryReaders) *UsageService {
	readers := queryReaders(usageRepo)
	if len(optional) > 0 {
		readers = optional[0]
	}
	return &UsageService{
		statsQueryCache: querycache.NewCache(30 * time.Second),
		usageRepo:       usageRepo,
		readers:         readers,
	}
}

// GetByID 根据 ID 获取使用日志。
func (s *UsageService) GetByID(ctx context.Context, id int64) (*UsageLog, error) {
	log, err := s.usageRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get usage log: %w", err)
	}
	return log, nil
}

// ListByUser 获取用户的使用日志列表。
func (s *UsageService) ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, pagination, err := s.usageRepo.ListByUser(ctx, userID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs: %w", err)
	}
	return logs, pagination, nil
}

// ListByAPIKey 获取 API Key 的使用日志列表。
func (s *UsageService) ListByAPIKey(ctx context.Context, apiKeyID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, pagination, err := s.usageRepo.ListByAPIKey(ctx, apiKeyID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs: %w", err)
	}
	return logs, pagination, nil
}

// ListByProvider 获取提供商的使用日志列表。
func (s *UsageService) ListByProvider(ctx context.Context, providerID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, pagination, err := s.usageRepo.ListByProvider(ctx, providerID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs: %w", err)
	}
	return logs, pagination, nil
}

// GetUsageRanking 获取指定时间范围内按配置指标排序的用户用量排行。
func (s *UsageService) GetUsageRanking(ctx context.Context, startTime, endTime time.Time, limit int, sortBy UsageRankingSortBy) (*UsageRankingResponse, error) {
	ranking, err := s.usageRepo.GetUsageRanking(ctx, startTime, endTime, limit, sortBy)
	if err != nil {
		return nil, fmt.Errorf("get usage ranking: %w", err)
	}
	return ranking, nil
}

// Delete 删除使用日志（管理员功能，谨慎使用）。
func (s *UsageService) Delete(ctx context.Context, id int64) error {
	if err := s.usageRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete usage log: %w", err)
	}
	return nil
}

// GetUserDashboardStats 获取单个用户的仪表盘汇总统计。
func (s *UsageService) GetUserDashboardStats(ctx context.Context, userID int64) (*UserDashboardStats, error) {
	stats, err := s.usageRepo.GetUserDashboardStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user dashboard stats: %w", err)
	}
	return stats, nil
}

// GetAPIKeyDashboardStats 获取指定 API Key 的仪表盘汇总统计。
func (s *UsageService) GetAPIKeyDashboardStats(ctx context.Context, apiKeyID int64) (*UserDashboardStats, error) {
	stats, err := s.usageRepo.GetAPIKeyDashboardStats(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("get api key dashboard stats: %w", err)
	}
	return stats, nil
}

// GetUserUsageTrendByUserID 获取指定用户的用量趋势。
func (s *UsageService) GetUserUsageTrendByUserID(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) ([]TrendDataPoint, error) {
	trend, err := s.usageRepo.GetUserUsageTrendByUserID(ctx, userID, startTime, endTime, granularity)
	if err != nil {
		return nil, fmt.Errorf("get user usage trend: %w", err)
	}
	return trend, nil
}

// GetUsageTrendWithFilters 使用统一过滤条件获取用量趋势。
func (s *UsageService) GetUsageTrendWithFilters(ctx context.Context, startTime, endTime time.Time, granularity string, filters UsageLogFilters) ([]TrendDataPoint, error) {
	if filterRepo := s.readers.UsageTrendWithFiltersRepo; filterRepo != nil {
		trend, err := filterRepo.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, filters)
		if err != nil {
			return nil, fmt.Errorf("get usage trend with filters: %w", err)
		}
		return trend, nil
	}
	trend, err := s.usageRepo.GetUsageTrendWithFilters(ctx, startTime, endTime, granularity, filters.UserID, filters.APIKeyID, filters.ProviderID, filters.GroupID, filters.Model, filters.RequestType, filters.Stream, filters.BillingType)
	if err != nil {
		return nil, fmt.Errorf("get usage trend with filters: %w", err)
	}
	return trend, nil
}

// GetUserModelStats 获取指定用户的模型用量统计。
func (s *UsageService) GetUserModelStats(ctx context.Context, userID int64, startTime, endTime time.Time) ([]ModelStat, error) {
	stats, err := s.usageRepo.GetUserModelStats(ctx, userID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get user model stats: %w", err)
	}
	return stats, nil
}

// GetModelStatsWithFiltersBySource 使用统一过滤条件按模型来源获取模型统计。
func (s *UsageService) GetModelStatsWithFiltersBySource(ctx context.Context, startTime, endTime time.Time, filters UsageLogFilters, modelSource string) ([]ModelStat, error) {
	normalizedSource := NormalizeModelSource(modelSource)

	if filterRepo := s.readers.ModelStatsWithUsageFiltersRepo; filterRepo != nil {
		stats, err := filterRepo.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, filters, normalizedSource)
		if err != nil {
			return nil, fmt.Errorf("get model stats with filters by source: %w", err)
		}
		return stats, nil
	}

	if sourceRepo := s.readers.ModelStatsBySourceRepo; sourceRepo != nil {
		stats, err := sourceRepo.GetModelStatsWithFiltersBySource(ctx, startTime, endTime, filters.UserID, filters.APIKeyID, filters.ProviderID, filters.GroupID, filters.RequestType, filters.Stream, filters.BillingType, normalizedSource)
		if err != nil {
			return nil, fmt.Errorf("get model stats with filters by source: %w", err)
		}
		return stats, nil
	}
	stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, startTime, endTime, filters.UserID, filters.APIKeyID, filters.ProviderID, filters.GroupID, filters.RequestType, filters.Stream, filters.BillingType)
	if err != nil {
		return nil, fmt.Errorf("get model stats with filters: %w", err)
	}
	return stats, nil
}

// GetGroupStatsWithFilters 使用统一过滤条件获取分组统计。
func (s *UsageService) GetGroupStatsWithFilters(ctx context.Context, startTime, endTime time.Time, filters UsageLogFilters) ([]GroupStat, error) {
	if filterRepo := s.readers.GroupStatsWithUsageFiltersRepo; filterRepo != nil {
		stats, err := filterRepo.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, filters)
		if err != nil {
			return nil, fmt.Errorf("get group stats with filters: %w", err)
		}
		return stats, nil
	}
	stats, err := s.usageRepo.GetGroupStatsWithFilters(ctx, startTime, endTime, filters.UserID, filters.APIKeyID, filters.ProviderID, filters.GroupID, filters.RequestType, filters.Stream, filters.BillingType)
	if err != nil {
		return nil, fmt.Errorf("get group stats with filters: %w", err)
	}
	return stats, nil
}

// GetAPIKeyModelStats 获取指定 API Key 的模型用量统计。
func (s *UsageService) GetAPIKeyModelStats(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) ([]ModelStat, error) {
	stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, startTime, endTime, 0, apiKeyID, 0, 0, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get api key model stats: %w", err)
	}
	return stats, nil
}

// GetAPIKeyDailyUsage 返回用户指定 API Key 的按日用量统计。
func (s *UsageService) GetAPIKeyDailyUsage(ctx context.Context, userID, apiKeyID int64, startTime, endTime time.Time) ([]APIKeyDailyUsagePoint, error) {
	return s.getAPIKeyDailyUsage(ctx, userID, apiKeyID, startTime, endTime)
}

// GetAPIKeyDailyUsageByKey 供已由当前 Key 完成鉴权的网关自省使用。
func (s *UsageService) GetAPIKeyDailyUsageByKey(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) ([]APIKeyDailyUsagePoint, error) {
	return s.getAPIKeyDailyUsage(ctx, 0, apiKeyID, startTime, endTime)
}

func (s *UsageService) getAPIKeyDailyUsage(ctx context.Context, userID, apiKeyID int64, startTime, endTime time.Time) ([]APIKeyDailyUsagePoint, error) {
	trend, err := s.usageRepo.GetUsageTrendWithFilters(ctx, startTime, endTime, "day", userID, apiKeyID, 0, 0, "", nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get api key daily usage: %w", err)
	}

	points := make([]APIKeyDailyUsagePoint, 0, len(trend))
	for _, row := range trend {
		points = append(points, APIKeyDailyUsagePoint{
			Date:             row.Date,
			Requests:         row.Requests,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheCreationTokens,
			TotalTokens:      row.TotalTokens,
			Cost:             row.Cost,
			ActualCost:       row.ActualCost,
		})
	}
	return points, nil
}

// GetBatchAPIKeyUsageStats 获取多个 API Key 的今日/累计实际扣费。
func (s *UsageService) GetBatchAPIKeyUsageStats(ctx context.Context, apiKeyIDs []int64, startTime, endTime time.Time) (map[int64]*BatchAPIKeyUsageStats, error) {
	stats, err := s.usageRepo.GetBatchAPIKeyUsageStats(ctx, apiKeyIDs, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get batch api key usage stats: %w", err)
	}
	return stats, nil
}

// ListWithFilters 按统一过滤条件列出用量日志。
func (s *UsageService) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters UsageLogFilters) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, result, err := s.usageRepo.ListWithFilters(ctx, params, filters)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs with filters: %w", err)
	}
	return logs, result, nil
}

// GetGlobalStats 获取指定时间范围内的全局用量统计。
func (s *UsageService) GetGlobalStats(ctx context.Context, startTime, endTime time.Time) (*UsageStats, error) {
	stats, err := s.usageRepo.GetGlobalStats(ctx, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get global usage stats: %w", err)
	}
	return stats, nil
}

// GetStatsWithFilters 按统一过滤条件获取用量统计。
func (s *UsageService) GetStatsWithFilters(ctx context.Context, filters UsageLogFilters) (*UsageStats, error) {
	stats, err := s.usageRepo.GetStatsWithFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("get usage stats with filters: %w", err)
	}
	return stats, nil
}

// GetUserStatsWithFilters 获取用户摘要，不执行响应中会被移除的上游端点和路径查询。
func (s *UsageService) GetUserStatsWithFilters(ctx context.Context, filters UsageLogFilters) (*UsageStats, error) {
	reader := s.readers.UserStatsReader
	ok := reader != nil
	if !ok {
		return s.GetStatsWithFilters(ctx, filters)
	}
	stats, err := reader.GetUserStatsWithFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("get user usage stats with filters: %w", err)
	}
	return stats, nil
}
