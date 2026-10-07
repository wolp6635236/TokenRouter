package httpapi

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"

	"github.com/TokenFlux/TokenRouter/internal/idempotency"
	idempotencyhttp "github.com/TokenFlux/TokenRouter/internal/idempotency/httpapi"
	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	groupdto "github.com/TokenFlux/TokenRouter/internal/routing/httpapi/dto"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/gin-gonic/gin"
)

// CreateGroupRequest represents create group request
type CreateGroupRequest struct {
	Localization               *locale.Update[routing.GroupCopy]       `json:"localization"`
	RoutingPolicy              routing.GroupRoutingPolicy              `json:"routing_policy"`
	Name                       string                                  `json:"name" binding:"required"`
	Description                string                                  `json:"description"`
	SchedulerType              string                                  `json:"scheduler_type" binding:"omitempty,oneof=basic advanced"`
	AdvancedSchedulerOverrides routing.GroupAdvancedSchedulerOverrides `json:"advanced_scheduler_overrides"`
	DisplayBrand               string                                  `json:"display_brand"`
	SortOrder                  *int                                    `json:"sort_order"`
	RateMultiplier             float64                                 `json:"rate_multiplier"`
	IsExclusive                bool                                    `json:"is_exclusive"`
	// 会话隔离开启后拒绝其它分组已归属的显式会话切入。
	SessionIsolationEnabled bool `json:"session_isolation_enabled"`

	// 图片生成权限独立于共享价格配置。
	AllowImageGeneration      bool `json:"allow_image_generation"`
	AllowBatchImageGeneration bool `json:"allow_batch_image_generation"`

	ClaudeCodeOnly                  bool   `json:"claude_code_only"`
	FallbackGroupID                 *int64 `json:"fallback_group_id"`
	FallbackGroupIDOnInvalidRequest *int64 `json:"fallback_group_id_on_invalid_request"`
	// UnavailableFallbackGroupID 当前分组停用时 API Key 优先回退到的分组。
	UnavailableFallbackGroupID *int64 `json:"unavailable_fallback_group_id"`
	// 模型路由配置（仅 anthropic 平台使用）
	ModelRouting        map[string][]int64 `json:"model_routing"`
	ModelRoutingEnabled bool               `json:"model_routing_enabled"`
	MCPXMLInject        *bool              `json:"mcp_xml_inject"`
	// 支持的模型系列（仅 antigravity 平台使用）
	SupportedModelScopes []string `json:"supported_model_scopes"`
	// 客户端文本协议完整准入集合；nil 表示创建时采用平台默认值。
	AllowedProtocols             []protocol.ProtocolID                         `json:"allowed_protocols"`
	ProtocolFallbacks            map[protocol.ProtocolID][]protocol.ProtocolID `json:"protocol_fallbacks"`
	ResponsesImagePolicy         string                                        `json:"responses_image_policy"`
	LegacyAllowedClientProtocols []protocol.ProtocolID                         `json:"allowed_client_protocols"`
	// OpenAI Messages 旧兼容开关。
	AllowMessagesDispatch bool `json:"allow_messages_dispatch"`
	AllowLive             bool `json:"allow_live"`
	// OpenAI 分组是否强制请求使用 Fast 优先级。
	ForceOpenAIFast bool `json:"force_openai_fast"`
	// 新策略优先于旧布尔输入，省略时保持兼容。
	OpenAIFastPolicy *string `json:"openai_fast_policy"`
	// OpenAI 分组的 Fast 请求是否按 Standard 价格计费。

	RequireOAuthOnly        bool                                 `json:"require_oauth_only"`
	RequirePrivacySet       bool                                 `json:"require_privacy_set"`
	DefaultMappedModel      string                               `json:"default_mapped_model"`
	ModelsListConfig        routing.GroupModelsListConfig        `json:"models_list_config"`
	AvailabilityProbeConfig routing.GroupAvailabilityProbeConfig `json:"availability_probe_config"`
	// 分组 RPM 上限（0 = 不限制）
	RPMLimit int `json:"rpm_limit"`
	// OpenAI/Codex 请求推理强度上限，空字符串表示不限制。
	MaxReasoningEffort string `json:"max_reasoning_effort"`
	// 超过上限时的访问控制：downgrade（默认）或 deny。
	MaxReasoningEffortOverLimit string `json:"max_reasoning_effort_over_limit"`
	// OpenAI/Codex 推理强度映射，可按模型精确名、前缀或后缀限定。
	ReasoningEffortMappings []routing.ReasoningEffortMapping `json:"reasoning_effort_mappings"`
	// 从指定分组复制提供商（创建后自动绑定）
	CopyProvidersFromGroupIDs []int64 `json:"copy_providers_from_group_ids"`
}

