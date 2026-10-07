/**
 * User Subscription API
 * API for regular users to view their own subscriptions and progress
 */

import { apiClient } from './client'
import type { UserSubscription } from '@/types'

// 撤销订阅接口返回被撤销记录、接续记录和改绑 Key 数量。
export interface RevokeSubscriptionResponse {
  revoked_subscription_id: number
  replacement_subscription_id: number | null
  rebound_api_key_count: number
}

/**
 * Get list of current user's subscriptions
 */
export async function getMySubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions')
  return response.data
}

/**
 * Get current user's active subscriptions
 */
export async function getActiveSubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions/active')
  return response.data
}

/** 撤销当前用户额度耗尽的订阅，并在有接续包时改绑显式订阅 Key。 */
export async function revokeExhaustedSubscription(
  subscriptionId: number
): Promise<RevokeSubscriptionResponse> {
  const response = await apiClient.post<RevokeSubscriptionResponse>(
    `/subscriptions/${subscriptionId}/revoke`
  )
  return response.data
}

export default {
  getMySubscriptions,
  getActiveSubscriptions,
  revokeExhaustedSubscription
}
