package apikey

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
)

// APIKeyAuthSnapshot API Key 认证缓存快照（仅包含认证所需字段）。
type APIKeyAuthSnapshot struct {
	Version  int    `json:"version"`
	APIKeyID int64  `json:"api_key_id"`
	UserID   int64  `json:"user_id"`
	TeamID   *int64 `json:"team_id,omitempty"`
	// TeamOwnerDisabled 表示团队 Owner 的独立锁定状态。
	TeamOwnerDisabled bool                               `json:"team_owner_disabled"`
	CreatedAt         time.Time                          `json:"created_at"`
	GroupID           *int64                             `json:"group_id,omitempty"`
	IsComposite       bool                               `json:"is_composite"`
	CompositeGroups   []APIKeyAuthCompositeGroupSnapshot `json:"composite_groups,omitempty"`
	Name              string                             `json:"name"`
	Status            string                             `json:"status"`
	// FastModePolicy 随鉴权快照下发，供网关热路径读取。
	FastModePolicy string `json:"fast_mode_policy"`
	// BillingMode 和指定订阅随鉴权快照下发，供请求处理时读取。
	BillingMode             string `json:"billing_mode"`
	PreferredSubscriptionID *int64 `json:"preferred_subscription_id,omitempty"`
	// ModelMapping 随鉴权快照下发，供请求处理时读取模型映射。
	ModelMapping   map[string]string        `json:"model_mapping,omitempty"`
	IPWhitelist    []string                 `json:"ip_whitelist,omitempty"`
	IPBlacklist    []string                 `json:"ip_blacklist,omitempty"`
	User           APIKeyAuthUserSnapshot   `json:"user"`
	ActorUser      *APIKeyAuthActorSnapshot `json:"actor_user,omitempty"`
	Team           *APIKeyAuthTeamSnapshot  `json:"team,omitempty"`
	TeamMembership *TeamMembership          `json:"team_membership,omitempty"`
	Group          *APIKeyAuthGroupSnapshot `json:"group,omitempty"`

	// Quota fields for API Key independent quota feature
	Quota     float64 `json:"quota"`      // Quota limit in USD (0 = unlimited)
	QuotaUsed float64 `json:"quota_used"` // Used quota amount

	// Expiration field for API Key expiration feature
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Expiration time (nil = never expires)

	// Rate limit configuration (only limits, not usage - usage read from Redis at KeyCheck time)
	// ConcurrencyLimit 和 RPMLimit 为 0 时不限制请求。
	ConcurrencyLimit int     `json:"concurrency_limit"`
	RPMLimit         int     `json:"rpm_limit"`
	RateLimit5h      float64 `json:"rate_limit_5h"`
	RateLimit1d      float64 `json:"rate_limit_1d"`
	RateLimit7d      float64 `json:"rate_limit_7d"`
	// FallbackWhenGroupUnavailable 控制停用分组请求级回退。
	FallbackWhenGroupUnavailable bool `json:"fallback_when_group_unavailable"`
}

// APIKeyAuthCompositeGroupSnapshot 缓存一个复合前缀对应的完整鉴权分组。
type APIKeyAuthCompositeGroupSnapshot struct {
	ID                   int64                    `json:"id"`
	GroupID              int64                    `json:"group_id"`
	Prefix               string                   `json:"prefix"`
	NormalizedPrefix     string                   `json:"normalized_prefix"`
	SortOrder            int                      `json:"sort_order"`
	UserGroupRPMOverride *int                     `json:"user_group_rpm_override,omitempty"`
	Group                *APIKeyAuthGroupSnapshot `json:"group,omitempty"`
}

// APIKeyAuthActorSnapshot 只缓存验证成员提供商状态所需的字段。
type APIKeyAuthActorSnapshot struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

// APIKeyAuthTeamSnapshot 缓存请求认证所需的团队运行状态。
type APIKeyAuthTeamSnapshot struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// APIKeyAuthUserSnapshot 用户快照。
type APIKeyAuthUserSnapshot struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	Role          string  `json:"role"`
	Balance       float64 `json:"balance"`
	Concurrency   int     `json:"concurrency"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`

	// Balance notification fields (required for CheckBalanceAfterDeduction)
	Email                      string             `json:"email"`
	Username                   string             `json:"username"`
	BalanceNotifyEnabled       bool               `json:"balance_notify_enabled"`
	BalanceNotifyThresholdType string             `json:"balance_notify_threshold_type"`
	BalanceNotifyThreshold     *float64           `json:"balance_notify_threshold,omitempty"`
	BalanceNotifyExtraEmails   []NotifyEmailEntry `json:"balance_notify_extra_emails,omitempty"`
	TotalRecharged             float64            `json:"total_recharged"`

	// RPMLimit 是用户级每分钟请求数上限，0 表示不限流。限流检查在没有更具体的限额时使用此值。
	RPMLimit int `json:"rpm_limit"`

	// UserGroupRPMOverride 该 API Key 对应的 (user, group) 专属 RPM 覆盖值。
	// nil = 无 override（回退到 group/user 级）；0 = 不限流；>0 = 专属上限。
	UserGroupRPMOverride *int `json:"user_group_rpm_override,omitempty"`

	// DisabledPublicGroups 记录该用户被禁止使用的公开分组 ID，用于认证热路径拦截已有 Key。
	DisabledPublicGroups []int64 `json:"disabled_public_groups,omitempty"`
}

