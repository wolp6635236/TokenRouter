package routing_test

import (
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/protocol"

	context "context"

	http "net/http"

	testing "testing"

	pagination "github.com/TokenFlux/TokenRouter/internal/pkg/pagination"

	"github.com/TokenFlux/TokenRouter/internal/server/httpx"

	require "github.com/stretchr/testify/require"
)

func ptrGroupClientProtocols(value []protocol.ProtocolID) *[]protocol.ProtocolID {
	return &value
}

// TestAdminServiceCreateGroupUsesUnifiedClientProtocolDefaults 验证新分组统一开放三个文本协议，非文本入口保持关闭。
func TestAdminServiceCreateGroupUsesUnifiedClientProtocolDefaults(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	group, err := newGroupAdminForTest(repo, nil, nil).CreateGroup(context.Background(), &routing.CreateGroupInput{Name: "mixed", RateMultiplier: 1})
	require.NoError(t, err)
	require.Equal(t, []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions}, group.AllowedProtocols)
	require.Empty(t, group.ProtocolFallbacks)
	require.False(t, group.AllowImageGeneration)
	require.False(t, group.AllowBatchImageGeneration)
	require.False(t, group.AllowLive)
}

func TestAdminServiceGroupAvailabilityProbeConfigReturnsBadRequest(t *testing.T) {
	invalidRetries := routing.MaxGroupAvailabilityProbeMaxRetries + 1
	invalidConfig := routing.GroupAvailabilityProbeConfig{
		Enabled:    true,
		ModelID:    "gpt-5.4",
		Prompt:     "hi",
		MaxRetries: &invalidRetries,
	}

	t.Run("create rejects invalid config", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)

		_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "invalid-probe", RateMultiplier: 1,
			AvailabilityProbeConfig: invalidConfig,
		})

		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
		require.Equal(t, routing.InvalidGroupAvailabilityProbeConfigReason, apperror.Reason(err))
		require.Nil(t, repo.created)
	})

	t.Run("update rejects invalid config", func(t *testing.T) {
		existing := &routing.Group{ID: 7, Name: "existing", Status: billing.StatusActive}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := newGroupAdminForTest(repo, nil, nil)

		_, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
			AvailabilityProbeConfig: &invalidConfig,
		})

		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
		require.Equal(t, routing.InvalidGroupAvailabilityProbeConfigReason, apperror.Reason(err))
		require.Nil(t, repo.updated)
	})
}

func TestAdminServiceGroupSchedulerTypeDefaultsValidatesAndUpdates(t *testing.T) {
	t.Run("create defaults to basic", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)

		group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "default-scheduler", RateMultiplier: 1,
		})

		require.NoError(t, err)
		require.Equal(t, routing.GroupSchedulerTypeBasic, group.SchedulerType)
		require.Equal(t, routing.GroupSchedulerTypeBasic, repo.created.SchedulerType)
	})

	t.Run("create accepts advanced", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)

		group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "advanced-scheduler", RateMultiplier: 1, SchedulerType: string(routing.GroupSchedulerTypeAdvanced),
		})

		require.NoError(t, err)
		require.Equal(t, routing.GroupSchedulerTypeAdvanced, group.SchedulerType)
	})

	t.Run("invalid value is rejected", func(t *testing.T) {
		svc := newGroupAdminForTest(&groupRepoStubForAdmin{}, nil, nil)

		_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "invalid-scheduler", RateMultiplier: 1, SchedulerType: "weighted",
		})

		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
		require.Equal(t, "INVALID_SCHEDULER_TYPE", apperror.Reason(err))
	})

	t.Run("update preserves explicit advanced choice", func(t *testing.T) {
		existing := &routing.Group{ID: 7, Name: "basic", Status: billing.StatusActive, SchedulerType: routing.GroupSchedulerTypeBasic}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := newGroupAdminForTest(repo, nil, nil)
		advanced := string(routing.GroupSchedulerTypeAdvanced)

		group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{SchedulerType: &advanced})

		require.NoError(t, err)
		require.Equal(t, routing.GroupSchedulerTypeAdvanced, group.SchedulerType)
		require.Equal(t, routing.GroupSchedulerTypeAdvanced, repo.updated.SchedulerType)
	})
}

func TestAdminServiceCreateGroupClientProtocolCompatibilityPrecedence(t *testing.T) {
	t.Run("legacy OpenAI switch is accepted when new field is omitted", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)

		group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "legacy", RateMultiplier: 1, AllowMessagesDispatch: true,
		})

		require.NoError(t, err)
		require.Equal(t, []protocol.ProtocolID{
			protocol.ProtocolAnthropicMessages,
			protocol.ProtocolOpenAIResponses,
			protocol.ProtocolOpenAIChatCompletions,
		}, group.AllowedProtocols)
		require.True(t, group.AllowMessagesDispatch)
	})

	t.Run("new field wins over legacy OpenAI switch", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{}
		svc := newGroupAdminForTest(repo, nil, nil)

		group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
			Name: "new-field",

			RateMultiplier:        1,
			AllowMessagesDispatch: true,
			AllowedProtocols: []protocol.ProtocolID{
				protocol.ProtocolOpenAIChatCompletions,
				protocol.ProtocolOpenAIResponses,
			},
		})

		require.NoError(t, err)
		require.Equal(t, []protocol.ProtocolID{
			protocol.ProtocolOpenAIResponses,
			protocol.ProtocolOpenAIChatCompletions,
		}, group.AllowedProtocols)
		require.False(t, group.AllowMessagesDispatch)
	})
}