// UpdateGroupRequest represents update group request
type UpdateGroupRequest struct {
	Localization               *locale.Update[routing.GroupCopy]        `json:"localization"`
	RoutingPolicy              *routing.GroupRoutingPolicy              `json:"routing_policy"`
	Name                       string                                   `json:"name"`
	Description                *string                                  `json:"description"`
	SchedulerType              *string                                  `json:"scheduler_type" binding:"omitempty,oneof=basic advanced"`
	AdvancedSchedulerOverrides *routing.GroupAdvancedSchedulerOverrides `json:"advanced_scheduler_overrides"`
	DisplayBrand               *string                                  `json:"display_brand"`
	SortOrder                  *int                                     `json:"sort_order"`
	RateMultiplier             *float64                                 `json:"rate_multiplier"`
	IsExclusive                *bool                                    `json:"is_exclusive"`
	// nil 表示不修改会话隔离开关。
	SessionIsolationEnabled *bool  `json:"session_isolation_enabled"`
	Status                  string `json:"status" binding:"omitempty,oneof=active inactive"`

	// 图片生成权限独立于共享价格配置。
	AllowImageGeneration      *bool `json:"allow_image_generation"`
	AllowBatchImageGeneration *bool `json:"allow_batch_image_generation"`

	ClaudeCodeOnly                  *bool  `json:"claude_code_only"`
	FallbackGroupID                 *int64 `json:"fallback_group_id"`
	FallbackGroupIDOnInvalidRequest *int64 `json:"fallback_group_id_on_invalid_request"`
	// UnavailableFallbackGroupID 当前分组停用时 API Key 优先回退到的分组。
	UnavailableFallbackGroupID *int64 `json:"unavailable_fallback_group_id"`
	// 模型路由配置（仅 anthropic 平台使用）
	ModelRouting        map[string][]int64 `json:"model_routing"`
	ModelRoutingEnabled *bool              `json:"model_routing_enabled"`
	MCPXMLInject        *bool              `json:"mcp_xml_inject"`
	// 支持的模型系列（仅 antigravity 平台使用）
	SupportedModelScopes *[]string `json:"supported_model_scopes"`
	// nil 表示不修改，空数组表示显式关闭全部文本协议（所有平台均合法）。
	AllowedProtocols             *[]protocol.ProtocolID                        `json:"allowed_protocols"`
	ProtocolFallbacks            map[protocol.ProtocolID][]protocol.ProtocolID `json:"protocol_fallbacks"`
	ResponsesImagePolicy         string                                        `json:"responses_image_policy"`
	LegacyAllowedClientProtocols *[]protocol.ProtocolID                        `json:"allowed_client_protocols"`
	// OpenAI Messages 旧兼容开关。
	AllowMessagesDispatch *bool `json:"allow_messages_dispatch"`
	AllowLive             *bool `json:"allow_live"`
	// OpenAI 分组是否强制请求使用 Fast 优先级。
	ForceOpenAIFast *bool `json:"force_openai_fast"`
	// 新策略优先于旧布尔输入，省略时保持兼容。
	OpenAIFastPolicy *string `json:"openai_fast_policy"`
	// OpenAI 分组的 Fast 请求是否按 Standard 价格计费。

	RequireOAuthOnly        *bool                                 `json:"require_oauth_only"`
	RequirePrivacySet       *bool                                 `json:"require_privacy_set"`
	DefaultMappedModel      *string                               `json:"default_mapped_model"`
	ModelsListConfig        *routing.GroupModelsListConfig        `json:"models_list_config"`
	AvailabilityProbeConfig *routing.GroupAvailabilityProbeConfig `json:"availability_probe_config"`
	// 分组 RPM 上限（0 = 不限制）；nil 表示未提供不改动
	RPMLimit *int `json:"rpm_limit"`
	// OpenAI/Codex 请求推理强度上限；空字符串清除，nil 不修改。
	MaxReasoningEffort *string `json:"max_reasoning_effort"`
	// 超过上限时的访问控制；空字符串视为 downgrade，nil 不修改。
	MaxReasoningEffortOverLimit *string `json:"max_reasoning_effort_over_limit"`
	// nil 不修改，空数组清空，非空数组替换。
	ReasoningEffortMappings *[]routing.ReasoningEffortMapping `json:"reasoning_effort_mappings"`
	// 从指定分组复制提供商（同步操作：先清空当前分组的提供商绑定，再绑定源分组的提供商）
	CopyProvidersFromGroupIDs []int64 `json:"copy_providers_from_group_ids"`
}

