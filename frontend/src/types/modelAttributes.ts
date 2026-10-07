import type { LocalizedUpdate } from '@/i18n/content'
// 模型属性只用于展示和配置导出，缺失字段表示未知或继承。
export interface ModelAttributes {
  search_terms?: string[]
  display_name_localization?: LocalizedUpdate<string>
  display_name?: string
  context?: number
  input_limit?: number
  output_limit?: number
  input_modalities?: string[]
  output_modalities?: string[]
  reasoning?: boolean
  tool_call?: boolean
  structured_output?: boolean
  temperature?: boolean
  attachment?: boolean
  route_differences?: boolean
}

export const attributeCapabilities = ['reasoning', 'tool_call', 'structured_output', 'temperature', 'attachment'] as const
export const attributeLimits = ['context', 'input_limit', 'output_limit'] as const
export const attributeModalities = ['text', 'image', 'audio', 'video', 'pdf'] as const
