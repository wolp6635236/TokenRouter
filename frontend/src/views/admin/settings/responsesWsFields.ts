import type { ResponsesWSParameters } from '@/api/admin/settings'

export type ResponsesWSNumberKey = {
  [K in keyof ResponsesWSParameters]: ResponsesWSParameters[K] extends number ? K : never
}[keyof ResponsesWSParameters]
export type ResponsesWSBooleanKey = Exclude<keyof ResponsesWSParameters, ResponsesWSNumberKey>

// ResponsesWSNumberField 描述一个数字输入框：页面值乘以 scale 得到接口值，unit 是输入框右侧的单位，hint 表示有字段说明。
export interface ResponsesWSNumberField {
  key: ResponsesWSNumberKey
  scale: number
  min: number
  max?: number
  step: string
  unit?: 'seconds' | 'ms' | 'mib'
  hint?: boolean
}

// ResponsesWSGroup 是页面上的一个分区。hint 表示分区有说明，toggle 是分区里的开关，dependent 为 true 时数字字段在开关打开后才显示。
export interface ResponsesWSGroup {
  key: string
  advanced: boolean
  hint?: boolean
  toggle?: ResponsesWSBooleanKey
  dependent?: boolean
  fields: ResponsesWSNumberField[]
}

export const responsesWsGroups: ResponsesWSGroup[] = [
  {
    key: 'pool',
    advanced: false,
    hint: true,
    fields: [
      { key: 'max_conns_per_provider', scale: 1, min: 1, step: '1', hint: true },
      { key: 'queue_limit_per_conn', scale: 1, min: 1, step: '1', hint: true },
      { key: 'min_idle_per_provider', scale: 1, min: 0, step: '1', hint: true },
      { key: 'max_idle_per_provider', scale: 1, min: 0, step: '1', hint: true },
    ],
  },
  {
    key: 'client',
    advanced: false,
    hint: true,
    fields: [
      { key: 'max_ingress_connections_per_api_key', scale: 1, min: 0, step: '1', hint: true },
      { key: 'client_first_message_timeout_seconds', scale: 1, min: 1, step: '1', unit: 'seconds' },
      { key: 'ingress_inter_turn_idle_timeout_seconds', scale: 1, min: 0, step: '1', unit: 'seconds', hint: true },
    ],
  },
  {
    key: 'messageSize',
    advanced: false,
    fields: [
      { key: 'client_read_limit_bytes', scale: 1048576, min: 1, step: 'any', unit: 'mib' },
      { key: 'http_bridge_threshold_bytes', scale: 1048576, min: 1, step: 'any', unit: 'mib', hint: true },
    ],
  },
  {
    key: 'timeout',
    advanced: false,
    hint: true,
    fields: [
      { key: 'dial_timeout_seconds', scale: 1, min: 1, step: '1', unit: 'seconds' },
      { key: 'read_timeout_seconds', scale: 1, min: 1, step: '1', unit: 'seconds' },
      { key: 'write_timeout_seconds', scale: 1, min: 1, step: '1', unit: 'seconds' },
    ],
  },
  {
    key: 'dynamic',
    advanced: true,
    toggle: 'dynamic_max_conns_by_provider_concurrency_enabled',
    dependent: true,
    fields: [
      { key: 'oauth_max_conns_factor', scale: 1, min: 1e-06, step: 'any' },
      { key: 'apikey_max_conns_factor', scale: 1, min: 1e-06, step: 'any' },
    ],
  },
  {
    key: 'prewarm',
    advanced: true,
    fields: [
      { key: 'pool_target_utilization', scale: 1, min: 1e-06, max: 1, step: 'any', hint: true },
      { key: 'prewarm_cooldown_ms', scale: 1, min: 0, step: '1', unit: 'ms' },
    ],
  },
  {
    key: 'session',
    advanced: true,
    toggle: 'ingress_previous_response_recovery_enabled',
    fields: [
      { key: 'sticky_session_ttl_seconds', scale: 1, min: 1, step: '1', unit: 'seconds' },
      { key: 'sticky_response_id_ttl_seconds', scale: 1, min: 1, step: '1', unit: 'seconds' },
    ],
  },
]

// responsesWsKeys 列出页面管理的全部参数，恢复默认值后再次编辑时用它清空所有覆盖。
export const responsesWsKeys: (keyof ResponsesWSParameters)[] = responsesWsGroups.flatMap(group => [
  ...(group.toggle ? [group.toggle] : []),
  ...group.fields.map(field => field.key),
])

// responsesWsGridClass 按字段数量选择列数，每一行都排满。
export function responsesWsGridClass(count: number) {
  if (count >= 4) return 'grid gap-4 sm:grid-cols-2 lg:grid-cols-4'
  if (count === 3) return 'grid gap-4 sm:grid-cols-3'
  return 'grid gap-4 sm:grid-cols-2'
}
