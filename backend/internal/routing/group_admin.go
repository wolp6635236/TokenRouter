package routing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"

	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	wireprotocol "github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

const GroupSortOrderStep = 10

// ListGroups 按筛选条件分页查询分组，并返回总数。
func (s *GroupAdmin) ListGroups(ctx context.Context, page, pageSize int, platform, status, search string, isExclusive *bool, sortBy, sortOrder string) ([]Group, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	groups, result, err := s.groupRepo.ListWithFilters(ctx, params, platform, status, search, isExclusive)
	if err != nil {
		return nil, 0, err
	}
	return groups, result.Total, nil
}

func (s *GroupAdmin) GetAllGroups(ctx context.Context) ([]Group, error) {
	return s.groupRepo.ListActive(ctx)
}

func (s *GroupAdmin) GetAllGroupsIncludingInactive(ctx context.Context) ([]Group, error) {
	// ListWithFilters 的空 status 表示不按状态过滤，因此会返回启用和禁用分组。
	// PageSize 10000 有意放宽；实际分组数量通常只是几十个。
	groups, _, err := s.groupRepo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10000}, "", "", "", nil)
	return groups, err
}

func (s *GroupAdmin) GetGroup(ctx context.Context, id int64) (*Group, error) {
	return s.groupRepo.GetByID(ctx, id)
}

