import type { LocalizedUpdate } from '@/i18n/content'
import type {
  GroupAdvancedSchedulerOverrides,
  GroupRoutingPolicy,
  GroupSchedulerType,
  ProtocolID,
} from '@/types'
import type { ReasoningEffortMappingRow } from '@/views/admin/groupsReasoningEffort'
import type { CodexImageToolMode } from '@/utils/codexImageToolMode'

// 表单类型描述可编辑草稿，页面初始化和提交逻辑处理 API 兼容字段。
export interface GroupSettingsDraft {
  localization?: LocalizedUpdate<{ display_name: string; description: string }>
  name: string
  description: string
  display_brand: string
  rate_multiplier: number
  status?: 'active' | 'inactive'
  is_exclusive: boolean
  scheduler_type: GroupSchedulerType
  advanced_scheduler_overrides: GroupAdvancedSchedulerOverrides
  copy_providers_from_group_ids: number[]
  require_oauth_only: boolean
  require_privacy_set: boolean
  session_isolation_enabled: boolean
  unavailable_fallback_group_id: number | null
  fallback_group_id_on_invalid_request: number | null
  availability_probe_enabled: boolean
  availability_probe_model_id: string
  availability_probe_interval_minutes: number
  availability_probe_timeout_seconds: number
  availability_probe_max_retries: number
  availability_probe_user_agent: string
  availability_probe_prompt: string
  routing_policy: GroupRoutingPolicy
  model_routing_enabled: boolean
  allowed_protocols: ProtocolID[]
  protocol_fallbacks: Partial<Record<ProtocolID, ProtocolID[]>>
  responses_image_policy: CodexImageToolMode
  claude_code_only: boolean
  fallback_group_id: number | null
  openai_fast_policy: string
  max_reasoning_effort: string
  max_reasoning_effort_over_limit: string
  reasoning_effort_mappings: ReasoningEffortMappingRow[]
}

export interface GroupSettingsOption extends Record<string, unknown> {
  value: string | number | null
  label: string
  disabled?: boolean
}

export interface GroupSettingsOptions {
  copyProviders: GroupSettingsOption[]
  unavailableFallback: GroupSettingsOption[]
  invalidRequestFallback: GroupSettingsOption[]
  clientFallback: GroupSettingsOption[]
  probeModels: GroupSettingsOption[]
}

export interface GroupRoutingProvider {
  id: number
  name: string
}

export interface GroupModelRoutingRule {
  pattern: string
  providers: GroupRoutingProvider[]
}

// 搜索状态仍由页面按稳定规则标识管理，删除规则后可取消尚未完成的请求。
export interface GroupProviderSearchState {
  keywords: Record<string, string>
  results: Record<string, GroupRoutingProvider[]>
  open: Record<string, boolean>
}
