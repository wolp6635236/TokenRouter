/**
 * Admin Redeem Codes API endpoints
 * Handles redeem code generation and management for administrators
 */

import { apiClient } from '../client'
import type {
  RedeemCode,
  GenerateRedeemCodesRequest,
  UpdateRedeemCodeRequest,
  BatchUpdateRedeemCodeFields,
  RedeemCodeType,
  PaginatedResponse
} from '@/types'

/**
 * List all redeem codes with pagination
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 20)
 * @param filters - Optional filters
 * @returns Paginated list of redeem codes
 */
export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    type?: RedeemCodeType
    status?: 'active' | 'used' | 'expired' | 'unused' | 'disabled'
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<RedeemCode>> {
  const { data } = await apiClient.get<PaginatedResponse<RedeemCode>>('/admin/redeem-codes', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

/**
 * Generate new redeem codes
 * @param count - Number of codes to generate
 * @param type - Type of redeem code
 * @param value - Value of the code
 * @param planId - Plan ID (required for subscription type)
 * @returns Array of generated redeem codes
 */
export async function generate(
  count: number,
  type: RedeemCodeType,
  value: number,
  planId?: number | null,
  maxUses?: number,
  expiresAt?: number | null,
  code?: string,
  expiresInDays?: number | null,
  requiresPayment: boolean = false
): Promise<RedeemCode[]> {
  const payload: GenerateRedeemCodesRequest = {
    count,
    type,
    value,
    // 由后端在领取时核对付款记录。
    requires_payment: requiresPayment
  }

  if (maxUses !== undefined && maxUses >= 0) {
    payload.max_uses = maxUses
  }

  if (expiresAt && expiresAt > 0) {
    payload.expires_at = expiresAt
  }
  if (expiresInDays && expiresInDays > 0) {
    payload.expires_in_days = expiresInDays
  }

  if (code && code.trim()) {
    payload.code = code.trim()
  }

  // 订阅类型专用字段
  if (type === 'subscription') {
    payload.plan_id = planId
  }

  const { data } = await apiClient.post<RedeemCode[]>('/admin/redeem-codes/generate', payload)
  return data
}

/**
 * 更新兑换码
 * @param id - 兑换码 ID
 * @param request - 需要更新的字段
 * @returns 更新后的兑换码
 */
export async function update(id: number, request: UpdateRedeemCodeRequest): Promise<RedeemCode> {
  const { data } = await apiClient.put<RedeemCode>(`/admin/redeem-codes/${id}`, request)
  return data
}

/**
 * Delete redeem code
 * @param id - Redeem code ID
 * @returns Success confirmation
 */
export async function deleteCode(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/redeem-codes/${id}`)
  return data
}

/**
 * Batch delete redeem codes
 * @param ids - Array of redeem code IDs
 * @returns Success confirmation
 */
export async function batchDelete(ids: number[]): Promise<{
  deleted: number
  message: string
}> {
  const { data } = await apiClient.post<{
    deleted: number
    message: string
  }>('/admin/redeem-codes/batch-delete', { ids })
  return data
}

/**
 * 批量修改选中兑换码的非权益字段
 * @param ids - 兑换码 ID 列表
 * @param fields - 需要更新的字段集合
 * @returns 更新数量
 */
export async function batchUpdate(
  ids: number[],
  fields: BatchUpdateRedeemCodeFields
): Promise<{
  updated: number
  message: string
}> {
  const { data } = await apiClient.post<{
    updated: number
    message: string
  }>('/admin/redeem-codes/batch-update', { ids, fields })
  return data
}

/**
 * Export redeem codes to CSV
 * @param filters - Optional filters
 * @returns CSV data as blob
 */
export async function exportCodes(filters?: {
  type?: RedeemCodeType
  status?: 'active' | 'used' | 'expired' | 'unused' | 'disabled'
  search?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}): Promise<Blob> {
  const response = await apiClient.get('/admin/redeem-codes/export', {
    params: filters,
    responseType: 'blob'
  })
  return response.data
}

export const redeemAPI = {
  list,
  generate,
  update,
  delete: deleteCode,
  batchDelete,
  batchUpdate,
  exportCodes
}

export default redeemAPI