// List handles listing all groups with pagination
// GET /api/v1/admin/groups
func (h *GroupHandler) List(c *gin.Context) {
	page, pageSize := httpx.ParsePagination(c)
	if _, supplied := c.GetQuery("platform"); supplied {
		httpx.ErrorFrom(c, infraerrors.BadRequest("REMOVED_GROUP_FIELD", "groups no longer have a platform"))
		return
	}
	platform := ""
	status := c.Query("status")
	search := c.Query("search")
	// 标准化和验证 search 参数
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}
	isExclusiveStr := c.Query("is_exclusive")
	sortBy := c.DefaultQuery("sort_by", "sort_order")
	sortOrder := c.DefaultQuery("sort_order", "asc")

	var isExclusive *bool
	if isExclusiveStr != "" {
		val := isExclusiveStr == "true"
		isExclusive = &val
	}

	groups, total, err := h.adminService.ListGroups(c.Request.Context(), page, pageSize, platform, status, search, isExclusive, sortBy, sortOrder)
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	outGroups := make([]groupdto.AdminGroup[struct{}], 0, len(groups))
	for i := range groups {
		outGroups = append(outGroups, *groupdto.AdminGroupFromRouting[struct{}](&groups[i]))
	}
	httpx.Paginated(c, outGroups, total, page, pageSize)
}

// GetAll 返回所有启用分组，不分页。
// 传入 ?include_inactive=true 时同时返回禁用分组，供管理端筛选、
// 绑定和维护已禁用分组。
// GET /api/v1/admin/groups/all
func (h *GroupHandler) GetAll(c *gin.Context) {
	if _, supplied := c.GetQuery("platform"); supplied {
		httpx.ErrorFrom(c, infraerrors.BadRequest("REMOVED_GROUP_FIELD", "groups no longer have a platform"))
		return
	}
	includeInactive := c.Query("include_inactive") == "true"

	var groups []routing.Group
	var err error

	if includeInactive {
		groups, err = h.adminService.GetAllGroupsIncludingInactive(c.Request.Context())
	} else {
		groups, err = h.adminService.GetAllGroups(c.Request.Context())
	}

	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	outGroups := make([]groupdto.AdminGroup[struct{}], 0, len(groups))
	for i := range groups {
		outGroups = append(outGroups, *groupdto.AdminGroupFromRouting[struct{}](&groups[i]))
	}
	httpx.Success(c, outGroups)
}

