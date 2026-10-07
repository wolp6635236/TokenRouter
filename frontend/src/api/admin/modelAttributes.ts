import { apiClient } from '../client'
import type { ModelAttributes } from '@/types/modelAttributes'

export interface AttributeRule {
  id?: string
  models: string[]
  attributes: ModelAttributes
}

export interface AttributeConfig {
  id?: number
  name: string
  description: string
  status: string
  group_ids: number[]
  rules: AttributeRule[]
  created_at?: string
  updated_at?: string
}

export interface DefaultAttributes {
  model: string
  provider: string
  source: string
  canonical_model_id?: string
  attributes: ModelAttributes
}

export interface AttributeCatalogResponse {
  items: DefaultAttributes[]
  total: number
  providers: string[]
  version: string
  last_updated: string
  last_error?: string
}

const base = '/admin/model-attributes'

// 默认查询和档案编辑分开，普通读取不触发目录更新。
export const modelAttributesAPI = {
  async list(params: Record<string, string | number>) {
    return (await apiClient.get<{ items: AttributeConfig[]; total: number }>(`${base}/configs`, { params })).data
  },
  async save(config: AttributeConfig) {
    return config.id
      ? (await apiClient.put<AttributeConfig>(`${base}/configs/${config.id}`, config)).data
      : (await apiClient.post<AttributeConfig>(`${base}/configs`, config)).data
  },
  async remove(id: number) { await apiClient.delete(`${base}/configs/${id}`) },
  async defaults(params: Record<string, string | number>) {
    return (await apiClient.get<AttributeCatalogResponse>(`${base}/defaults`, { params })).data
  },
  async getModelDefaultAttributes(model: string) {
    return (await apiClient.get<ModelAttributes>(`${base}/defaults/model`, { params: { model } })).data
  },
  async update() { await apiClient.post(`${base}/defaults/update`, undefined, { timeout: 120_000 }) },
}
