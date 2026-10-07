package routing_test

import (
	"context"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"

	"github.com/TokenFlux/TokenRouter/internal/server/httpx"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceGroupAdvancedSchedulerOverrides(t *testing.T) {
	t.Run("create deep copies sparse overrides", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)
		overrides := routing.GroupAdvancedSchedulerOverrides{
			StickyWeightedEnabled: groupAdvancedSchedulerOverrideTestPointer(false),
			LBTopK:                groupAdvancedSchedulerOverrideTestPointer(3),
		}

		group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "advanced-overrides", RateMultiplier: 1,
			SchedulerType: string(routing.GroupSchedulerTypeAdvanced), AdvancedSchedulerOverrides: overrides,
		})

		require.NoError(t, err)
		require.False(t, *group.AdvancedSchedulerOverrides.StickyWeightedEnabled)
		require.Equal(t, 3, *repo.created.AdvancedSchedulerOverrides.LBTopK)
		*overrides.LBTopK = 99
		require.Equal(t, 3, *repo.created.AdvancedSchedulerOverrides.LBTopK)
	})

	t.Run("update retains omission and clears explicit empty object", func(t *testing.T) {
		existing := &routing.Group{
			ID: 7, Name: "advanced", Status: billing.StatusActive,
			SchedulerType: routing.GroupSchedulerTypeAdvanced,
			AdvancedSchedulerOverrides: routing.GroupAdvancedSchedulerOverrides{
				LBTopK: groupAdvancedSchedulerOverrideTestPointer(3),
			},
		}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := newGroupAdminForTest(repo, nil, nil)

		unchanged, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{})
		require.NoError(t, err)
		require.Equal(t, 3, *unchanged.AdvancedSchedulerOverrides.LBTopK)

		empty := routing.GroupAdvancedSchedulerOverrides{}
		cleared, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{AdvancedSchedulerOverrides: &empty})
		require.NoError(t, err)
		require.Zero(t, cleared.AdvancedSchedulerOverrides)
		require.Zero(t, repo.updated.AdvancedSchedulerOverrides)
	})

	t.Run("invalid overrides are rejected", func(t *testing.T) {
		svc := newGroupAdminForTest(&groupRepoStubForAdmin{}, nil, nil)
		_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "invalid-advanced-overrides", RateMultiplier: 1,
			AdvancedSchedulerOverrides: routing.GroupAdvancedSchedulerOverrides{
				LBTopK: groupAdvancedSchedulerOverrideTestPointer(0),
			},
		})

		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
		require.Equal(t, "INVALID_ADVANCED_SCHEDULER_OVERRIDES", apperror.Reason(err))
	})

	t.Run("merged weight overflow is rejected", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		weights := policy.ConfigScoreWeights{Priority: math.MaxFloat64 * 0.75}
		svc := newGroupAdminPortsForTest(repo, nil, nil, nil, nil, nil, &weights)

		_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "overflowing-advanced-overrides", RateMultiplier: 1,
			AdvancedSchedulerOverrides: routing.GroupAdvancedSchedulerOverrides{
				WeightLoad: groupAdvancedSchedulerOverrideTestPointer(math.MaxFloat64 * 0.75),
			},
		})

		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
		require.Equal(t, "INVALID_ADVANCED_SCHEDULER_OVERRIDES", apperror.Reason(err))
		require.Nil(t, repo.created)
	})

	t.Run("update rejects merged weight overflow", func(t *testing.T) {
		existing := &routing.Group{
			ID: 8, Name: "existing-advanced-overrides",
			Status: billing.StatusActive, SchedulerType: routing.GroupSchedulerTypeAdvanced,
		}
		repo := &groupRepoStubForAdmin{getByID: existing}
		weights := policy.ConfigScoreWeights{Priority: math.MaxFloat64 * 0.75}
		svc := newGroupAdminPortsForTest(repo, nil, nil, nil, nil, nil, &weights)
		overrides := routing.GroupAdvancedSchedulerOverrides{
			WeightLoad: groupAdvancedSchedulerOverrideTestPointer(math.MaxFloat64 * 0.75),
		}

		_, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
			AdvancedSchedulerOverrides: &overrides,
		})

		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
		require.Equal(t, "INVALID_ADVANCED_SCHEDULER_OVERRIDES", apperror.Reason(err))
		require.Nil(t, repo.updated)
	})

	t.Run("all zero base weights remain writable", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)
		zero := 0.0

		_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "zero-base-advanced-overrides", RateMultiplier: 1,
			AdvancedSchedulerOverrides: routing.GroupAdvancedSchedulerOverrides{
				WeightPriority:      &zero,
				WeightLoad:          &zero,
				WeightQueue:         &zero,
				WeightErrorRate:     &zero,
				WeightTTFT:          &zero,
				WeightReset:         &zero,
				WeightQuotaHeadroom: &zero,
			},
		})

		require.NoError(t, err)
		require.NotNil(t, repo.created)
	})
}