func TestAdminServiceRejectsInvalidGroupClientProtocols(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		protocols []protocol.ProtocolID
	}{
		{"unknown", capability.PlatformQoder, []protocol.ProtocolID{"unknown"}},
		{"duplicate", capability.PlatformQoder, []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolAnthropicMessages}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newGroupAdminForTest(&groupRepoStubForAdmin{}, nil, nil)
			_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
				Name: tt.name, RateMultiplier: 1, AllowedProtocols: tt.protocols,
			})

			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
			require.Equal(t, "INVALID_ALLOWED_CLIENT_PROTOCOLS", apperror.Reason(err))
		})
	}
}

func TestAdminServiceAllowsEmptyGroupClientProtocolsForEveryPlatform(t *testing.T) {
	platforms := []string{
		capability.PlatformAnthropic,
		capability.PlatformOpenAI,
		capability.PlatformGemini,
		capability.PlatformAntigravity,
		capability.PlatformQoder,
		capability.PlatformGrok,
	}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			svc := newGroupAdminForTest(&groupRepoStubForAdmin{}, nil, nil)

			group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
				Name: platform, RateMultiplier: 1, AllowedProtocols: []protocol.ProtocolID{},
			})

			require.NoError(t, err)
			require.NotNil(t, group.AllowedProtocols)
			require.Empty(t, group.AllowedProtocols)
		})
	}
}

func TestAdminServiceUpdateGroupPreservesExplicitEmptyClientProtocols(t *testing.T) {
	existing := &routing.Group{ID: 1, Name: "openai", Status: billing.StatusActive, AllowedProtocols: []protocol.ProtocolID{}}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{})

	require.NoError(t, err)
	require.NotNil(t, group.AllowedProtocols)
	require.Empty(t, group.AllowedProtocols)
}

// TestAdminServiceUpdateGroupPreservesAllConfiguredProtocols 验证更新名称或其他策略不会隐式修改客户端入口。
func TestAdminServiceUpdateGroupPreservesAllConfiguredProtocols(t *testing.T) {
	protocols := []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolOpenAIResponses, protocol.ProtocolGeminiGenerateContent, protocol.ProtocolImageBatches}
	repo := &groupRepoStubForAdmin{getByID: &routing.Group{ID: 1, Name: "before", Status: billing.StatusActive, AllowedProtocols: protocols}}
	group, err := newGroupAdminForTest(repo, nil, nil).UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{Name: "after"})
	require.NoError(t, err)
	require.Equal(t, protocols, group.AllowedProtocols)
	require.True(t, group.AllowBatchImageGeneration)
}

func TestAdminServiceUpdateGroupNewClientProtocolsOverrideLegacySwitch(t *testing.T) {
	existing := &routing.Group{
		ID: 1, Name: "openai", Status: billing.StatusActive,
		AllowedProtocols: []protocol.ProtocolID{protocol.ProtocolOpenAIResponses, protocol.ProtocolOpenAIChatCompletions},
	}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := newGroupAdminForTest(repo, nil, nil)
	legacyEnabled := false

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		AllowedProtocols: ptrGroupClientProtocols([]protocol.ProtocolID{
			protocol.ProtocolAnthropicMessages,
			protocol.ProtocolOpenAIResponses,
			protocol.ProtocolOpenAIChatCompletions,
		}),
		AllowMessagesDispatch: &legacyEnabled,
	})

	require.NoError(t, err)
	require.True(t, group.AllowMessagesDispatch)
	require.True(t, group.AllowsClientProtocol(protocol.ProtocolAnthropicMessages))
}

// groupRepoStubForAdmin 用于测试 AdminService 的 GroupRepository Stub
type groupRepoStubForAdmin struct {
	created *routing.
		Group
	updated *routing.
		Group
	getByID *routing.
		Group
	getErr error // GetByID 返回的错误

	listWithFiltersCalls       int
	listWithFiltersParams      pagination.PaginationParams
	listWithFiltersPlatform    string
	listWithFiltersStatus      string
	listWithFiltersSearch      string
	listWithFiltersIsExclusive *bool
	listWithFiltersGroups      []routing.Group
	listWithFiltersResult      *pagination.PaginationResult
	listWithFiltersErr         error
	groupSortOrderLockCalls    int
}

func (s *groupRepoStubForAdmin) Create(_ context.Context, g *routing.Group) error {
	s.created = g
	return nil
}

func (s *groupRepoStubForAdmin) Update(_ context.Context, g *routing.Group) error {
	s.updated = g
	return nil
}