// APIKeyAuthGroupSnapshot 分组快照。
type APIKeyAuthGroupSnapshot struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`

	SchedulerType GroupSchedulerType `json:"scheduler_type"`
	// AdvancedSchedulerOverrides 随认证快照下发，供请求调度时读取分组配置。
	AdvancedSchedulerOverrides GroupAdvancedSchedulerOverrides `json:"advanced_scheduler_overrides"`
	IsExclusive                bool                            `json:"is_exclusive"`
	Status                     string                          `json:"status"`
	RateMultiplier             float64                         `json:"rate_multiplier"`
	SessionIsolationEnabled    bool                            `json:"session_isolation_enabled"`
	AllowImageGeneration       bool                            `json:"allow_image_generation"`
	AllowBatchImageGeneration  bool                            `json:"allow_batch_image_generation"`

	RoutingPolicy routing.GroupRoutingPolicy `json:"routing_policy"`

	ClaudeCodeOnly                  bool   `json:"claude_code_only"`
	FallbackGroupID                 *int64 `json:"fallback_group_id,omitempty"`
	FallbackGroupIDOnInvalidRequest *int64 `json:"fallback_group_id_on_invalid_request,omitempty"`
	UnavailableFallbackGroupID      *int64 `json:"unavailable_fallback_group_id,omitempty"`

	// Model routing is used by gateway provider selection, so it must be part of auth cache snapshot.
	// Only anthropic groups use these fields; others may leave them empty.
	ModelRouting        map[string][]int64 `json:"model_routing,omitempty"`
	ModelRoutingEnabled bool               `json:"model_routing_enabled"`
	MCPXMLInject        bool               `json:"mcp_xml_inject"`

	// 支持的模型系列（仅 antigravity 平台使用）
	SupportedModelScopes []string `json:"supported_model_scopes,omitempty"`

	// AllowedProtocols 不使用 omitempty，确保空集合按 [] 写入快照。
	AllowedProtocols     []protocol.ProtocolID                         `json:"allowed_protocols"`
	ProtocolFallbacks    map[protocol.ProtocolID][]protocol.ProtocolID `json:"protocol_fallbacks"`
	ResponsesImagePolicy string                                        `json:"responses_image_policy"`
	AllowLive            bool                                          `json:"allow_live"`
	// ForceOpenAIFast 保留组级 OpenAI Fast 策略，供请求期无需回源即可执行。
	ForceOpenAIFast bool `json:"force_openai_fast"`
	// OpenAIFastPolicy 保存管理员选择的互斥加速策略。
	OpenAIFastPolicy string `json:"openai_fast_policy"`

	DefaultMappedModel string                `json:"default_mapped_model,omitempty"`
	ModelsListConfig   GroupModelsListConfig `json:"models_list_config,omitempty"`

	// RPMLimit 分组级每分钟请求数上限（0 = 不限制）；用于 billing_cache_service.checkRPM 级联判断。
	RPMLimit int `json:"rpm_limit"`

	// MaxReasoningEffort OpenAI/Anthropic 请求的推理强度上限，空字符串表示不限制。
	MaxReasoningEffort string `json:"max_reasoning_effort,omitempty"`
	// MaxReasoningEffortOverLimit 超过上限时的访问控制：downgrade（默认）或 deny。
	MaxReasoningEffortOverLimit string `json:"max_reasoning_effort_over_limit,omitempty"`
	// ReasoningEffortMappings 在应用上限前改写请求指定的推理强度值。
	ReasoningEffortMappings []ReasoningEffortMapping `json:"reasoning_effort_mappings"`
}

// APIKeyAuthCacheEntry 缓存条目，支持负缓存。
type APIKeyAuthCacheEntry struct {
	NotFound bool                `json:"not_found"`
	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
}

// GroupSchedulerType、GroupAdvancedSchedulerOverrides 和 GroupModelsListConfig 使用路由分组的配置类型。
type (
	GroupSchedulerType              = accessview.GroupSchedulerType
	GroupAdvancedSchedulerOverrides = accessview.GroupAdvancedSchedulerOverrides
	GroupModelsListConfig           = accessview.GroupModelsListConfig

	ReasoningEffortMapping = routing.ReasoningEffortMapping
)
