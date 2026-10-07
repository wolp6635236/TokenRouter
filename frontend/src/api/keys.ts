/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */

import { apiClient } from './client'
import type {
  ApiKey,
  ApiKeyBillingSubscriptionOption,
  CreateApiKeyRequest,
  UpdateApiKeyRequest,
  PaginatedResponse
} from '@/types'

/**
 * List all API keys for current user
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 10)
 * @param filters - Optional filter parameters
 * @param options - Optional request options
 * @returns Paginated list of API keys
 */
export async function list(
  page: number = 1,
  pageSize: number = 10,
  filters?: {
    search?: string
    status?: string
    group_id?: number | string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
    scope?: 'personal' | 'team'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<ApiKey>> {
  const { data } = await apiClient.get<PaginatedResponse<ApiKey>>('/keys', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

/**
 * 获取当前作用域可锁定使用的有效订阅。
 */
export async function getBillingOptions(
  scope: 'personal' | 'team' = 'personal'
): Promise<ApiKeyBillingSubscriptionOption[]> {
  const { data } = await apiClient.get<ApiKeyBillingSubscriptionOption[]>('/keys/billing-options', {
    params: { scope }
  })
  return data
}

/**
 * 使用已组装好的请求体创建 API Key。
 */
export async function createWithPayload(payload: CreateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.post<ApiKey>('/keys', payload)
  return data
}

/**
 * Update API key
 * @param id - API key ID
 * @param updates - Fields to update
 * @returns Updated API key
 */
export async function update(id: number, updates: UpdateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}`, updates)
  return data
}

// 原地轮换凭据，保留 Key 的配置与用量。
export async function rotate(id: number): Promise<ApiKey> {
  const { data } = await apiClient.post<ApiKey>(`/keys/${id}/rotate`)
  return data
}

/**
 * Delete API key
 * @param id - API key ID
 * @returns Success confirmation
 */
export async function deleteKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/keys/${id}`)
  return data
}

/**
 * Toggle API key status (active/inactive)
 * @param id - API key ID
 * @param status - New status
 * @returns Updated API key
 */
export async function toggleStatus(id: number, status: 'active' | 'inactive'): Promise<ApiKey> {
  return update(id, { status })
}

export const keysAPI = {
  list,
  getBillingOptions,
  createWithPayload,
  update,
  rotate,
  delete: deleteKey,
  toggleStatus
}

export default keysAPI