func (s *groupRepoStubForAdmin) GetByID(_ context.Context, _ int64) (*routing.Group, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getByID, nil
}

func (s *groupRepoStubForAdmin) GetByIDLite(_ context.Context, _ int64) (*routing.Group, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getByID, nil
}

func (s *groupRepoStubForAdmin) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete call")
}

func (s *groupRepoStubForAdmin) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}

func (s *groupRepoStubForAdmin) List(_ context.Context, _ pagination.PaginationParams) ([]routing.Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *groupRepoStubForAdmin) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]routing.Group, *pagination.PaginationResult, error) {
	s.listWithFiltersCalls++
	s.listWithFiltersParams = params
	s.listWithFiltersPlatform = platform
	s.listWithFiltersStatus = status
	s.listWithFiltersSearch = search
	s.listWithFiltersIsExclusive = isExclusive

	if s.listWithFiltersErr != nil {
		return nil, nil, s.listWithFiltersErr
	}

	result := s.listWithFiltersResult
	if result == nil {
		result = &pagination.PaginationResult{
			Total:    int64(len(s.listWithFiltersGroups)),
			Page:     params.Page,
			PageSize: params.PageSize,
		}
	}

	return s.listWithFiltersGroups, result, nil
}

func (s *groupRepoStubForAdmin) ListActive(_ context.Context) ([]routing.Group, error) {
	panic("unexpected ListActive call")
}

func (s *groupRepoStubForAdmin) ListActiveByPlatform(_ context.Context, _ string) ([]routing.Group, error) {
	panic("unexpected ListActiveByPlatform call")
}

func (s *groupRepoStubForAdmin) ListActiveByPlatformLite(_ context.Context, _ string) ([]routing.Group, error) {
	panic("unexpected ListActiveByPlatformLite call")
}

func (s *groupRepoStubForAdmin) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName call")
}

func (s *groupRepoStubForAdmin) GetProviderCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetProviderCount call")
}

func (s *groupRepoStubForAdmin) DeleteProviderGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	panic("unexpected DeleteProviderGroupsByGroupID call")
}

func (s *groupRepoStubForAdmin) BindProvidersToGroup(_ context.Context, _ int64, _ []int64) error {
	panic("unexpected BindProvidersToGroup call")
}

func (s *groupRepoStubForAdmin) GetProviderIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	panic("unexpected GetProviderIDsByGroupIDs call")
}

func (s *groupRepoStubForAdmin) UpdateSortOrders(_ context.Context, _ []routing.GroupSortOrderUpdate) error {
	return nil
}

// LockGroupSortOrder 记录创建流程是否申请了排序位置锁。
func (s *groupRepoStubForAdmin) LockGroupSortOrder(_ context.Context) error {
	s.groupSortOrderLockCalls++
	return nil
}

func TestAdminService_ListGroups_PassesSortParams(t *testing.T) {
	repo := &groupRepoStubForAdmin{
		listWithFiltersGroups: []routing.Group{{ID: 1, Name: "g1"}},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, _, err := svc.ListGroups(context.Background(), 3, 25, capability.PlatformOpenAI, billing.StatusActive, "needle", nil, "provider_count", "ASC")
	require.NoError(t, err)
	require.Equal(t, pagination.PaginationParams{
		Page:      3,
		PageSize:  25,
		SortBy:    "provider_count",
		SortOrder: "ASC",
	}, repo.listWithFiltersParams)
}

func TestAdminService_ListGroups_PassesSessionIsolationSortParams(t *testing.T) {
	repo := &groupRepoStubForAdmin{
		listWithFiltersGroups: []routing.Group{{ID: 1, Name: "g1"}},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, _, err := svc.ListGroups(context.Background(), 1, 20, "", "", "", nil, "session_isolation_enabled", "DESC")
	require.NoError(t, err)
	require.Equal(t, pagination.PaginationParams{
		Page:      1,
		PageSize:  20,
		SortBy:    "session_isolation_enabled",
		SortOrder: "DESC",
	}, repo.listWithFiltersParams)
}

func TestAdminService_CreateGroup_PreservesNonGrokImageGenerationDisabled(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name:        "anthropic-text",
		Description: "Anthropic text group",

		RateMultiplier: 1.0,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.False(t, repo.created.AllowImageGeneration)
	require.False(t, group.AllowImageGeneration)
}

func TestAdminService_CreateGroup_WithSessionIsolation(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name: "isolated-group",

		RateMultiplier:          1.0,
		SessionIsolationEnabled: true,
	})

	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.True(t, repo.created.SessionIsolationEnabled)
	require.True(t, group.SessionIsolationEnabled)
}

func TestAdminService_CreateGroup_DisablesBatchImageWhenImageGenerationDisabled(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name:        "gemini-no-image",
		Description: "Gemini group without image generation",

		RateMultiplier:            1.0,
		AllowImageGeneration:      false,
		AllowBatchImageGeneration: true,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)

	require.False(t, repo.created.AllowImageGeneration)
	require.False(t, repo.created.AllowBatchImageGeneration)
	require.False(t, group.AllowBatchImageGeneration)
}

