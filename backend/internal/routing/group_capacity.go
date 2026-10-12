package routing

import (
	"context"
	"slices"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
)

type GroupCapacitySummary = accessview.GroupCapacitySummary

type groupCapacityProviderRef struct {
	groupID    int64
	providerID int64
}

type CapacityProviderRow struct {
	GroupID  int64
	Provider provider.CapacitySnapshot
}
type CapacityProviders interface {
	ListSchedulableByGroupID(context.Context, int64) ([]provider.CapacitySnapshot, error)
}
type CapacityBatchProviders interface {
	ListSchedulableCapacityByGroupIDs(context.Context, []int64) ([]CapacityProviderRow, error)
}
type CapacityGroups interface {
	ListActiveIDs(context.Context) ([]int64, error)
}
type CapacityConcurrency interface {
	GetProviderConcurrencyBatch(context.Context, []int64) (map[int64]int, error)
}
type CapacitySessions interface {
	GetActiveSessionCountBatch(context.Context, []int64, map[int64]time.Duration) (map[int64]int, error)
}
type CapacityRPM interface {
	GetRPMBatch(context.Context, []int64) (map[int64]int, error)
}

// CapacityService 聚合提供商配置和运行计数，计算分组容量。
type CapacityService struct {
	providerRepo       CapacityProviders
	groupRepo          CapacityGroups
	concurrencyService CapacityConcurrency
	sessionLimitCache  CapacitySessions
	rpmCache           CapacityRPM
}

func NewCapacityService(providers CapacityProviders, groups CapacityGroups, concurrency CapacityConcurrency, sessions CapacitySessions, rpm CapacityRPM) *CapacityService {
	return &CapacityService{providerRepo: providers, groupRepo: groups, concurrencyService: concurrency, sessionLimitCache: sessions, rpmCache: rpm}
}

// GetAllGroupCapacity 返回全部活跃分组的容量摘要。
func (s *CapacityService) GetAllGroupCapacity(ctx context.Context) ([]GroupCapacitySummary, error) {
	groupIDs, err := s.groupRepo.ListActiveIDs(ctx)
	if err != nil {
		return nil, err
	}

	if lister, ok := s.providerRepo.(CapacityBatchProviders); ok {
		return s.getGroupCapacitiesBatch(ctx, groupIDs, lister)
	}

	return s.getGroupCapacitiesSequential(ctx, groupIDs), nil
}

func (s *CapacityService) getGroupCapacitiesSequential(ctx context.Context, groupIDs []int64) []GroupCapacitySummary {
	results := make([]GroupCapacitySummary, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		cap, err := s.GetGroupCapacity(ctx, groupID)
		if err != nil {
			// 单个分组失败时跳过，保留其它分组的部分结果。
			continue
		}
		cap.GroupID = groupID
		results = append(results, cap)
	}
	return results
}

func (s *CapacityService) getGroupCapacitiesBatch(ctx context.Context, groupIDs []int64, lister CapacityBatchProviders) ([]GroupCapacitySummary, error) {
	results := make([]GroupCapacitySummary, len(groupIDs))
	groupIndex := make(map[int64]int, len(groupIDs))
	for i, groupID := range groupIDs {
		results[i].GroupID = groupID
		groupIndex[groupID] = i
	}
	if len(groupIDs) == 0 {
		return results, nil
	}

	rows, err := lister.ListSchedulableCapacityByGroupIDs(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return results, nil
	}

	refs := make([]groupCapacityProviderRef, 0, len(rows))
	seenGroupProvider := make(map[groupCapacityProviderRef]struct{}, len(rows))
	providerIDSet := make(map[int64]struct{}, len(rows))
	providerIDs := make([]int64, 0, len(rows))
	sessionTimeouts := make(map[int64]time.Duration)

	for _, row := range rows {
		idx, ok := groupIndex[row.GroupID]
		if !ok || row.Provider.ID <= 0 {
			continue
		}

		acc := row.Provider
		if acc.QuotaAutoPaused {
			continue
		}

		ref := groupCapacityProviderRef{groupID: row.GroupID, providerID: row.Provider.ID}
		if _, ok := seenGroupProvider[ref]; ok {
			continue
		}
		seenGroupProvider[ref] = struct{}{}
		refs = append(refs, ref)

		if _, ok := providerIDSet[row.Provider.ID]; !ok {
			providerIDSet[row.Provider.ID] = struct{}{}
			providerIDs = append(providerIDs, row.Provider.ID)
		}

		results[idx].ConcurrencyMax += acc.Concurrency

		if maxSessions := acc.MaxSessions; maxSessions > 0 {
			results[idx].SessionsMax += maxSessions
			timeout := time.Duration(acc.SessionIdleTimeoutMinutes) * time.Minute
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			sessionTimeouts[acc.ID] = timeout
		}

		if rpm := acc.BaseRPM; rpm > 0 {
			results[idx].RPMMax += rpm
		}
	}

	if len(providerIDs) == 0 {
		return results, nil
	}

	concurrencyMap := map[int64]int{}
	if s.concurrencyService != nil {
		concurrencyMap, _ = s.concurrencyService.GetProviderConcurrencyBatch(ctx, providerIDs)
	}

	sessionProviderIDs := providerIDsForGroupsWithLimit(refs, groupIndex, results, func(summary GroupCapacitySummary) bool {
		return summary.SessionsMax > 0
	})
	var sessionsMap map[int64]int
	if len(sessionProviderIDs) > 0 && s.sessionLimitCache != nil {
		sessionsMap, _ = s.sessionLimitCache.GetActiveSessionCountBatch(ctx, sessionProviderIDs, sessionTimeouts)
	}

	rpmProviderIDs := providerIDsForGroupsWithLimit(refs, groupIndex, results, func(summary GroupCapacitySummary) bool {
		return summary.RPMMax > 0
	})
	var rpmMap map[int64]int
	if len(rpmProviderIDs) > 0 && s.rpmCache != nil {
		rpmMap, _ = s.rpmCache.GetRPMBatch(ctx, rpmProviderIDs)
	}

	for _, ref := range refs {
		idx := groupIndex[ref.groupID]
		results[idx].ConcurrencyUsed += concurrencyMap[ref.providerID]
		if sessionsMap != nil && results[idx].SessionsMax > 0 {
			results[idx].SessionsUsed += sessionsMap[ref.providerID]
		}
		if rpmMap != nil && results[idx].RPMMax > 0 {
			results[idx].RPMUsed += rpmMap[ref.providerID]
		}
	}
	return results, nil
}