// GetByID handles getting a group by ID
// GET /api/v1/admin/groups/:id
func (h *GroupHandler) GetByID(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.BadRequest(c, "Invalid group ID")
		return
	}

	group, err := h.adminService.GetGroup(c.Request.Context(), groupID)
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, groupdto.AdminGroupFromRouting[struct{}](group))
}

// GetModelsListCandidates 获取自定义 /v1/models 列表可选模型 ID。
// GET /api/v1/admin/groups/:id/models-list-candidates
func (h *GroupHandler) GetModelsListCandidates(c *gin.Context) {
	if _, supplied := c.GetQuery("platform"); supplied {
		httpx.ErrorFrom(c, infraerrors.BadRequest("REMOVED_GROUP_FIELD", "groups no longer have a platform"))
		return
	}
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupID < 0 {
		httpx.BadRequest(c, "Invalid group ID")
		return
	}

	models, err := h.adminService.GetGroupModelsListCandidates(
		c.Request.Context(),
		groupID,
		"",
	)
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, gin.H{"models": models})
}

// Create handles creating a new group
// POST /api/v1/admin/groups
func (h *GroupHandler) Create(c *gin.Context) {
	var req CreateGroupRequest
	if err := httpx.BindJSONStrict(c, &req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	legacyProtocolInput := req.AllowedProtocols == nil && req.LegacyAllowedClientProtocols != nil
	if legacyProtocolInput {
		req.AllowedProtocols = req.LegacyAllowedClientProtocols
	}

	group, err := h.adminService.CreateGroup(c.Request.Context(), &routing.CreateGroupInput{
		Localization:                    req.Localization,
		Name:                            req.Name,
		Description:                     req.Description,
		SchedulerType:                   req.SchedulerType,
		AdvancedSchedulerOverrides:      req.AdvancedSchedulerOverrides,
		DisplayBrand:                    req.DisplayBrand,
		SortOrder:                       req.SortOrder,
		RateMultiplier:                  req.RateMultiplier,
		IsExclusive:                     req.IsExclusive,
		SessionIsolationEnabled:         req.SessionIsolationEnabled,
		RoutingPolicy:                   req.RoutingPolicy,
		AllowImageGeneration:            req.AllowImageGeneration,
		AllowBatchImageGeneration:       req.AllowBatchImageGeneration,
		ClaudeCodeOnly:                  req.ClaudeCodeOnly,
		FallbackGroupID:                 req.FallbackGroupID,
		FallbackGroupIDOnInvalidRequest: req.FallbackGroupIDOnInvalidRequest,
		UnavailableFallbackGroupID:      req.UnavailableFallbackGroupID,
		ModelRouting:                    req.ModelRouting,
		ModelRoutingEnabled:             req.ModelRoutingEnabled,
		MCPXMLInject:                    req.MCPXMLInject,
		SupportedModelScopes:            req.SupportedModelScopes,
		LegacyProtocolInput:             legacyProtocolInput,
		AllowedProtocols:                req.AllowedProtocols,
		ProtocolFallbacks:               req.ProtocolFallbacks,
		ResponsesImagePolicy:            req.ResponsesImagePolicy,
		AllowMessagesDispatch:           req.AllowMessagesDispatch,
		AllowLive:                       req.AllowLive,
		ForceOpenAIFast:                 req.ForceOpenAIFast,
		OpenAIFastPolicy:                req.OpenAIFastPolicy,
		RequireOAuthOnly:                req.RequireOAuthOnly,
		RequirePrivacySet:               req.RequirePrivacySet,
		DefaultMappedModel:              req.DefaultMappedModel,
		ModelsListConfig:                req.ModelsListConfig,
		AvailabilityProbeConfig:         req.AvailabilityProbeConfig,
		RPMLimit:                        req.RPMLimit,
		MaxReasoningEffort:              req.MaxReasoningEffort,
		MaxReasoningEffortOverLimit:     req.MaxReasoningEffortOverLimit,
		ReasoningEffortMappings:         req.ReasoningEffortMappings,
		CopyProvidersFromGroupIDs:       req.CopyProvidersFromGroupIDs,
	})
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, groupdto.AdminGroupFromRouting[struct{}](group))
}