// TestAdminServiceCreateGroupAllowsExplicitBatchProtocol 验证批量图片准入独立于提供商平台，实际provider在创建任务时选择。
func TestAdminServiceCreateGroupAllowsExplicitBatchProtocol(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	group, err := newGroupAdminForTest(repo, nil, nil).CreateGroup(context.Background(), &routing.CreateGroupInput{Name: "batch", RateMultiplier: 1, AllowedProtocols: []protocol.ProtocolID{protocol.ProtocolImageBatches}})
	require.NoError(t, err)
	require.True(t, group.AllowBatchImageGeneration)
}

// TestAdminServiceCreateGroupPreservesFastPolicies 验证功能策略保存于分组，执行时仅由适用提供商使用。
func TestAdminServiceCreateGroupPreservesFastPolicies(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	group, err := newGroupAdminForTest(repo, nil, nil).CreateGroup(context.Background(), &routing.CreateGroupInput{Name: "fast", RateMultiplier: 1, ForceOpenAIFast: true})
	require.NoError(t, err)
	require.True(t, group.ForceOpenAIFast)
}

func TestAdminServiceUpdateGroupPreservesFastPolicies(t *testing.T) {
	existingGroup := &routing.Group{
		ID: 1, Name: "existing-fast", Status: billing.StatusActive,
		ForceOpenAIFast: true,
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.UpdateGroup(context.Background(), existingGroup.ID, &routing.UpdateGroupInput{})

	require.NoError(t, err)
	require.NotNil(t, group)
	require.True(t, repo.updated.ForceOpenAIFast)
}

func TestAdminService_UpdateGroup_PreservesImageGenerationControlsWhenOmitted(t *testing.T) {
	existingGroup := &routing.Group{
		ID:   1,
		Name: "existing-group",

		Status:               billing.StatusActive,
		AllowImageGeneration: true,
		AllowedProtocols:     []protocol.ProtocolID{"openai_images_generations", "openai_images_edits"},
		ProtocolFallbacks:    map[protocol.ProtocolID][]protocol.ProtocolID{},
		ResponsesImagePolicy: "inherit",
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	svc := newGroupAdminForTest(repo, nil, nil)

	updatedDesc := "updated"
	group, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{
		Description: &updatedDesc,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.True(t, repo.updated.AllowImageGeneration)
}

func TestAdminService_UpdateGroup_WithSessionIsolation(t *testing.T) {
	existingGroup := &routing.Group{
		ID:   1,
		Name: "existing-group",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	svc := newGroupAdminForTest(repo, nil, nil)
	enabled := true

	group, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{
		SessionIsolationEnabled: &enabled,
	})

	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.True(t, repo.updated.SessionIsolationEnabled)
	require.True(t, group.SessionIsolationEnabled)
}

func TestAdminService_UpdateGroup_DisablesBatchImageWhenImageGenerationDisabled(t *testing.T) {
	existingGroup := &routing.Group{
		ID:   1,
		Name: "existing-gemini",

		Status:                    billing.StatusActive,
		AllowImageGeneration:      true,
		AllowBatchImageGeneration: true,
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	svc := newGroupAdminForTest(repo, nil, nil)
	disabled := false

	group, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{
		AllowImageGeneration: &disabled,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.False(t, repo.updated.AllowImageGeneration)
	require.False(t, repo.updated.AllowBatchImageGeneration)
	require.False(t, group.AllowBatchImageGeneration)
}

func TestAdminService_UpdateGroup_ClearsDescriptionWhenEmptyString(t *testing.T) {
	existingGroup := &routing.Group{
		ID:          1,
		Name:        "existing-group",
		Description: "Auto-created default group",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	svc := newGroupAdminForTest(repo, nil, nil)

	empty := ""
	_, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{
		Description: &empty,
	})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, "", repo.updated.Description, "空字符串应清空分组描述")
}

func TestAdminService_UpdateGroup_PreservesDescriptionWhenNil(t *testing.T) {
	existingGroup := &routing.Group{
		ID:          1,
		Name:        "existing-group",
		Description: "keep me",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForAdmin{getByID: existingGroup}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, err := svc.UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{
		Description: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, "keep me", repo.updated.Description, "nil 应保留原有分组描述")
}

func TestAdminService_UpdateGroup_ReasoningEffortMappingsTriState(t *testing.T) {
	tests := []struct {
		name  string
		input *routing.UpdateGroupInput
		want  []routing.ReasoningEffortMapping
	}{
		{
			name:  "nil preserves existing mappings",
			input: &routing.UpdateGroupInput{},
			want:  []routing.ReasoningEffortMapping{{From: "max", To: "xhigh"}},
		},
		{
			name: "empty array clears mappings",
			input: func() *routing.UpdateGroupInput {
				empty := []routing.ReasoningEffortMapping{}
				return &routing.UpdateGroupInput{ReasoningEffortMappings: &empty}
			}(),
			want: []routing.ReasoningEffortMapping{},
		},
		{
			name: "non empty array replaces and canonicalizes mappings",
			input: func() *routing.UpdateGroupInput {
				replacement := []routing.ReasoningEffortMapping{{From: " X-HIGH ", To: " high "}}
				return &routing.UpdateGroupInput{ReasoningEffortMappings: &replacement}
			}(),
			want: []routing.ReasoningEffortMapping{{From: "xhigh", To: "high"}},
		},
		{
			name: "model scoped mappings are canonicalized independently",
			input: func() *routing.UpdateGroupInput {
				replacement := []routing.ReasoningEffortMapping{
					{From: " MAX ", To: " low ", MatchType: "PREFIX", Model: " gpt "},
					{From: "max", To: "medium", Model: "gpt-5.4"},
				}
				return &routing.UpdateGroupInput{ReasoningEffortMappings: &replacement}
			}(),
			want: []routing.ReasoningEffortMapping{
				{From: "max", To: "low", MatchType: "prefix", Model: "gpt"},
				{From: "max", To: "medium", MatchType: "exact", Model: "gpt-5.4"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := &routing.Group{
				ID:   1,
				Name: "openai-group",

				Status:                  billing.StatusActive,
				ReasoningEffortMappings: []routing.ReasoningEffortMapping{{From: "max", To: "xhigh"}},
			}
			repo := &groupRepoStubForAdmin{getByID: existing}
			svc := newGroupAdminForTest(repo, nil, nil)

			_, err := svc.UpdateGroup(context.Background(), existing.ID, tt.input)

			require.NoError(t, err)
			require.Equal(t, tt.want, repo.updated.ReasoningEffortMappings)
		})
	}
}

func TestAdminService_UpdateGroup_RejectsInvalidReasoningEffortMappings(t *testing.T) {
	existing := &routing.Group{
		ID:   1,
		Name: "openai",

		RateMultiplier: 1,
		Status:         billing.StatusActive,
	}
	repo := &groupRepoStubForInvalidRequestFallback{groups: map[int64]*routing.Group{existing.ID: existing}}
	svc := newGroupAdminForTest(repo, nil, nil)
	invalid := []routing.ReasoningEffortMapping{
		{From: "max", To: "xhigh"},
		{From: " MAX ", To: "high"},
	}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		ReasoningEffortMappings: &invalid,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate reasoning effort mapping source")
	require.Nil(t, repo.updated)
}

func TestAdminServiceUpdateGroupPreservesReasoningPolicy(t *testing.T) {
	existing := &routing.Group{
		ID:   1,
		Name: "openai-group",

		Status:                      billing.StatusActive,
		MaxReasoningEffort:          "medium",
		MaxReasoningEffortOverLimit: routing.ReasoningEffortOverLimitDeny,
		ReasoningEffortMappings:     []routing.ReasoningEffortMapping{{From: "max", To: "xhigh"}},
	}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{})

	require.NoError(t, err)
	require.Equal(t, "medium", repo.updated.MaxReasoningEffort)
	require.Equal(t, routing.ReasoningEffortOverLimitDeny, repo.updated.MaxReasoningEffortOverLimit)
	require.Equal(t, existing.ReasoningEffortMappings, repo.updated.ReasoningEffortMappings)
}

func TestAdminServiceUpdateGroupPreservesMessagesDispatchPolicy(t *testing.T) {
	existing := &routing.Group{ID: 1, Name: "mixed", Status: billing.StatusActive, AllowedProtocols: []protocol.ProtocolID{protocol.ProtocolAnthropicMessages, protocol.ProtocolLive}, DefaultMappedModel: "gpt-test"}
	repo := &groupRepoStubForAdmin{getByID: existing}
	group, err := newGroupAdminForTest(repo, nil, nil).UpdateGroup(context.Background(), 1, &routing.UpdateGroupInput{Name: "renamed"})
	require.NoError(t, err)
	require.True(t, group.AllowMessagesDispatch)
	require.True(t, group.AllowLive)
	require.Equal(t, "gpt-test", group.DefaultMappedModel)
}

func TestAdminService_ListGroups_WithSearch(t *testing.T) {
	t.Run("search 参数正常传递到 repository 层", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{
			listWithFiltersGroups: []routing.Group{{ID: 1, Name: "alpha"}},
			listWithFiltersResult: &pagination.PaginationResult{Total: 1},
		}
		svc := newGroupAdminForTest(repo, nil, nil)

		groups, total, err := svc.ListGroups(context.Background(), 1, 20, "", "", "alpha", nil, "", "")
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		require.Equal(t, []routing.Group{{ID: 1, Name: "alpha"}}, groups)

		require.Equal(t, 1, repo.listWithFiltersCalls)
		require.Equal(t, pagination.PaginationParams{Page: 1, PageSize: 20}, repo.listWithFiltersParams)
		require.Equal(t, "alpha", repo.listWithFiltersSearch)
		require.Nil(t, repo.listWithFiltersIsExclusive)
	})

	t.Run("search 为空字符串时传递空字符串", func(t *testing.T) {
		repo := &groupRepoStubForAdmin{
			listWithFiltersGroups: []routing.Group{},
			listWithFiltersResult: &pagination.PaginationResult{Total: 0},
		}
		svc := newGroupAdminForTest(repo, nil, nil)

		groups, total, err := svc.ListGroups(context.Background(), 2, 10, "", "", "", nil, "", "")
		require.NoError(t, err)
		require.Empty(t, groups)
		require.Equal(t, int64(0), total)

		require.Equal(t, 1, repo.listWithFiltersCalls)
		require.Equal(t, pagination.PaginationParams{Page: 2, PageSize: 10}, repo.listWithFiltersParams)
		require.Equal(t, "", repo.listWithFiltersSearch)
		require.Nil(t, repo.listWithFiltersIsExclusive)
	})

	t.Run("search 与其他过滤条件组合使用", func(t *testing.T) {
		isExclusive := true
		repo := &groupRepoStubForAdmin{
			listWithFiltersGroups: []routing.Group{{ID: 2, Name: "beta"}},
			listWithFiltersResult: &pagination.PaginationResult{Total: 42},
		}
		svc := newGroupAdminForTest(repo, nil, nil)

		groups, total, err := svc.ListGroups(context.Background(), 3, 50, capability.PlatformAntigravity, billing.StatusActive, "beta", &isExclusive, "", "")
		require.NoError(t, err)
		require.Equal(t, int64(42), total)
		require.Equal(t, []routing.Group{{ID: 2, Name: "beta"}}, groups)

		require.Equal(t, 1, repo.listWithFiltersCalls)
		require.Equal(t, pagination.PaginationParams{Page: 3, PageSize: 50}, repo.listWithFiltersParams)
		require.Equal(t, capability.PlatformAntigravity, repo.listWithFiltersPlatform)
		require.Equal(t, billing.StatusActive, repo.listWithFiltersStatus)
		require.Equal(t, "beta", repo.listWithFiltersSearch)
		require.NotNil(t, repo.listWithFiltersIsExclusive)
		require.True(t, *repo.listWithFiltersIsExclusive)
	})
}

func TestAdminService_ValidateFallbackGroup_DetectsCycle(t *testing.T) {
	groupID := int64(1)
	fallbackID := int64(2)
	repo := &groupRepoStubForFallbackCycle{
		groups: map[int64]*routing.Group{
			groupID: {
				ID:              groupID,
				FallbackGroupID: &fallbackID,
			},
			fallbackID: {
				ID:              fallbackID,
				FallbackGroupID: &groupID,
			},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	err := svc.ValidateFallbackGroup(context.Background(), groupID, fallbackID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fallback group cycle")
}

type groupRepoStubForFallbackCycle struct {
	groups map[int64]*routing.Group
}

func (s *groupRepoStubForFallbackCycle) Create(_ context.Context, _ *routing.Group) error {
	panic("unexpected Create call")
}

func (s *groupRepoStubForFallbackCycle) Update(_ context.Context, _ *routing.Group) error {
	panic("unexpected Update call")
}

func (s *groupRepoStubForFallbackCycle) GetByID(ctx context.Context, id int64) (*routing.Group, error) {
	return s.GetByIDLite(ctx, id)
}

func (s *groupRepoStubForFallbackCycle) GetByIDLite(_ context.Context, id int64) (*routing.Group, error) {
	if g, ok := s.groups[id]; ok {
		return g, nil
	}
	return nil, routing.ErrGroupNotFound
}

func (s *groupRepoStubForFallbackCycle) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete call")
}

func (s *groupRepoStubForFallbackCycle) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}

func (s *groupRepoStubForFallbackCycle) List(_ context.Context, _ pagination.PaginationParams) ([]routing.Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *groupRepoStubForFallbackCycle) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _ string, _ *bool) ([]routing.Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *groupRepoStubForFallbackCycle) ListActive(_ context.Context) ([]routing.Group, error) {
	panic("unexpected ListActive call")
}

func (s *groupRepoStubForFallbackCycle) ListActiveByPlatform(_ context.Context, _ string) ([]routing.Group, error) {
	panic("unexpected ListActiveByPlatform call")
}

func (s *groupRepoStubForFallbackCycle) ListActiveByPlatformLite(_ context.Context, _ string) ([]routing.Group, error) {
	panic("unexpected ListActiveByPlatformLite call")
}

func (s *groupRepoStubForFallbackCycle) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName call")
}

func (s *groupRepoStubForFallbackCycle) GetProviderCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetProviderCount call")
}

func (s *groupRepoStubForFallbackCycle) DeleteProviderGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	panic("unexpected DeleteProviderGroupsByGroupID call")
}

func (s *groupRepoStubForFallbackCycle) BindProvidersToGroup(_ context.Context, _ int64, _ []int64) error {
	panic("unexpected BindProvidersToGroup call")
}

func (s *groupRepoStubForFallbackCycle) GetProviderIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	panic("unexpected GetProviderIDsByGroupIDs call")
}

