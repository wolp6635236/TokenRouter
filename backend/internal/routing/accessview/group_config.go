package accessview

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
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
	// Models 是控制台查询时填充的可请求目录，不持久化或用于调度授权。
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
	Hydrated                   bool // indicates the group was loaded from a trusted repository source
	// DuplicateOperationID 用于恢复已提交的分组复制结果，存于内部配置。
	DuplicateOperationID string

	// SessionIsolationEnabled 表示目标分组是否拒绝其它分组已归属的显式会话切入。
	SessionIsolationEnabled bool

	// 图片生成权限独立于共享价格配置。
	AllowImageGeneration      bool
	AllowBatchImageGeneration bool

	// Claude Code 客户端限制
	ClaudeCodeOnly  bool
	FallbackGroupID *int64
	// 无效请求兜底分组（仅 anthropic 平台使用）
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
	// MaxReasoningEffortOverLimit 控制显式推理强度超过上限时降档或拒绝。
	MaxReasoningEffortOverLimit string
	// ReasoningEffortMappings 在应用上限前改写请求中显式指定的值。
	ReasoningEffortMappings []ReasoningEffortMapping

	CreatedAt                time.Time
	UpdatedAt                time.Time
	ProviderCount            int64
	ActiveProviderCount      int64
	RateLimitedProviderCount int64
}