// GetGroupModelsListCandidates 新组展示默认建议，已有组按分组策略和提供商能力解析请求模型。
// @project-doc docs/interfaces/model_catalog_and_marketplace.md#model_catalog_resolution
func (s *GroupAdmin) GetGroupModelsListCandidates(ctx context.Context, id int64, _ string) ([]string, error) {
	if id <= 0 {
		return s.options.DefaultModels(""), nil
	}
	group, err := s.groupRepo.GetByIDLite(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.providerRepo == nil {
		return []string{}, nil
	}
	providers, err := s.providerRepo.ListSchedulableByGroupID(ctx, id)
	if err != nil {
		return nil, err
	}
	resolution := s.options.ModelResolver.ResolveWithProviders(ctx, &id, "", nil, providers)
	candidates := make([]string, 0, len(resolution.Models))
	for _, model := range resolution.Models {
		candidates = append(candidates, model.ID)
	}
	sort.Strings(candidates)
	if group.CustomModelsListEnabled() {
		candidates = FilterModelsListCandidates(candidates, group.ModelsListConfig.Models)
	}
	return candidates, nil
}

// SanitizeGroupOpenAIFast 规范化功能配置，实际应用范围由执行提供商决定。
func SanitizeGroupOpenAIFast(group *Group) {
	if group == nil {
		return
	}
	group.OpenAIFastPolicy = group.EffectiveOpenAIFastPolicy()
	group.ForceOpenAIFast = group.OpenAIFastPolicy == GroupOpenAIFastPolicyForcePriority
}

func (s *GroupAdmin) CreateGroup(ctx context.Context, input *CreateGroupInput) (*Group, error) {
	if err := ValidateGroupRoutingPolicy(input.RoutingPolicy); err != nil {
		return nil, err
	}
	fastPolicy, policyErr := ResolveGroupOpenAIFastPolicyInput(input.OpenAIFastPolicy, input.ForceOpenAIFast)
	if policyErr != nil {
		return nil, policyErr
	}
	if input.RateMultiplier <= 0 {
		return nil, errors.New("rate_multiplier must be > 0")
	}

	platform := ""
	schedulerType, err := NormalizeGroupSchedulerType(input.SchedulerType)
	if err != nil {
		return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_SCHEDULER_TYPE", "%v", err)
	}
	if err := s.ValidateAdvancedOverrides(ctx, input.AdvancedSchedulerOverrides); err != nil {
		return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_ADVANCED_SCHEDULER_OVERRIDES", "%v", err)
	}
	allowedClientProtocols := input.AllowedProtocols
	if allowedClientProtocols == nil {
		allowedClientProtocols = capability.DefaultGroupClientProtocols(platform)
	}

	maxReasoningEffort, err := NormalizeMaxReasoningEffortForPlatform(PlatformOpenAI, input.MaxReasoningEffort)
	if err != nil {
		return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_MAX_REASONING_EFFORT", "%v", err)
	}
	maxReasoningEffortOverLimit, err := NormalizeMaxReasoningEffortOverLimitForPlatform(PlatformOpenAI, input.MaxReasoningEffortOverLimit)
	if err != nil {
		return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_MAX_REASONING_EFFORT_OVER_LIMIT", "%v", err)
	}
	reasoningEffortMappings, err := NormalizeReasoningEffortMappings(PlatformOpenAI, input.ReasoningEffortMappings)
	if err != nil {
		return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_REASONING_EFFORT_MAPPING", "%v", err)
	}

	// 校验降级分组
	if input.FallbackGroupID != nil {
		if err := s.ValidateFallbackGroup(ctx, 0, *input.FallbackGroupID); err != nil {
			return nil, err
		}
	}
	unavailableFallbackGroupID := input.UnavailableFallbackGroupID
	if unavailableFallbackGroupID != nil && *unavailableFallbackGroupID <= 0 {
		unavailableFallbackGroupID = nil
	}
	if unavailableFallbackGroupID != nil {
		if err := s.ValidateUnavailableFallbackGroup(ctx, 0, platform, *unavailableFallbackGroupID); err != nil {
			return nil, err
		}
	}
	fallbackOnInvalidRequest := input.FallbackGroupIDOnInvalidRequest
	if fallbackOnInvalidRequest != nil && *fallbackOnInvalidRequest <= 0 {
		fallbackOnInvalidRequest = nil
	}
	// 校验无效请求兜底分组
	if fallbackOnInvalidRequest != nil {
		if err := s.ValidateFallbackGroupOnInvalidRequest(ctx, 0, platform, *fallbackOnInvalidRequest); err != nil {
			return nil, err
		}
	}

	// MCPXMLInject：默认为 true，仅当显式传入 false 时关闭
	mcpXMLInject := true
	if input.MCPXMLInject != nil {
		mcpXMLInject = *input.MCPXMLInject
	}

	allowImageGeneration := input.AllowImageGeneration
	allowBatchImageGeneration := input.AllowBatchImageGeneration && allowImageGeneration

	// 如果指定了复制提供商的源分组，先获取提供商 ID 列表
	var providerIDsToCopy []int64
	if len(input.CopyProvidersFromGroupIDs) > 0 {
		// 去重源分组 IDs
		seen := make(map[int64]struct{})
		uniqueSourceGroupIDs := make([]int64, 0, len(input.CopyProvidersFromGroupIDs))
		for _, srcGroupID := range input.CopyProvidersFromGroupIDs {
			if _, exists := seen[srcGroupID]; !exists {
				seen[srcGroupID] = struct{}{}
				uniqueSourceGroupIDs = append(uniqueSourceGroupIDs, srcGroupID)
			}
		}

		// 校验源分组的平台是否与新分组一致
		for _, srcGroupID := range uniqueSourceGroupIDs {
			_, err := s.groupRepo.GetByIDLite(ctx, srcGroupID)
			if err != nil {
				return nil, fmt.Errorf("source group %d not found: %w", srcGroupID, err)
			}
		}

		// 获取所有源分组的提供商（去重）
		var err error
		providerIDsToCopy, err = s.groupRepo.GetProviderIDsByGroupIDs(ctx, uniqueSourceGroupIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get providers from source groups: %w", err)
		}
	}
	availabilityProbeConfig, err := NormalizeGroupAvailabilityProbeConfigForAdminWrite(input.AvailabilityProbeConfig)
	if err != nil {
		return nil, err
	}

	sortOrder := 0
	if input.SortOrder != nil {
		sortOrder = *input.SortOrder
	}
	group := &Group{
		Name:        input.Name,
		Description: input.Description,

		SchedulerType:                   schedulerType,
		AdvancedSchedulerOverrides:      policy.CloneGroupAdvancedSchedulerOverrides(input.AdvancedSchedulerOverrides),
		DisplayBrand:                    strings.TrimSpace(input.DisplayBrand),
		SortOrder:                       sortOrder,
		RateMultiplier:                  input.RateMultiplier,
		IsExclusive:                     input.IsExclusive,
		SessionIsolationEnabled:         input.SessionIsolationEnabled,
		Status:                          StatusActive,
		RoutingPolicy:                   input.RoutingPolicy.Clone(),
		AllowImageGeneration:            allowImageGeneration,
		AllowBatchImageGeneration:       allowBatchImageGeneration,
		ClaudeCodeOnly:                  input.ClaudeCodeOnly,
		FallbackGroupID:                 input.FallbackGroupID,
		FallbackGroupIDOnInvalidRequest: fallbackOnInvalidRequest,
		UnavailableFallbackGroupID:      unavailableFallbackGroupID,
		ModelRouting:                    input.ModelRouting,
		MCPXMLInject:                    mcpXMLInject,
		SupportedModelScopes:            input.SupportedModelScopes,
		AllowedProtocols:                allowedClientProtocols,
		ProtocolFallbacks:               input.ProtocolFallbacks,
		ResponsesImagePolicy:            input.ResponsesImagePolicy,
		AllowLive:                       input.AllowLive,
		ForceOpenAIFast:                 input.ForceOpenAIFast,
		OpenAIFastPolicy:                fastPolicy,
		RequireOAuthOnly:                input.RequireOAuthOnly,
		RequirePrivacySet:               input.RequirePrivacySet,
		DefaultMappedModel:              input.DefaultMappedModel,
		ModelsListConfig:                NormalizeGroupModelsListConfig(input.ModelsListConfig),
		AvailabilityProbeConfig:         availabilityProbeConfig,
		RPMLimit:                        input.RPMLimit,
		MaxReasoningEffort:              maxReasoningEffort,
		MaxReasoningEffortOverLimit:     maxReasoningEffortOverLimit,
		ReasoningEffortMappings:         reasoningEffortMappings,
	}
	SanitizeGroupMessagesDispatchFields(group)
	SanitizeGroupOpenAIFast(group)
	SanitizeGroupReasoningEffortPolicy(group)
	if group.ProtocolFallbacks == nil {
		group.ProtocolFallbacks = capability.DefaultProtocolFallbacks(platform)
	}
	if input.Localization != nil {
		next, err := locale.Prepare(locale.Original(GroupCopy{DisplayName: group.Name, Description: group.Description}), *input.Localization, ValidateGroupCopy)
		if err != nil {
			return nil, err
		}
		group.Localization = GroupLocalization(next)
	} else {
		group.Localization = GroupLocalization(GroupContent(group))
	}
	var legacy *LegacyGroupProtocolPatch
	if input.AllowedProtocols == nil || input.LegacyProtocolInput {
		legacy = &LegacyGroupProtocolPatch{Image: &group.AllowImageGeneration, Batch: &group.AllowBatchImageGeneration, Live: &group.AllowLive}
	}
	if err := NormalizeGroupProtocolPolicy(group, legacy); err != nil {
		return nil, err
	}

	// require_oauth_only: 过滤掉 apikey 类型提供商
	if group.RequireOAuthOnly && len(providerIDsToCopy) > 0 {
		providers, err := s.providerRepo.GetByIDs(ctx, providerIDsToCopy)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch providers for oauth filter: %w", err)
		}
		oauthIDs := make(map[int64]struct{}, len(providers))
		for _, acc := range providers {
			if acc.Type != capability.ProviderTypeAPIKey {
				oauthIDs[acc.ID] = struct{}{}
			}
		}
		var filtered []int64
		for _, aid := range providerIDsToCopy {
			if _, ok := oauthIDs[aid]; ok {
				filtered = append(filtered, aid)
			}
		}
		providerIDsToCopy = filtered
	}

	if err := s.options.Mutate(ctx, func(opCtx context.Context) error {
		if input.SortOrder == nil {
			resolvedSortOrder, err := s.NextGroupSortOrder(opCtx)
			if err != nil {
				return err
			}
			group.SortOrder = resolvedSortOrder
		}
		if err := s.groupRepo.Create(opCtx, group); err != nil {
			return err
		}
		// 提供商复制与分组创建放在同一事务中，避免出现部分提交。
		if len(providerIDsToCopy) > 0 {
			if err := s.groupRepo.BindProvidersToGroup(opCtx, group.ID, providerIDsToCopy); err != nil {
				return fmt.Errorf("failed to bind providers to new group: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if len(providerIDsToCopy) > 0 {
		group.ProviderCount = int64(len(providerIDsToCopy))
	}

	return group, nil
}

// NextGroupSortOrder 在创建事务内分配末尾排序值，避免并发新建产生重复位置。
func (s *GroupAdmin) NextGroupSortOrder(ctx context.Context) (int, error) {
	if s.groupSortOrderRepo != nil {
		if err := s.groupSortOrderRepo.LockGroupSortOrder(ctx); err != nil {
			return 0, fmt.Errorf("lock group sort order: %w", err)
		}
	}

	groups, _, err := s.groupRepo.ListWithFilters(
		ctx,
		pagination.PaginationParams{
			Page:      1,
			PageSize:  1,
			SortBy:    "sort_order",
			SortOrder: "desc",
		},
		"",
		"",
		"",
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("load last group sort order: %w", err)
	}
	if len(groups) == 0 {
		return 0, nil
	}

	next := groups[0].SortOrder + GroupSortOrderStep
	if next < groups[0].SortOrder {
		return 0, errors.New("group sort order overflow")
	}
	return next, nil
}

// ValidateFallbackGroup 校验降级分组的有效性
// currentGroupID: 当前分组 ID（新建时为 0）
// fallbackGroupID: 降级分组 ID
func (s *GroupAdmin) ValidateFallbackGroup(ctx context.Context, currentGroupID, fallbackGroupID int64) error {
	// 不能将自己设置为降级分组
	if currentGroupID > 0 && currentGroupID == fallbackGroupID {
		return fmt.Errorf("cannot set self as fallback group")
	}

	visited := map[int64]struct{}{}
	nextID := fallbackGroupID
	for {
		if _, seen := visited[nextID]; seen {
			return fmt.Errorf("fallback group cycle detected")
		}
		visited[nextID] = struct{}{}
		if currentGroupID > 0 && nextID == currentGroupID {
			return fmt.Errorf("fallback group cycle detected")
		}

		// 检查降级分组是否存在
		fallbackGroup, err := s.groupRepo.GetByIDLite(ctx, nextID)
		if err != nil {
			return fmt.Errorf("fallback group not found: %w", err)
		}

		// 降级分组不能启用 claude_code_only，否则会造成死循环
		if nextID == fallbackGroupID && fallbackGroup.ClaudeCodeOnly {
			return fmt.Errorf("fallback group cannot have claude_code_only enabled")
		}

		if fallbackGroup.FallbackGroupID == nil {
			return nil
		}
		nextID = *fallbackGroup.FallbackGroupID
	}
}

// ValidateFallbackGroupOnInvalidRequest 校验无效请求兜底分组的有效性。
// currentGroupID: 当前分组 ID（新建时为 0）
// platform: 当前分组的平台
// fallbackGroupID: 兜底分组 ID
func (s *GroupAdmin) ValidateFallbackGroupOnInvalidRequest(ctx context.Context, currentGroupID int64, platform string, fallbackGroupID int64) error {
	if currentGroupID > 0 && currentGroupID == fallbackGroupID {
		return fmt.Errorf("cannot set self as invalid request fallback group")
	}

	fallbackGroup, err := s.groupRepo.GetByIDLite(ctx, fallbackGroupID)
	if err != nil {
		return fmt.Errorf("fallback group not found: %w", err)
	}
	if fallbackGroup.FallbackGroupIDOnInvalidRequest != nil {
		return fmt.Errorf("fallback group cannot have invalid request fallback configured")
	}
	return nil
}

func (s *GroupAdmin) UpdateGroup(ctx context.Context, id int64, input *UpdateGroupInput) (*Group, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Localization != nil {
		current := GroupContent(group)
		next, err := locale.Prepare(current, *input.Localization, ValidateGroupCopy)
		if err != nil {
			return nil, err
		}
		group.Localization = GroupLocalization(next)
	}
	previousAllowedProtocols := group.EffectiveAllowedProtocols()

	if input.Name != "" {
		group.Name = input.Name
	}
	if input.Description != nil {
		group.Description = *input.Description
	}
	if input.SchedulerType != nil {
		schedulerType, normalizeErr := NormalizeGroupSchedulerType(*input.SchedulerType)
		if normalizeErr != nil {
			return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_SCHEDULER_TYPE", "%v", normalizeErr)
		}
		group.SchedulerType = schedulerType
	}
	if input.AdvancedSchedulerOverrides != nil {
		if validationErr := s.ValidateAdvancedOverrides(ctx, *input.AdvancedSchedulerOverrides); validationErr != nil {
			return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_ADVANCED_SCHEDULER_OVERRIDES", "%v", validationErr)
		}
		group.AdvancedSchedulerOverrides = policy.CloneGroupAdvancedSchedulerOverrides(*input.AdvancedSchedulerOverrides)
	}
	if input.AllowedProtocols != nil {
		group.AllowedProtocols = append([]wireprotocol.ProtocolID{}, *input.AllowedProtocols...)
	} else {
		// 字段缺省时保留原集合；切换平台只移除新平台不支持的协议。
		group.AllowedProtocols = previousAllowedProtocols
	}
	if input.DisplayBrand != nil {
		group.DisplayBrand = strings.TrimSpace(*input.DisplayBrand)
	}
	if input.SortOrder != nil {
		group.SortOrder = *input.SortOrder
	}
	if input.RateMultiplier != nil {
		if *input.RateMultiplier <= 0 {
			return nil, errors.New("rate_multiplier must be > 0")
		}
		group.RateMultiplier = *input.RateMultiplier
	}
	if input.IsExclusive != nil {
		group.IsExclusive = *input.IsExclusive
	}
	if input.SessionIsolationEnabled != nil {
		group.SessionIsolationEnabled = *input.SessionIsolationEnabled
	}
	if input.Status != "" {
		group.Status = input.Status
	}
	if input.RoutingPolicy != nil {
		if err := ValidateGroupRoutingPolicy(*input.RoutingPolicy); err != nil {
			return nil, err
		}
		group.RoutingPolicy = input.RoutingPolicy.Clone()
	}
	// 图片能力和批量图片策略独立于模型价卡。
	if input.AllowImageGeneration != nil {
		group.AllowImageGeneration = *input.AllowImageGeneration
	}
	if input.AllowBatchImageGeneration != nil {
		group.AllowBatchImageGeneration = *input.AllowBatchImageGeneration
	}
	if !group.AllowImageGeneration {
		group.AllowBatchImageGeneration = false
	}
	// Claude Code 客户端限制
	if input.ClaudeCodeOnly != nil {
		group.ClaudeCodeOnly = *input.ClaudeCodeOnly
	}
	if input.FallbackGroupID != nil {
		// 校验降级分组
		if *input.FallbackGroupID > 0 {
			if err := s.ValidateFallbackGroup(ctx, id, *input.FallbackGroupID); err != nil {
				return nil, err
			}
			group.FallbackGroupID = input.FallbackGroupID
		} else {
			// 传入 0 或负数表示清除降级分组
			group.FallbackGroupID = nil
		}
	}
	fallbackOnInvalidRequest := group.FallbackGroupIDOnInvalidRequest
	if input.FallbackGroupIDOnInvalidRequest != nil {
		if *input.FallbackGroupIDOnInvalidRequest > 0 {
			fallbackOnInvalidRequest = input.FallbackGroupIDOnInvalidRequest
		} else {
			fallbackOnInvalidRequest = nil
		}
	}
	if fallbackOnInvalidRequest != nil {
		if err := s.ValidateFallbackGroupOnInvalidRequest(ctx, id, "", *fallbackOnInvalidRequest); err != nil {
			return nil, err
		}
	}
	group.FallbackGroupIDOnInvalidRequest = fallbackOnInvalidRequest
	unavailableFallbackGroupID := group.UnavailableFallbackGroupID
	if input.UnavailableFallbackGroupID != nil {
		if *input.UnavailableFallbackGroupID > 0 {
			unavailableFallbackGroupID = input.UnavailableFallbackGroupID
		} else {
			unavailableFallbackGroupID = nil
		}
	}
	if unavailableFallbackGroupID != nil {
		if err := s.ValidateUnavailableFallbackGroup(ctx, id, "", *unavailableFallbackGroupID); err != nil {
			return nil, err
		}
	}
	group.UnavailableFallbackGroupID = unavailableFallbackGroupID

	// 模型路由配置
	if input.ModelRouting != nil {
		group.ModelRouting = input.ModelRouting
	}
	if input.ModelRoutingEnabled != nil {
		group.ModelRoutingEnabled = *input.ModelRoutingEnabled
	}
	if input.MCPXMLInject != nil {
		group.MCPXMLInject = *input.MCPXMLInject
	}

	// 支持的模型系列（仅 antigravity 平台使用）
	if input.SupportedModelScopes != nil {
		group.SupportedModelScopes = *input.SupportedModelScopes
	}

	// 旧开关已在协议集合归一化阶段处理，此处只保留其它 OpenAI 专用配置。
	if input.AllowLive != nil {
		group.AllowLive = *input.AllowLive
	}
	if input.OpenAIFastPolicy != nil || input.ForceOpenAIFast != nil {
		legacyForce := input.ForceOpenAIFast != nil && *input.ForceOpenAIFast
		policy, err := ResolveGroupOpenAIFastPolicyInput(input.OpenAIFastPolicy, legacyForce)
		if err != nil {
			return nil, err
		}
		group.OpenAIFastPolicy = policy
	}
	if input.RequireOAuthOnly != nil {
		group.RequireOAuthOnly = *input.RequireOAuthOnly
	}
	if input.RequirePrivacySet != nil {
		group.RequirePrivacySet = *input.RequirePrivacySet
	}
	if input.DefaultMappedModel != nil {
		group.DefaultMappedModel = *input.DefaultMappedModel
	}
	if input.ModelsListConfig != nil {
		group.ModelsListConfig = NormalizeGroupModelsListConfig(*input.ModelsListConfig)
	}
	if input.AvailabilityProbeConfig != nil {
		config, err := NormalizeGroupAvailabilityProbeConfigForAdminWrite(*input.AvailabilityProbeConfig)
		if err != nil {
			return nil, err
		}
		group.AvailabilityProbeConfig = config
	}
	if input.RPMLimit != nil {
		group.RPMLimit = *input.RPMLimit
	}
	if input.MaxReasoningEffort != nil {
		maxReasoningEffort, err := NormalizeMaxReasoningEffortForPlatform(PlatformOpenAI, *input.MaxReasoningEffort)
		if err != nil {
			return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_MAX_REASONING_EFFORT", "%v", err)
		}
		group.MaxReasoningEffort = maxReasoningEffort
	}
	if input.MaxReasoningEffortOverLimit != nil {
		maxReasoningEffortOverLimit, err := NormalizeMaxReasoningEffortOverLimitForPlatform(PlatformOpenAI, *input.MaxReasoningEffortOverLimit)
		if err != nil {
			return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_MAX_REASONING_EFFORT_OVER_LIMIT", "%v", err)
		}
		group.MaxReasoningEffortOverLimit = maxReasoningEffortOverLimit
	}
	if input.ReasoningEffortMappings != nil {
		reasoningEffortMappings, err := NormalizeReasoningEffortMappings(PlatformOpenAI, *input.ReasoningEffortMappings)
		if err != nil {
			return nil, infraerrors.Newf(infraerrors.Category(400), "INVALID_REASONING_EFFORT_MAPPING", "%v", err)
		}
		group.ReasoningEffortMappings = reasoningEffortMappings
	}
	SanitizeGroupMessagesDispatchFields(group)
	SanitizeGroupOpenAIFast(group)
	SanitizeGroupReasoningEffortPolicy(group)
	if input.LegacyProtocolInput {
		for _, protocol := range previousAllowedProtocols {
			if protocol != wireprotocol.ProtocolAnthropicMessages && protocol != wireprotocol.ProtocolOpenAIResponses && protocol != wireprotocol.ProtocolOpenAIChatCompletions && protocol != wireprotocol.ProtocolGeminiGenerateContent {
				group.AllowedProtocols = append(group.AllowedProtocols, protocol)
			}
		}
	}
	if input.ProtocolFallbacks != nil {
		group.ProtocolFallbacks = input.ProtocolFallbacks
	}
	if input.ResponsesImagePolicy != "" {
		group.ResponsesImagePolicy = input.ResponsesImagePolicy
	}
	var legacy *LegacyGroupProtocolPatch
	if input.AllowedProtocols == nil || input.LegacyProtocolInput {
		legacy = &LegacyGroupProtocolPatch{Image: input.AllowImageGeneration, Live: input.AllowLive}
		if input.AllowBatchImageGeneration != nil {
			legacy.Batch = &group.AllowBatchImageGeneration
		}
		if input.AllowedProtocols == nil {
			legacy.Messages = input.AllowMessagesDispatch
		}
	}
	if err := NormalizeGroupProtocolPolicy(group, legacy); err != nil {
		return nil, err
	}

	// 如果指定了复制提供商的源分组，同步绑定（替换当前分组的提供商）
	var providerIDsToCopy []int64
	if len(input.CopyProvidersFromGroupIDs) > 0 {
		// 去重源分组 IDs
		seen := make(map[int64]struct{})
		uniqueSourceGroupIDs := make([]int64, 0, len(input.CopyProvidersFromGroupIDs))
		for _, srcGroupID := range input.CopyProvidersFromGroupIDs {
			// 校验：源分组不能是自身
			if srcGroupID == id {
				return nil, fmt.Errorf("cannot copy providers from self")
			}
			// 去重
			if _, exists := seen[srcGroupID]; !exists {
				seen[srcGroupID] = struct{}{}
				uniqueSourceGroupIDs = append(uniqueSourceGroupIDs, srcGroupID)
			}
		}

		// 校验源分组存在后复制提供商关联
		for _, srcGroupID := range uniqueSourceGroupIDs {
			_, err := s.groupRepo.GetByIDLite(ctx, srcGroupID)
			if err != nil {
				return nil, fmt.Errorf("source group %d not found: %w", srcGroupID, err)
			}
		}

		// 获取所有源分组的提供商（去重）
		providerIDsToCopy, err = s.groupRepo.GetProviderIDsByGroupIDs(ctx, uniqueSourceGroupIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get providers from source groups: %w", err)
		}

		// require_oauth_only: 过滤掉 apikey 类型提供商
		if group.RequireOAuthOnly && len(providerIDsToCopy) > 0 {
			providers, err := s.providerRepo.GetByIDs(ctx, providerIDsToCopy)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch providers for oauth filter: %w", err)
			}
			oauthIDs := make(map[int64]struct{}, len(providers))
			for _, acc := range providers {
				if acc.Type != capability.ProviderTypeAPIKey {
					oauthIDs[acc.ID] = struct{}{}
				}
			}
			var filtered []int64
			for _, aid := range providerIDsToCopy {
				if _, ok := oauthIDs[aid]; ok {
					filtered = append(filtered, aid)
				}
			}
			providerIDsToCopy = filtered
		}
	}

	if err := s.options.Mutate(ctx, func(opCtx context.Context) error {
		if err := s.groupRepo.Update(opCtx, group); err != nil {
			return err
		}
		// 分组属性更新和提供商替换必须同事务提交，避免删绑成功一半。
		if len(input.CopyProvidersFromGroupIDs) > 0 {
			if _, err := s.groupRepo.DeleteProviderGroupsByGroupID(opCtx, id); err != nil {
				return fmt.Errorf("failed to clear existing provider bindings: %w", err)
			}
			if len(providerIDsToCopy) > 0 {
				if err := s.groupRepo.BindProvidersToGroup(opCtx, id, providerIDsToCopy); err != nil {
					return fmt.Errorf("failed to bind providers to group: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
	}
	// 共享价格配置缓存按分组及模型索引价卡；分组策略通过认证快照独立读取。
	// 仅在平台实际变化且事务提交成功后失效，避免继续按旧平台匹配。

	return group, nil
}

func (s *GroupAdmin) DeleteGroup(ctx context.Context, id int64) error {
	var groupKeys []string
	if s.authCacheInvalidator != nil {
		keys, err := s.apiKeyRepo.ListKeysByGroupID(ctx, id)
		if err == nil {
			groupKeys = keys
		}
	}

	_, err := s.groupRepo.DeleteCascade(ctx, id)
	if err != nil {
		return err
	}
	// 注意：user_group_rate_multipliers 表通过外键 ON DELETE CASCADE 自动清理
	if s.authCacheInvalidator != nil {
		for _, key := range groupKeys {
			s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key)
		}
	}

	return nil
}

func (s *GroupAdmin) UpdateGroupSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error {
	return s.groupRepo.UpdateSortOrders(ctx, updates)
}