func (s *groupRepoStubForFallbackCycle) UpdateSortOrders(_ context.Context, _ []routing.GroupSortOrderUpdate) error {
	return nil
}

type groupRepoStubForInvalidRequestFallback struct {
	groups  map[int64]*routing.Group
	created *routing.Group
	updated *routing.Group
}

func (s *groupRepoStubForInvalidRequestFallback) Create(_ context.Context, g *routing.Group) error {
	s.created = g
	return nil
}

func (s *groupRepoStubForInvalidRequestFallback) Update(_ context.Context, g *routing.Group) error {
	s.updated = g
	return nil
}

func (s *groupRepoStubForInvalidRequestFallback) GetByID(ctx context.Context, id int64) (*routing.Group, error) {
	return s.GetByIDLite(ctx, id)
}

func (s *groupRepoStubForInvalidRequestFallback) GetByIDLite(_ context.Context, id int64) (*routing.Group, error) {
	if g, ok := s.groups[id]; ok {
		return g, nil
	}
	return nil, routing.ErrGroupNotFound
}

func (s *groupRepoStubForInvalidRequestFallback) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete call")
}

func (s *groupRepoStubForInvalidRequestFallback) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}

func (s *groupRepoStubForInvalidRequestFallback) List(_ context.Context, _ pagination.PaginationParams) ([]routing.Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *groupRepoStubForInvalidRequestFallback) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]routing.Group, *pagination.PaginationResult, error) {
	if params.Page != 1 || params.PageSize != 1 || params.SortBy != "sort_order" || params.SortOrder != "desc" || platform != "" || status != "" || search != "" || isExclusive != nil {
		panic("unexpected ListWithFilters call")
	}
	var last *routing.Group
	for _, group := range s.groups {
		group := group
		if last == nil || group.SortOrder > last.SortOrder {
			last = group
		}
	}
	if last == nil {
		return nil, &pagination.PaginationResult{Page: 1, PageSize: 1}, nil
	}
	return []routing.Group{*last}, &pagination.PaginationResult{Total: int64(len(s.groups)), Page: 1, PageSize: 1}, nil
}