// groupModelsListProviderRepoStub 只实现候选模型测试需要的可调度提供商查询。
type groupModelsListProviderRepoStub struct {
	routing.GroupProviders
	providers     []provider.Record
	calledGroupID int64
}

func (s *groupModelsListProviderRepoStub) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]routing.CatalogueProvider, error) {
	s.calledGroupID = groupID
	records := append([]provider.Record(nil), s.providers...)
	for i := range records {
		if records[i].Type == "" {
			records[i].Type = capability.ProviderTypeAPIKey
		}
	}
	return gatewayprovider.CatalogueProviders(records), nil
}

// TestAdminService_GetGroupModelsListCandidates_UsesConfiguredRequestModels 确保 OpenAI-compatible 分组不会混入 OpenAI 默认模型。
func TestAdminService_GetGroupModelsListCandidates_UsesConfiguredRequestModels(t *testing.T) {
	groupID := int64(10)
	groupRepo := &groupRepoStubForAdmin{
		getByID: &routing.Group{ID: groupID},
	}
	providerRepo := &groupModelsListProviderRepoStub{
		providers: []provider.Record{
			{
				ID:       1,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"model_whitelist": []any{"deepseek-v4-pro", "deepseek-v4-flash"},
				},
			},
			{
				ID:       2,
				Platform: capability.PlatformAnthropic,
				Credentials: map[string]any{
					"model_whitelist": []any{"claude-sonnet-4-6"},
				},
			},
		},
	}
	svc := newGroupAdminPortsForTest(groupRepo, nil, nil, nil, providerRepo, nil, nil)

	models, err := svc.GetGroupModelsListCandidates(context.Background(), groupID, "")

	require.NoError(t, err)
	require.Equal(t, groupID, providerRepo.calledGroupID)
	require.Equal(t, []string{"claude-sonnet-4-6", "deepseek-v4-flash", "deepseek-v4-pro"}, models)
}

// TestGroupModelsListCandidatesApplyGroupMappingAndRestrictions 覆盖请求、分组映射和上游三个白名单阶段。
func TestGroupModelsListCandidatesApplyGroupMappingAndRestrictions(t *testing.T) {
	for _, stage := range []string{routing.BillingModelSourceRequested, routing.BillingModelSourceGroupMapped, routing.BillingModelSourceUpstream} {
		t.Run(stage, func(t *testing.T) {
			group := &routing.Group{ID: 59, RoutingPolicy: routing.GroupRoutingPolicy{
				Enabled: true, RestrictModels: true, RestrictionModelSource: stage,
				ModelMapping: map[string]string{
					"gemini-3.8-flash":  "gemini-3.8-flash-tiered",
					"gemini-3.7-flash":  "gemini-3.7-flash-tiered",
					"gemini-3.1-pro":    "gemini-3.1-pro-high",
					"unavailable-alias": "missing-model",
				},
			}}
			requested := []string{"gemini-3.1-pro", "gemini-3.7-flash", "gemini-3.8-flash"}
			mapped := []string{"gemini-3.1-pro-high", "gemini-3.7-flash-tiered", "gemini-3.8-flash-tiered"}
			allowed := requested
			if stage != routing.BillingModelSourceRequested {
				allowed = mapped
			}
			group.RoutingPolicy.AllowedModels = append(append([]string{}, allowed...), "unavailable-alias")
			providers := &groupModelsListProviderRepoStub{providers: []provider.Record{{
				ID: 3678, Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{"model_whitelist": mapped},
			}}}
			svc := newGroupAdminPortsForTest(&groupRepoStubForAdmin{getByID: group}, nil, nil, nil, providers, nil, nil)
			models, err := svc.GetGroupModelsListCandidates(context.Background(), group.ID, "")
			require.NoError(t, err)
			if stage == routing.BillingModelSourceRequested {
				require.Equal(t, requested, models)
			} else {
				require.ElementsMatch(t, append(append([]string{}, requested...), mapped...), models)
			}
			// 自定义列表筛选可请求的模型，已失效的上游名称会被排除。
			group.ModelsListConfig = routing.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-3.8-flash", "unavailable-alias"}}
			models, err = svc.GetGroupModelsListCandidates(context.Background(), group.ID, "")
			require.NoError(t, err)
			require.Equal(t, []string{"gemini-3.8-flash"}, models)
		})
	}
}

