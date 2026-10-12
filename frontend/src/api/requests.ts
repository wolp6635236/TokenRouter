import apiClient from './client'

export interface RequestDetail {
  request_id: string
  parent_request_id?: string
  started_at: string
  finished_at?: string
  method: string
  path: string
  state: string
  error_code?: string
  status_code?: number
  duration_ms?: number
  model?: string
  platform?: string
  legacy: boolean
  pending?: boolean
  aliases?: { kind: string; value: string }[]
  timings?: Record<string, number>
  attempts?: { number: number; provider_id?: number; upstream_request_id?: string; status_code?: number; duration_ms?: number; outcome?: string }[]
  usage: { id: number; model: string; input_tokens: number; output_tokens: number; actual_cost: number }[] | null
  errors: { id: number; status_code: number; phase: string }[] | null
  audit_ids?: number[]
}

// findRequests 调用管理员接口查询请求诊断记录。
export async function findRequests(requestId: string, signal?: AbortSignal) {
  const { data } = await apiClient.get<{ items: RequestDetail[]; has_more: boolean }>('/admin/requests', {
    params: { request_id: requestId },
    signal,
  })
  return data
}