func (s *groupRepoStubForInvalidRequestFallback) ListActive(_ context.Context) ([]routing.Group, error) {
	panic("unexpected ListActive call")
}

func (s *groupRepoStubForInvalidRequestFallback) ListActiveByPlatform(_ context.Context, _ string) ([]routing.Group, error) {
	panic("unexpected ListActiveByPlatform call")
}

func (s *groupRepoStubForInvalidRequestFallback) ListActiveByPlatformLite(_ context.Context, _ string) ([]routing.Group, error) {
	panic("unexpected ListActiveByPlatformLite call")
}

func (s *groupRepoStubForInvalidRequestFallback) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName call")
}

func (s *groupRepoStubForInvalidRequestFallback) GetProviderCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetProviderCount call")
}

func (s *groupRepoStubForInvalidRequestFallback) DeleteProviderGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	panic("unexpected DeleteProviderGroupsByGroupID call")
}

func (s *groupRepoStubForInvalidRequestFallback) GetProviderIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	panic("unexpected GetProviderIDsByGroupIDs call")
}

func (s *groupRepoStubForInvalidRequestFallback) BindProvidersToGroup(_ context.Context, _ int64, _ []int64) error {
	panic("unexpected BindProvidersToGroup call")
}

func (s *groupRepoStubForInvalidRequestFallback) UpdateSortOrders(_ context.Context, _ []routing.GroupSortOrderUpdate) error {
	return nil
}