// Duplicate 创建停用状态的分组副本，并保留源分组的提供商绑定。
// 接口：POST /api/v1/admin/groups/:id/duplicate。
func (h *GroupHandler) Duplicate(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupID <= 0 {
		httpx.BadRequest(c, "Invalid group ID")
		return
	}
	actorScope := idempotencyhttp.AdminActorScope(c)

	result, err := h.ExecuteAdminIdempotent(
		c,
		"admin.groups.duplicate",
		struct {
			GroupID int64 `json:"group_id"`
		}{GroupID: groupID},
		h.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context) (any, error) {
			group, execErr := h.adminService.DuplicateGroup(ctx, groupID, actorScope, c.GetHeader("Idempotency-Key"))
			if execErr != nil {
				return nil, execErr
			}
			return groupdto.AdminGroupFromRouting[struct{}](group), nil
		},
	)
	if err != nil {
		reason := infraerrors.Reason(err)
		if reason == infraerrors.Reason(idempotency.ErrIdempotencyInProgress) || reason == infraerrors.Reason(idempotency.ErrIdempotencyStoreUnavail) {
			recovered, recoverErr := h.adminService.RecoverDuplicateGroup(c.Request.Context(), groupID, actorScope, c.GetHeader("Idempotency-Key"))
			if recoverErr != nil {
				slog.Warn("group_duplicate_recovery_failed", "group_id", groupID, "actor_scope", actorScope, "reason", reason, "error", recoverErr)
			} else if recovered != nil {
				c.Header("X-Idempotency-Recovered", "true")
				httpx.Success(c, groupdto.AdminGroupFromRouting[struct{}](recovered))
				return
			}
		}
		httpx.ErrorFrom(c, err)
		return
	}

	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	httpx.Success(c, result.Data)
}

// Update handles updating a group
// PUT /api/v1/admin/groups/:id
func (h *GroupHandler) Update(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.BadRequest(c, "Invalid group ID")
		return
	}

	var req UpdateGroupRequest
	if err := httpx.BindJSONStrict(c, &req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	legacyProtocolInput := req.AllowedProtocols == nil && req.LegacyAllowedClientProtocols != nil
	if legacyProtocolInput {
		req.AllowedProtocols = req.LegacyAllowedClientProtocols
	}

	group, err := h.adminService.UpdateGroup(c.Request.Context(), groupID, &routing.UpdateGroupInput{
		Localization:                    req.Localization,
		Name:                            req.Name,
		Description:                     req.Description,
		SchedulerType:                   req.SchedulerType,
		AdvancedSchedulerOverrides:      req.AdvancedSchedulerOverrides,
		DisplayBrand:                    req.DisplayBrand,
		SortOrder:                       req.SortOrder,
		RateMultiplier:                  req.RateMultiplier,
		IsExclusive:                     req.IsExclusive,
		SessionIsolationEnabled:         req.SessionIsolationEnabled,
		Status:                          req.Status,
		RoutingPolicy:                   req.RoutingPolicy,
		AllowImageGeneration:            req.AllowImageGeneration,
		AllowBatchImageGeneration:       req.AllowBatchImageGeneration,
		ClaudeCodeOnly:                  req.ClaudeCodeOnly,
		FallbackGroupID:                 req.FallbackGroupID,
		FallbackGroupIDOnInvalidRequest: req.FallbackGroupIDOnInvalidRequest,
		UnavailableFallbackGroupID:      req.UnavailableFallbackGroupID,
		ModelRouting:                    req.ModelRouting,
		ModelRoutingEnabled:             req.ModelRoutingEnabled,
		MCPXMLInject:                    req.MCPXMLInject,
		SupportedModelScopes:            req.SupportedModelScopes,
		LegacyProtocolInput:             legacyProtocolInput,
		AllowedProtocols:                req.AllowedProtocols,
		ProtocolFallbacks:               req.ProtocolFallbacks,
		ResponsesImagePolicy:            req.ResponsesImagePolicy,
		AllowMessagesDispatch:           req.AllowMessagesDispatch,
		AllowLive:                       req.AllowLive,
		ForceOpenAIFast:                 req.ForceOpenAIFast,
		OpenAIFastPolicy:                req.OpenAIFastPolicy,
		RequireOAuthOnly:                req.RequireOAuthOnly,
		RequirePrivacySet:               req.RequirePrivacySet,
		DefaultMappedModel:              req.DefaultMappedModel,
		ModelsListConfig:                req.ModelsListConfig,
		AvailabilityProbeConfig:         req.AvailabilityProbeConfig,
		RPMLimit:                        req.RPMLimit,
		MaxReasoningEffort:              req.MaxReasoningEffort,
		MaxReasoningEffortOverLimit:     req.MaxReasoningEffortOverLimit,
		ReasoningEffortMappings:         req.ReasoningEffortMappings,
		CopyProvidersFromGroupIDs:       req.CopyProvidersFromGroupIDs,
	})
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, groupdto.AdminGroupFromRouting[struct{}](group))
}

