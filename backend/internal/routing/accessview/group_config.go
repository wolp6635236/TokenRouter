package accessview

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

// GroupConfig 保存分组配置值，供提供商等模块读取。
type GroupConfig struct {
	Localization GroupLocalization
	// DisplayName 是请求生成的展示值，持久化继续使用 Name。
	DisplayName   string
	RoutingPolicy GroupRoutingPolicy
	ID            int64
	Name          string
	Description   string
	// Models 是控制台查询时填充的可请求模型目录。
	Models         []string
	ModelProtocols map[string][]protocol.ProtocolID

	// SchedulerType 决定该分组使用基础或高级调度器。
	SchedulerType GroupSchedulerType
	// AdvancedSchedulerOverrides 仅对高级调度分组生效，未设置字段继承网关通用设置。
	AdvancedSchedulerOverrides GroupAdvancedSchedulerOverrides
	DisplayBrand               string
	RateMultiplier             float64
	IsExclusive                bool
	Status                     string
	Hydrated                   bool // 表示分组已从可信存储加载。
	// DuplicateOperationID 用于恢复已提交的分组复制结果，存于内部配置。
	DuplicateOperationID string

	// SessionIsolationEnabled 表示目标分组是否拒绝已归属其他分组的指定会话。
	SessionIsolationEnabled bool

	// 图片生成权限独立于共享价格配置。
	AllowImageGeneration      bool
	AllowBatchImageGeneration bool

	// Claude Code 客户端限制
	ClaudeCodeOnly  bool
	FallbackGroupID *int64
	// Anthropic 无效请求的回退分组。
	FallbackGroupIDOnInvalidRequest *int64
	// UnavailableFallbackGroupID 表示当前分组停用时 API Key 优先回退到的分组。
	UnavailableFallbackGroupID *int64

	// 模型路由配置
	// key: 模型匹配模式（支持 * 通配符，如 "claude-opus-*"）
	// value: 优先提供商 ID 列表
	ModelRouting        map[string][]int64
	ModelRoutingEnabled bool

	// MCP XML 协议注入开关（仅 antigravity 平台使用）
	MCPXMLInject bool

	// 支持的模型系列（仅 antigravity 平台使用）
	// 可选值: claude, gemini_text, gemini_image
	SupportedModelScopes []string

	// 分组排序
	SortOrder int

	// AllowedProtocols 是分组允许的完整客户端协议与业务入口集合，空集合表示全部关闭。
	AllowedProtocols     []protocol.ProtocolID
	ProtocolFallbacks    map[protocol.ProtocolID][]protocol.ProtocolID
	ResponsesImagePolicy string
	// AllowMessagesDispatch 是从协议集合派生并持久化的弃用兼容镜像。
	AllowMessagesDispatch bool
	AllowLive             bool
	// ForceOpenAIFast 强制 OpenAI 分组请求使用 service_tier=priority。
	ForceOpenAIFast bool
	// OpenAIFastPolicy 保存管理员选择的互斥加速策略。
	OpenAIFastPolicy string

	RequireOAuthOnly   bool // 仅允许非 apikey 类型提供商关联（OpenAI/Antigravity/Anthropic/Gemini）
	RequirePrivacySet  bool // 调度时仅允许 privacy 已成功设置的提供商（OpenAI/Antigravity/Anthropic/Gemini）
	DefaultMappedModel string
	ModelsListConfig   GroupModelsListConfig
	// AvailabilityProbeConfig 控制该分组的主动可用性探测。
	AvailabilityProbeConfig GroupAvailabilityProbeConfig

	// RPMLimit 分组级每分钟请求数上限（0 = 不限制）。
	// 一旦设置即接管该分组用户的限流（覆盖用户级 rpm_limit），可被 user-group rpm_override 进一步覆盖。
	RPMLimit int

	// MaxReasoningEffort 限制实际生效的 OpenAI/Anthropic 推理强度。
	// 空字符串表示不限制；Anthropic 不支持 minimal。
	MaxReasoningEffort string
	// MaxReasoningEffortOverLimit 控制请求指定的推理强度超过上限时降档或拒绝。
	MaxReasoningEffortOverLimit string
	// ReasoningEffortMappings 在应用上限前改写请求指定的推理强度。
	ReasoningEffortMappings []ReasoningEffortMapping

	CreatedAt                time.Time
	UpdatedAt                time.Time
	ProviderCount            int64
	ActiveProviderCount      int64
	RateLimitedProviderCount int64
}

// GroupAdvancedSchedulerOverrides 使用 policy 包定义的分组高级调度配置。
type GroupAdvancedSchedulerOverrides = policy.GroupAdvancedSchedulerOverrides

// GroupAvailabilityProbeConfig 是分组主动可用性探测配置。
// 配置挂在 groups 表上，便于每个分组独立控制探测模型、提示词和频率。
type GroupAvailabilityProbeConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
	// MaxRetries 表示首次探测失败后允许重试的最大次数；nil 使用服务端默认值。
	MaxRetries *int   `json:"max_retries,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
}

// GroupCopy 保存面向用户的展示名称和描述。
type GroupCopy struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// GroupLocalization 与分组的业务名称分别持久化。
type GroupLocalization locale.Content[GroupCopy]

// GroupModelsListConfig 控制可选的自定义 /v1/models 响应列表。
type GroupModelsListConfig struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"`
}

// ReasoningEffortMapping 按模型和匹配方式改写推理强度。
type ReasoningEffortMapping struct {
	From      string `json:"from"`
	To        string `json:"to"`
	MatchType string `json:"match_type,omitempty"`
	Model     string `json:"model,omitempty"`
}

// GroupSchedulerType 表示分组使用的提供商调度器类型。
type GroupSchedulerType string