func TestAdminService_CreateGroup_InvalidRequestFallbackRejectsFallbackGroup(t *testing.T) {
	tests := []struct {
		name        string
		fallback    *routing.Group
		wantMessage string
	}{
		{
			name: "nested_fallback",
			fallback: &routing.Group{
				ID: 10,

				FallbackGroupIDOnInvalidRequest: func() *int64 { v := int64(99); return &v }(),
			},
			wantMessage: "fallback group cannot have invalid request fallback configured",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fallbackID := tc.fallback.ID
			repo := &groupRepoStubForInvalidRequestFallback{
				groups: map[int64]*routing.Group{
					fallbackID: tc.fallback,
				},
			}
			svc := newGroupAdminForTest(repo, nil, nil)

			_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
				Name: "g1",

				RateMultiplier:                  1.0,
				FallbackGroupIDOnInvalidRequest: &fallbackID,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMessage)
			require.Nil(t, repo.created)
		})
	}
}

func TestAdminService_CreateGroup_InvalidRequestFallbackNotFound(t *testing.T) {
	fallbackID := int64(10)
	repo := &groupRepoStubForInvalidRequestFallback{}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name: "g1",

		RateMultiplier:                  1.0,
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "fallback group not found")
	require.Nil(t, repo.created)
}

func TestAdminService_CreateGroup_InvalidRequestFallbackAllowsAnthropic(t *testing.T) {
	fallbackID := int64(10)
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			fallbackID: {ID: fallbackID},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name: "g1",

		RateMultiplier:                  1.0,
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.Equal(t, fallbackID, *repo.created.FallbackGroupIDOnInvalidRequest)
}

