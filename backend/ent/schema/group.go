package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/TokenFlux/TokenRouter/ent/schema/mixins"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

// Group holds the schema definition for the Group entity.
type Group struct {
	ent.Schema
}

func (Group) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "groups"},
	}
}

func (Group) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (Group) Fields() []ent.Field {
	return []ent.Field{
		field.JSON("localization", routing.GroupLocalization{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		// 唯一约束通过部分索引实现（WHERE deleted_at IS NULL），支持软删除后重用
		// 见迁移文件 016_soft_delete_partial_unique_indexes.sql
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Float("rate_multiplier").
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).
			Default(1.0),
		field.Bool("is_exclusive").
			Default(false),
		field.String("status").
			MaxLen(20).
			Default(routing.StatusActive),
		field.String("duplicate_operation_id").
			MaxLen(64).
			Optional().
			Nillable().
			Immutable().
			Comment("内部幂等恢复标识，不对 API 暴露"),

		// scheduler_type 由分组决定基础或高级调度器，默认保持历史基础调度行为。
		field.String("scheduler_type").
			MaxLen(16).
			Default("basic").
			Comment("分组调度器类型：basic 或 advanced"),
		field.JSON("advanced_scheduler_overrides", policy.GroupAdvancedSchedulerOverrides{}).
			Default(policy.GroupAdvancedSchedulerOverrides{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("分组高级调度器稀疏覆盖；未设置字段继承网关通用设置"),
		field.String("display_brand").
			MaxLen(50).
			Default("").
			Comment("模型广场展示品牌"),

		// 图片生成权限（antigravity 和 gemini 平台使用）
		field.Bool("allow_image_generation").
			Default(false).
			Comment("是否允许该分组使用图片生成能力"),
		field.Bool("allow_batch_image_generation").
			Default(false).
			Comment("是否允许该分组使用批量图片生成能力"),

		// 搜索与工具调用按每千次显式定价，用于 Grok web_search 等。

		field.JSON("routing_policy", json.RawMessage{}).Optional().Comment("分组独立模型与功能策略"),

		// Claude Code 客户端限制 (added by migration 029)
		field.Bool("claude_code_only").
			Default(false).
			Comment("是否仅允许 Claude Code 客户端"),
		field.Int64("fallback_group_id").
			Optional().
			Nillable().
			Comment("非 Claude Code 请求降级使用的分组 ID"),
		field.Int64("fallback_group_id_on_invalid_request").
			Optional().
			Nillable().
			Comment("无效请求兜底使用的分组 ID"),
		field.Int64("unavailable_fallback_group_id").
			Optional().
			Nillable().
			Comment("当前分组不可用时优先回退使用的分组 ID"),

		// 模型路由配置 (added by migration 040)
		field.JSON("model_routing", map[string][]int64{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("模型路由配置：模型模式 -> 优先提供商ID列表"),

		// 模型路由开关 (added by migration 041)
		field.Bool("model_routing_enabled").
			Default(false).
			Comment("是否启用模型路由配置"),

		// MCP XML 协议注入开关 (added by migration 042)
		field.Bool("mcp_xml_inject").
			Default(true).
			Comment("是否注入 MCP XML 调用协议提示词（仅 antigravity 平台）"),

		// 支持的模型系列 (added by migration 046)
		field.JSON("supported_model_scopes", []string{}).
			Default([]string{"claude", "gemini_text", "gemini_image"}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("支持的模型系列：claude, gemini_text, gemini_image"),

		// 分组排序 (added by migration 052)
		field.Int("sort_order").
			Default(0).
			Comment("分组显示排序，数值越小越靠前"),

		// OpenAI Messages 调度配置 (added by migration 069)
		field.Bool("allow_messages_dispatch").
			Default(false).
			Comment("是否允许 /v1/messages 调度到此 OpenAI 分组"),
		field.JSON("allowed_protocols", []protocol.ProtocolID{}).
			Default([]protocol.ProtocolID{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("允许客户端调用分组的协议与业务入口完整集合"),
		// 协议转换与 Responses 图片策略是独立的分组控制项。
		field.JSON("protocol_fallbacks", map[protocol.ProtocolID][]protocol.ProtocolID{}).
			Default(map[protocol.ProtocolID][]protocol.ProtocolID{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("responses_image_policy").Default("inherit"),
		field.Bool("allow_live").
			Default(false).
			Comment("是否允许此 OpenAI 分组访问 Live 接口"),
		field.String("openai_fast_policy").
			Default("follow_request").
			Comment("分组加速策略：follow_request/force_priority/force_ultrafast/force_off"),
		field.Bool("force_openai_fast").
			Default(false).
			Comment("是否强制此 OpenAI 分组请求使用 service_tier=priority"),
		field.Bool("require_oauth_only").
			Default(false).
			Comment("仅允许非 apikey 类型提供商关联到此分组"),
		field.Bool("require_privacy_set").
			Default(false).
			Comment("调度时仅允许 privacy 已成功设置的提供商"),
		field.String("default_mapped_model").
			MaxLen(100).
			Default("").
			Comment("默认映射模型 ID，当提供商级映射找不到时使用此值"),
		field.JSON("models_list_config", accessview.GroupModelsListConfig{}).
			Default(accessview.GroupModelsListConfig{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("自定义 /v1/models 展示列表配置；仅影响模型列表响应，不影响调度"),
		field.JSON("availability_probe_config", accessview.GroupAvailabilityProbeConfig{}).
			Default(accessview.GroupAvailabilityProbeConfig{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("分组主动可用性探测配置"),

		// 分组级每分钟请求数上限（0 = 不限制）。设置后优先于用户级兜底生效。
		field.Int("rpm_limit").
			Default(0).
			Comment("分组 RPM 上限，0 表示不限制；设置后接管该分组用户的限流"),

		// OpenAI/Codex 请求的推理强度上限，空字符串表示不限制。
		field.String("max_reasoning_effort").
			MaxLen(20).
			Default("").
			Comment("OpenAI reasoning effort 上限；可选 minimal/low/medium/high/xhigh/max"),
		field.String("max_reasoning_effort_over_limit").
			MaxLen(20).
			Default("downgrade").
			Comment("超过推理强度上限时的访问控制：downgrade 自动降档，deny 拒绝访问"),
		field.JSON("reasoning_effort_mappings", []routing.ReasoningEffortMapping{}).
			Default([]routing.ReasoningEffortMapping{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("OpenAI reasoning effort 自定义映射；可按模型精确名、前缀或后缀限定，先映射再应用上限"),

		// 会话隔离开关：开启后拒绝其它分组已归属的显式会话切入。
		field.Bool("session_isolation_enabled").
			Default(false).
			Comment("是否开启会话隔离"),
	}
}

func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_keys", APIKey.Type),
		edge.To("api_key_composite_groups", APIKeyCompositeGroup.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("usage_logs", UsageLog.Type),
		edge.From("providers", Provider.Type).
			Ref("groups").
			Through("provider_groups", ProviderGroup.Type),
		edge.From("allowed_users", User.Type).
			Ref("allowed_groups").
			Through("user_allowed_groups", UserAllowedGroup.Type),
		edge.From("disabled_public_users", User.Type).
			Ref("disabled_public_groups").
			Through("user_disabled_public_groups", UserDisabledPublicGroup.Type),
		// 注意：fallback_group_id 直接作为字段使用，不定义 edge
		// 这样允许多个分组指向同一个降级分组（M2O 关系）
	}
}

func (Group) Indexes() []ent.Index {
	return []ent.Index{
		// name 字段已在 Fields() 中声明 Unique()，无需重复索引
		index.Fields("status"),
		index.Fields("is_exclusive"),
		index.Fields("deleted_at"),
		index.Fields("sort_order"),
		index.Fields("session_isolation_enabled"),
		index.Fields("duplicate_operation_id").
			Unique().
			StorageKey("idx_groups_duplicate_operation_id_active").
			Annotations(entsql.IndexWhere("duplicate_operation_id IS NOT NULL AND deleted_at IS NULL")),
	}
}