// GetGroupCapacityByIDs 返回指定分组的容量摘要，仓储支持批量读取时执行一次聚合查询。
func (s *CapacityService) GetGroupCapacityByIDs(ctx context.Context, groupIDs []int64) (map[int64]GroupCapacitySummary, error) {
	results := make(map[int64]GroupCapacitySummary, len(groupIDs))
	if s == nil || len(groupIDs) == 0 {
		return results, nil
	}

	normalized := make([]int64, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}

		if err := ctx.Err(); err != nil {
			return results, err
		}
		normalized = append(normalized, groupID)
	}

	if lister, ok := s.providerRepo.(CapacityBatchProviders); ok {
		summaries, err := s.getGroupCapacitiesBatch(ctx, normalized, lister)
		if err != nil {
			return results, err
		}
		for _, summary := range summaries {
			results[summary.GroupID] = summary
		}
		return results, nil
	}

	for _, groupID := range normalized {
		capacity, err := s.GetGroupCapacity(ctx, groupID)
		if err != nil {
			continue
		}
		capacity.GroupID = groupID
		results[groupID] = capacity
	}

	return results, nil
}

func providerIDsForGroupsWithLimit(refs []groupCapacityProviderRef, groupIndex map[int64]int, summaries []GroupCapacitySummary, include func(GroupCapacitySummary) bool) []int64 {
	seen := make(map[int64]struct{})
	providerIDs := make([]int64, 0)
	for _, ref := range refs {
		idx, ok := groupIndex[ref.groupID]
		if !ok || !include(summaries[idx]) {
			continue
		}
		if _, ok := seen[ref.providerID]; ok {
			continue
		}
		seen[ref.providerID] = struct{}{}
		providerIDs = append(providerIDs, ref.providerID)
	}
	return providerIDs
}

func (s *CapacityService) GetGroupCapacity(ctx context.Context, groupID int64) (GroupCapacitySummary, error) {
	providers, err := s.providerRepo.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return GroupCapacitySummary{}, err
	}
	if len(providers) == 0 {
		return GroupCapacitySummary{}, nil
	}
	providers = slices.DeleteFunc(providers, func(a provider.CapacitySnapshot) bool { return a.QuotaAutoPaused })
	if len(providers) == 0 {
		return GroupCapacitySummary{}, nil
	}

	// 收集提供商 ID 和容量配置。
	providerIDs := make([]int64, 0, len(providers))
	sessionTimeouts := make(map[int64]time.Duration)
	var concurrencyMax, sessionsMax, rpmMax int

	for i := range providers {
		acc := &providers[i]
		providerIDs = append(providerIDs, acc.ID)
		concurrencyMax += acc.Concurrency

		if ms := acc.MaxSessions; ms > 0 {
			sessionsMax += ms
			timeout := time.Duration(acc.SessionIdleTimeoutMinutes) * time.Minute
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			sessionTimeouts[acc.ID] = timeout
		}

		if rpm := acc.BaseRPM; rpm > 0 {
			rpmMax += rpm
		}
	}

	// 批量查询运行时容量数据，缓存异常时该指标留空，其他指标继续展示。
	concurrencyMap := map[int64]int{}
	if s.concurrencyService != nil {
		concurrencyMap, _ = s.concurrencyService.GetProviderConcurrencyBatch(ctx, providerIDs)
	}

	var sessionsMap map[int64]int
	if sessionsMax > 0 && s.sessionLimitCache != nil {
		sessionsMap, _ = s.sessionLimitCache.GetActiveSessionCountBatch(ctx, providerIDs, sessionTimeouts)
	}

	var rpmMap map[int64]int
	if rpmMax > 0 && s.rpmCache != nil {
		rpmMap, _ = s.rpmCache.GetRPMBatch(ctx, providerIDs)
	}

	// 聚合提供商级容量为分组级容量。
	var concurrencyUsed, sessionsUsed, rpmUsed int
	for _, id := range providerIDs {
		concurrencyUsed += concurrencyMap[id]
		if sessionsMap != nil {
			sessionsUsed += sessionsMap[id]
		}
		if rpmMap != nil {
			rpmUsed += rpmMap[id]
		}
	}

	return GroupCapacitySummary{
		ConcurrencyUsed: concurrencyUsed,
		ConcurrencyMax:  concurrencyMax,
		SessionsUsed:    sessionsUsed,
		SessionsMax:     sessionsMax,
		RPMUsed:         rpmUsed,
		RPMMax:          rpmMax,
	}, nil
}