func TestAdminService_CreateGroup_InvalidRequestFallbackAllowsAntigravity(t *testing.T) {
	fallbackID := int64(10)
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			fallbackID: {ID: fallbackID},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name: "g1",

		RateMultiplier:                  1.0,
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.Equal(t, fallbackID, *repo.created.FallbackGroupIDOnInvalidRequest)
}

func TestAdminService_CreateGroup_InvalidRequestFallbackClearsOnZero(t *testing.T) {
	zero := int64(0)
	repo := &groupRepoStubForInvalidRequestFallback{}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name: "g1",

		RateMultiplier:                  1.0,
		FallbackGroupIDOnInvalidRequest: &zero,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.Nil(t, repo.created.FallbackGroupIDOnInvalidRequest)
}

func TestAdminService_CreateGroup_UnavailableFallbackAllowsSamePlatformActiveGroup(t *testing.T) {
	fallbackID := int64(10)
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			fallbackID: {ID: fallbackID, Status: billing.StatusActive},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
		Name: "g1",

		RateMultiplier:             1.0,
		UnavailableFallbackGroupID: &fallbackID,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.Equal(t, fallbackID, *repo.created.UnavailableFallbackGroupID)
}

func TestAdminService_CreateGroup_UnavailableFallbackRejectsInvalidGroup(t *testing.T) {
	tests := []struct {
		name        string
		fallback    *routing.Group
		wantMessage string
	}{
		{
			name:        "inactive_target",
			fallback:    &routing.Group{ID: 10, Status: billing.StatusDisabled},
			wantMessage: "unavailable fallback group must be active",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fallbackID := tc.fallback.ID
			repo := &groupRepoStubForInvalidRequestFallback{
				groups: map[int64]*routing.Group{
					fallbackID: tc.fallback,
				},
			}
			svc := newGroupAdminForTest(repo, nil, nil)

			_, err := svc.CreateGroup(context.Background(), &routing.CreateGroupInput{
				Name: "g1",

				RateMultiplier:             1.0,
				UnavailableFallbackGroupID: &fallbackID,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMessage)
			require.Nil(t, repo.created)
		})
	}
}

func TestAdminService_UpdateGroup_UnavailableFallbackRejectsSelf(t *testing.T) {
	existing := &routing.Group{
		ID:   1,
		Name: "g1",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{existing.ID: existing},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		UnavailableFallbackGroupID: &existing.ID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot set self as unavailable fallback group")
	require.Nil(t, repo.updated)
}

func TestAdminService_UpdateGroup_UnavailableFallbackClearsOnZero(t *testing.T) {
	fallbackID := int64(10)
	existing := &routing.Group{
		ID:   1,
		Name: "g1",

		Status:                     billing.StatusActive,
		UnavailableFallbackGroupID: &fallbackID,
	}
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			existing.ID: existing,
			fallbackID:  {ID: fallbackID, Status: billing.StatusActive},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	clear := int64(0)
	group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		UnavailableFallbackGroupID: &clear,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.Nil(t, repo.updated.UnavailableFallbackGroupID)
}

func TestAdminService_UpdateGroup_InvalidRequestFallbackClearsOnZero(t *testing.T) {
	fallbackID := int64(10)
	existing := &routing.Group{
		ID:   1,
		Name: "g1",

		Status:                          billing.StatusActive,
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	}
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			existing.ID: existing,
			fallbackID:  {ID: fallbackID},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	clear := int64(0)
	group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		FallbackGroupIDOnInvalidRequest: &clear,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.Nil(t, repo.updated.FallbackGroupIDOnInvalidRequest)
}

func TestAdminServiceUpdateGroupAllowsConfiguredFallback(t *testing.T) {
	fallbackID := int64(10)
	existing := &routing.Group{
		ID:   1,
		Name: "g1",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			existing.ID: existing,
			fallbackID:  {ID: fallbackID},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	})
	require.NoError(t, err)
	require.Equal(t, fallbackID, *repo.updated.FallbackGroupIDOnInvalidRequest)
}

func TestAdminService_UpdateGroup_InvalidRequestFallbackSetSuccess(t *testing.T) {
	fallbackID := int64(10)
	existing := &routing.Group{
		ID:   1,
		Name: "g1",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			existing.ID: existing,
			fallbackID:  {ID: fallbackID},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.Equal(t, fallbackID, *repo.updated.FallbackGroupIDOnInvalidRequest)
}

func TestAdminService_UpdateGroup_InvalidRequestFallbackAllowsAntigravity(t *testing.T) {
	fallbackID := int64(10)
	existing := &routing.Group{
		ID:   1,
		Name: "g1",

		Status: billing.StatusActive,
	}
	repo := &groupRepoStubForInvalidRequestFallback{
		groups: map[int64]*routing.Group{
			existing.ID: existing,
			fallbackID:  {ID: fallbackID},
		},
	}
	svc := newGroupAdminForTest(repo, nil, nil)

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &routing.UpdateGroupInput{
		FallbackGroupIDOnInvalidRequest: &fallbackID,
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.Equal(t, fallbackID, *repo.updated.FallbackGroupIDOnInvalidRequest)
}