// TestAdminServiceCustomModelsCannotInventUnsupportedModels 确保已有分组不会因为 OpenAI 上游平台回退出 GPT 默认模型。
func TestAdminServiceCustomModelsCannotInventUnsupportedModels(t *testing.T) {
	groupID := int64(12)
	groupRepo := &groupRepoStubForAdmin{
		getByID: &routing.Group{
			ID: groupID,

			ModelsListConfig: routing.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			},
		},
	}
	providerRepo := &groupModelsListProviderRepoStub{
		providers: []provider.Record{
			{ID: 1, Platform: capability.PlatformOpenAI},
		},
	}
	svc := newGroupAdminPortsForTest(groupRepo, nil, nil, nil, providerRepo, nil, nil)

	models, err := svc.GetGroupModelsListCandidates(context.Background(), groupID, "")

	require.NoError(t, err)
	require.Equal(t, groupID, providerRepo.calledGroupID)
	require.Empty(t, models)
}

// TestAdminService_GetGroupModelsListCandidates_FiltersCustomModelsList 确保候选存在有限模型时按自定义模型列表取交集。
func TestAdminService_GetGroupModelsListCandidates_FiltersCustomModelsList(t *testing.T) {
	groupID := int64(13)
	groupRepo := &groupRepoStubForAdmin{
		getByID: &routing.Group{
			ID: groupID,

			ModelsListConfig: routing.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"deepseek-v4-pro", "gpt-5.5", "deepseek-v4-flash"},
			},
		},
	}
	providerRepo := &groupModelsListProviderRepoStub{
		providers: []provider.Record{
			{
				ID:       1,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"model_whitelist": []any{"deepseek-v4-flash", "deepseek-v4-pro"},
				},
			},
		},
	}
	svc := newGroupAdminPortsForTest(groupRepo, nil, nil, nil, providerRepo, nil, nil)

	models, err := svc.GetGroupModelsListCandidates(context.Background(), groupID, "")

	require.NoError(t, err)
	require.Equal(t, []string{"deepseek-v4-pro", "deepseek-v4-flash"}, models)
}

// TestAdminServiceGetGroupModelsListCandidatesKeepsEmptyIntersection 确保编辑时切换平台不会沿用旧平台的自定义模型。
func TestAdminServiceGetGroupModelsListCandidatesKeepsEmptyIntersection(t *testing.T) {
	groupID := int64(14)
	groupRepo := &groupRepoStubForAdmin{
		getByID: &routing.Group{
			ID: groupID,

			ModelsListConfig: routing.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"deepseek-v4-flash"},
			},
		},
	}
	providerRepo := &groupModelsListProviderRepoStub{
		providers: []provider.Record{
			{
				ID:       1,
				Platform: capability.PlatformAnthropic,
				Credentials: map[string]any{
					"model_whitelist": []any{"claude-sonnet-4-6"},
				},
			},
		},
	}
	svc := newGroupAdminPortsForTest(groupRepo, nil, nil, nil, providerRepo, nil, nil)

	models, err := svc.GetGroupModelsListCandidates(context.Background(), groupID, "")

	require.NoError(t, err)
	require.Empty(t, models)
}

// TestAdminService_GetGroupModelsListCandidates_FallsBackToPlatformDefaults 默认目录仍受分组协议限制。
func TestAdminService_GetGroupModelsListCandidates_FallsBackToPlatformDefaults(t *testing.T) {
	groupID := int64(11)
	groupRepo := &groupRepoStubForAdmin{
		getByID: &routing.Group{ID: groupID},
	}
	providerRepo := &groupModelsListProviderRepoStub{
		providers: []provider.Record{
			{ID: 1, Platform: capability.PlatformOpenAI},
		},
	}
	svc := newGroupAdminPortsForTest(groupRepo, nil, nil, nil, providerRepo, nil, nil)

	models, err := svc.GetGroupModelsListCandidates(context.Background(), groupID, "")

	require.NoError(t, err)
	expected := provideradapter.DefaultProviderModels(&provider.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey})
	expected = slices.DeleteFunc(expected, func(model string) bool { return strings.HasPrefix(model, "text-embedding-") })
	require.ElementsMatch(t, expected, models)
	require.NotContains(t, models, "text-embedding-3-small")
}

func TestAdminService_CreateGroup_AppendsSortOrder(t *testing.T) {
	repo := &groupRepoStubForAdmin{
		listWithFiltersGroups: []routing.Group{{ID: 9, SortOrder: 40}},
	}
	svc := newGroupAdminPortsForTest(repo, nil, nil, repo, nil, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name:           "appended-group",
		RateMultiplier: 1,
	})

	require.NoError(t, err)
	require.Equal(t, 50, group.SortOrder)
	require.Equal(t, 50, repo.created.SortOrder)
	require.Equal(t, 1, repo.groupSortOrderLockCalls)
	require.Equal(t, pagination.PaginationParams{
		Page:      1,
		PageSize:  1,
		SortBy:    "sort_order",
		SortOrder: "desc",
	}, repo.listWithFiltersParams)
}