// Delete handles deleting a group
// DELETE /api/v1/admin/groups/:id
func (h *GroupHandler) Delete(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.BadRequest(c, "Invalid group ID")
		return
	}

	err = h.adminService.DeleteGroup(c.Request.Context(), groupID)
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, gin.H{"message": "Group deleted successfully"})
}

// UpdateSortOrderRequest represents the request to update group sort orders
type UpdateSortOrderRequest struct {
	Updates []struct {
		ID        int64 `json:"id" binding:"required"`
		SortOrder int   `json:"sort_order"`
	} `json:"updates" binding:"required,min=1"`
}

// UpdateSortOrder handles updating group sort orders
// PUT /api/v1/admin/groups/sort-order
func (h *GroupHandler) UpdateSortOrder(c *gin.Context) {
	var req UpdateSortOrderRequest
	if err := httpx.BindJSONStrict(c, &req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	updates := make([]routing.GroupSortOrderUpdate, 0, len(req.Updates))
	for _, u := range req.Updates {
		updates = append(updates, routing.GroupSortOrderUpdate{
			ID:        u.ID,
			SortOrder: u.SortOrder,
		})
	}

	if err := h.adminService.UpdateGroupSortOrders(c.Request.Context(), updates); err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, gin.H{"message": "Sort order updated successfully"})
}

// GroupAdministration 提供路由管理所需的操作。
type GroupAdministration interface {
	ListGroups(context.Context, int, int, string, string, string, *bool, string, string) ([]routing.Group, int64, error)
	GetAllGroups(context.Context) ([]routing.Group, error)
	GetAllGroupsIncludingInactive(context.Context) ([]routing.Group, error)
	GetGroup(context.Context, int64) (*routing.Group, error)
	GetGroupModelsListCandidates(context.Context, int64, string) ([]string, error)
	CreateGroup(context.Context, *routing.CreateGroupInput) (*routing.Group, error)
	UpdateGroup(context.Context, int64, *routing.UpdateGroupInput) (*routing.Group, error)
	DuplicateGroup(context.Context, int64, string, string) (*routing.Group, error)
	RecoverDuplicateGroup(context.Context, int64, string, string) (*routing.Group, error)
	DeleteGroup(context.Context, int64) error
	UpdateGroupSortOrders(context.Context, []routing.GroupSortOrderUpdate) error
}

// GroupHandler 只解码输入和输出 HTTP，业务规则通过窄用例接口调用。
type GroupHandler struct {
	idempotencyhttp.Executor

	adminService GroupAdministration
	resources    GroupResources
}

func NewGroupHandler(admin GroupAdministration, resources ...GroupResources) *GroupHandler {
	handler := &GroupHandler{adminService: admin}
	if len(resources) > 0 {
		handler.resources = resources[0]
	}
	return handler
}