func TestAdminService_CreateGroup_PreservesExplicitSortOrder(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := newGroupAdminPortsForTest(repo, nil, nil, repo, nil, nil, nil)
	explicitSortOrder := 5

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name:           "explicit-sort-group",
		RateMultiplier: 1,
		SortOrder:      &explicitSortOrder,
	})

	require.NoError(t, err)
	require.Equal(t, explicitSortOrder, group.SortOrder)
	require.Equal(t, 0, repo.groupSortOrderLockCalls)
	require.Equal(t, 0, repo.listWithFiltersCalls)
}

// TestAdminService_UpdateGroup_OpenAIFastInvalidatesAuthCache 验证缓存快照中的两个
// Fast 字段更新后会沿用现有分组级缓存失效边界。
func TestAdminService_UpdateGroup_OpenAIFastInvalidatesAuthCache(t *testing.T) {
	existingGroup := &routing.Group{ID: 1, Name: "existing-fast", Status: billing.StatusActive}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	invalidator := &authCacheInvalidatorStub{}
	svc := newGroupAdminPortsForTest(repo, nil, nil, nil, nil, invalidator, nil)
	enabled := true

	group, err := svc.UpdateGroup(context.Background(), existingGroup.ID, &routing.UpdateGroupInput{
		ForceOpenAIFast: &enabled,
	})

	require.NoError(t, err)
	require.NotNil(t, group)
	require.True(t, repo.updated.ForceOpenAIFast)
	require.Equal(t, []int64{existingGroup.ID}, invalidator.groupIDs)
}

func TestAdminService_UpdateGroup_InvalidatesAuthCacheOnRPMLimitChange(t *testing.T) {
	existingGroup := &routing.Group{
		ID:   1,
		Name: "existing-group",

		Status:   billing.StatusActive,
		RPMLimit: 10,
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	invalidator := &authCacheInvalidatorStub{}
	svc := newGroupAdminPortsForTest(repo, nil, nil, nil, nil, invalidator, nil)

	rpmLimit := 60
	group, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{
		RPMLimit: &rpmLimit,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, 60, repo.updated.RPMLimit)
	require.Equal(t, []int64{1}, invalidator.groupIDs, "分组 RPMLimit 写入 auth snapshot，变更后必须失效 API Key 认证缓存")
}

// TestAdminGroupOpenAIFastPolicy 验证新策略必须优先于旧布尔输入，并在更新、缓存失效和平台切换中完整保留。
func TestAdminGroupOpenAIFastPolicy(t *testing.T) {
	for _, policy := range []string{"follow_request", "force_priority", "force_ultrafast", "force_off"} {
		t.Run(policy, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{}
			invalidator := &authCacheInvalidatorStub{}
			svc := newGroupAdminPortsForTest(repo, nil, nil, nil, nil, invalidator, nil)
			group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{Name: "fast", RateMultiplier: 1, ForceOpenAIFast: true, OpenAIFastPolicy: &policy})
			require.NoError(t, err)
			require.Equal(t, policy, group.OpenAIFastPolicy)
			require.Equal(t, policy == "force_priority", group.ForceOpenAIFast)
			group.ID = 1
			repo.getByID = group
			kept, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{})
			require.NoError(t, err)
			require.Equal(t, policy, kept.OpenAIFastPolicy)
			off := false
			changed, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{ForceOpenAIFast: &off})
			require.NoError(t, err)
			require.Equal(t, "follow_request", changed.OpenAIFastPolicy)
			changed, err = svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{OpenAIFastPolicy: &policy})
			require.NoError(t, err)
			require.Equal(t, policy, changed.OpenAIFastPolicy)
			require.Contains(t, invalidator.groupIDs, int64(1))
			changed, err = svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{})
			require.NoError(t, err)
			require.Equal(t, policy, changed.OpenAIFastPolicy)
		})
	}
}

// groupAdvancedSchedulerOverrideTestPointer 保留稀疏字段的显式存在性。
func groupAdvancedSchedulerOverrideTestPointer[T any](value T) *T { return &value }

// authCacheInvalidatorStub 只记录管理操作的提交后失效。
type authCacheInvalidatorStub struct {
	groupIDs []int64
	keys     []string
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByGroupID(_ context.Context, id int64) {
	s.groupIDs = append(s.groupIDs, id)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByKey(_ context.Context, key string) {
	s.keys = append(s.keys, key)
}
