<template>
  <AppLayout>
    <div class="space-y-4">
      <div v-if="loading" class="grid gap-4 lg:grid-cols-2" role="status" :aria-label="t('common.loading')" aria-busy="true" data-loading-skeleton>
        <div v-for="card in 2" :key="card" class="card space-y-6 p-6" aria-hidden="true">
          <Skeleton width="50%" :height="24" />
          <Skeleton width="70%" :height="16" />
          <ContentSkeleton variant="detail" :rows="6" />
          <Skeleton :height="36" />
        </div>
      </div>

      <div v-else-if="planChains.length === 0" class="card p-12 text-center">
        <div
          class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
        >
          <Icon name="creditCard" size="xl" class="text-gray-400" />
        </div>
        <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('userSubscriptions.noActiveSubscriptions') }}
        </h3>
        <p class="text-gray-500 dark:text-dark-400">
          {{ t('userSubscriptions.noActiveSubscriptionsDesc') }}
        </p>
      </div>

      <div v-else class="grid gap-4 lg:grid-cols-2">
        <!-- 卡片分三段：头部放名称、状态与操作，中间一条信息栏展示周期和分组，底部展示各周期用量。 -->
        <article
          v-for="chain in planChains"
          :key="chain.plan_id"
          class="card flex flex-col overflow-hidden"
        >
          <header class="flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-end sm:justify-between sm:px-6">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h3 class="truncate text-base font-semibold text-gray-900 dark:text-dark-50">
                  {{ chain.plan?.name || `Plan #${chain.plan_id}` }}
                </h3>
                <span :class="['badge', chain.status === 'active' ? 'badge-success' : 'badge-warning']">
                  <span
                    :class="[
                      'h-1.5 w-1.5 rounded-full',
                      chain.status === 'active' ? 'bg-emerald-500' : 'bg-amber-500'
                    ]"
                  />
                  {{ t(`userSubscriptions.status.${chain.status}`) }}
                </span>
                <span v-if="chain.pending_count > 0" class="badge badge-gray">
                  <Icon name="clock" size="xs" :animate-on-hover="false" />
                  {{ t('userSubscriptions.queuedPacks', { count: chain.pending_count }) }}
                </span>
              </div>
              <p
                v-if="chain.plan?.description"
                class="mt-1 text-sm text-gray-500 dark:text-dark-400"
              >
                {{ chain.plan.description }}
              </p>
            </div>

            <div class="flex shrink-0 items-center gap-2">
              <button
                v-if="canRevokeChain(chain)"
                type="button"
                data-testid="revoke-subscription"
                class="btn btn-danger btn-sm flex-1 sm:flex-none"
                :disabled="revoking"
                @click="openRevokeDialog(chain)"
              >
                <Icon name="ban" size="sm" />
                {{ t('userSubscriptions.revoke') }}
              </button>
              <button
                type="button"
                class="btn btn-primary btn-sm flex-1 sm:flex-none"
                @click="router.push({ path: '/purchase', query: { tab: 'subscription', plan: String(chain.plan_id) } })"
              >
                <Icon name="refresh" size="sm" />
                {{ t('payment.renewNow') }}
              </button>
            </div>
          </header>

          <!-- 窄屏两列时分组单独占满第二行，sm 起三项并排，分隔线随列数切换。 -->
          <dl class="grid grid-cols-2 border-y border-gray-100 dark:border-dark-700 sm:grid-cols-3">
            <div class="border-r border-gray-100 px-4 py-3 dark:border-dark-700 sm:px-6">
              <dt class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('userSubscriptions.startsAt') }}
              </dt>
              <dd class="mt-1 text-sm font-medium tabular-nums text-gray-900 dark:text-dark-100">
                {{ formatDateOnly(new Date(chain.starts_at)) }}
              </dd>
            </div>
            <div class="px-4 py-3 sm:border-r sm:border-gray-100 sm:px-6 sm:dark:border-dark-700">
              <dt class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('userSubscriptions.expires') }}
              </dt>
              <dd class="mt-1 text-sm font-medium" :class="getExpirationClass(chain.expires_at)">
                {{ chain.expiration.primary }}
              </dd>
              <dd
                v-if="chain.expiration.secondary"
                class="mt-0.5 text-xs tabular-nums text-gray-500 dark:text-dark-400"
              >
                {{ chain.expiration.secondary }}
              </dd>
              <dd
                v-if="chain.pending_count > 0 && chain.active"
                class="mt-0.5 text-xs text-gray-500 dark:text-dark-400"
              >
                {{ t('userSubscriptions.currentPackEnds', { date: formatDateOnly(new Date(chain.active.expires_at)) }) }}
              </dd>
            </div>
            <div
              class="col-span-2 border-t border-gray-100 px-4 py-3 dark:border-dark-700 sm:col-span-1 sm:border-t-0 sm:px-6"
            >
              <dt class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('userSubscriptions.groupAccess') }}
              </dt>
              <dd
                v-if="chain.plan?.groups_restricted && chain.plan.applicable_groups?.length"
                class="mt-1 flex flex-wrap gap-1.5"
              >
                <span
                  v-for="group in chain.plan.applicable_groups"
                  :key="group.id"
                  class="rounded-compact bg-gray-100 px-2 py-0.5 text-xs text-gray-700 dark:bg-dark-700 dark:text-dark-200"
                >
                  {{ group.name || `#${group.id}` }}
                </span>
              </dd>
              <dd
                v-else
                class="mt-1 text-sm font-medium"
                :class="chain.plan?.groups_restricted
                  ? 'text-amber-700 dark:text-amber-300'
                  : 'text-gray-900 dark:text-dark-100'"
              >
                {{ chain.plan?.groups_restricted ? t('userSubscriptions.restrictedGroups') : t('userSubscriptions.allGroups') }}
              </dd>
            </div>
          </dl>

          <div class="flex-1 px-4 py-4 sm:px-6">
            <div v-if="chain.active" class="space-y-4">
              <div
                v-for="window in usageWindows(chain.active)"
                :key="window.key"
              >
                <div class="flex items-baseline justify-between gap-3">
                  <span class="text-sm font-medium text-gray-700 dark:text-dark-100">
                    {{ window.label }}
                  </span>
                  <span class="text-sm tabular-nums">
                    <span class="font-medium text-gray-900 dark:text-dark-50">
                      {{ formatBalanceAmount(window.used) }}
                    </span>
                    <span class="text-gray-400 dark:text-dark-500">
                      / {{ formatBalanceAmount(window.limit) }}
                    </span>
                  </span>
                </div>
                <div class="relative mt-2 h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                  <div
                    class="absolute inset-y-0 left-0 rounded-full transition-[width,background-color] duration-layout"
                    :class="getProgressBarClass(window.used, window.limit)"
                    :style="{ width: getProgressWidth(window.used, window.limit) }"
                  />
                </div>
                <div class="mt-1.5 flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-400">
                  <span>{{ window.window_start ? formatUsageWindow(chain.active, window) : '' }}</span>
                  <span class="tabular-nums">{{ getUsagePercentLabel(window.used, window.limit) }}</span>
                </div>
              </div>

              <div
                v-if="usageWindows(chain.active).length === 0"
                class="flex items-center gap-3"
              >
                <div
                  class="flex h-9 w-9 shrink-0 items-center justify-center rounded-compact bg-emerald-50 text-xl text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400"
                >
                  ∞
                </div>
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-dark-50">
                    {{ t('userSubscriptions.unlimited') }}
                  </p>
                  <p class="text-xs text-gray-500 dark:text-dark-400">
                    {{ t('userSubscriptions.unlimitedDesc') }}
                  </p>
                </div>
              </div>
            </div>

            <p
              v-else
              class="rounded-control bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:bg-amber-500/10 dark:text-amber-300"
            >
              {{ t('userSubscriptions.pendingOnly') }}
            </p>
          </div>
        </article>
      </div>
    </div>
    <ConfirmDialog
      :show="revokeTarget !== null"
      :title="t('userSubscriptions.revokeTitle')"
      :message="revokeDialogMessage"
      :confirm-text="t('userSubscriptions.revoke')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      :loading="revoking"
      @confirm="confirmRevoke"
      @cancel="closeRevokeDialog"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { getLocale } from '@/i18n'
import { useLocaleRefresh } from '@/composables/useLocaleRefresh'
import Skeleton from '@/components/common/Skeleton.vue'
import ContentSkeleton from '@/components/common/ContentSkeleton.vue'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useSubscriptionStore } from '@/stores/subscriptions'
import subscriptionsAPI, { type RevokeSubscriptionResponse } from '@/api/subscriptions'
import type { SubscriptionPlan, UserSubscription } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useBalanceDisplay } from '@/composables/useBalanceDisplay'
import { formatDateOnly, formatDateTimeToMinute } from '@/utils/format'
import {
  getExpirationDateRelation,
  getRemainingDurationParts,
  isOneTimeDailyQuota,
  highestQuotaExhausted,
  type RemainingDurationParts
} from '@/utils/subscriptionQuota'

type PlanChain = {
  plan_id: number
  plan?: SubscriptionPlan
  status: 'active' | 'pending'
  starts_at: string
  expires_at: string
  active?: UserSubscription
  pending_count: number
  expiration: { primary: string; secondary: string }
}

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const subscriptionStore = useSubscriptionStore()
const { formatBalanceAmount } = useBalanceDisplay()

const subscriptions = ref<UserSubscription[]>([])
const loading = ref(true)
const revokeTarget = ref<PlanChain | null>(null)
const revoking = ref(false)

const planChains = computed<PlanChain[]>(() => {
  const map = new Map<number, UserSubscription[]>()
  for (const subscription of subscriptions.value) {
    if (subscription.status !== 'active' && subscription.status !== 'pending') continue
    const items = map.get(subscription.plan_id)
    if (items) {
      items.push(subscription)
    } else {
      map.set(subscription.plan_id, [subscription])
    }
  }

  return [...map.entries()]
    .map(([planId, items]) => {
      const sorted = [...items].sort(
        (a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime()
      )
      const active = sorted.find((item) => item.status === 'active')
      const pending = sorted.filter((item) => item.status === 'pending')
      const last = sorted[sorted.length - 1]
      return {
        plan_id: planId,
        plan: active?.plan || sorted[0]?.plan,
        status: active ? ('active' as const) : ('pending' as const),
        starts_at: sorted[0].starts_at,
        expires_at: last.expires_at,
        active,
        pending_count: pending.length,
        expiration: getExpirationParts(last.expires_at)
      }
    })
    .sort((a, b) => new Date(a.expires_at).getTime() - new Date(b.expires_at).getTime())
})

async function loadSubscriptions() {
  try {
    loading.value = true
    subscriptions.value = await subscriptionsAPI.getMySubscriptions()
  } catch (error) {
    console.error('Failed to load subscriptions:', error)
    appStore.showError(t('userSubscriptions.failedToLoad'))
  } finally {
    loading.value = false
  }
}

function usageWindows(subscription: UserSubscription) {
  return [
    {
      key: 'daily',
      label: t('userSubscriptions.daily'),
      used: subscription.daily_usage_usd || 0,
      limit: subscription.daily_limit_usd,
      window_start: subscription.daily_window_start,
      hours: 24
    },
    {
      key: 'weekly',
      label: t('userSubscriptions.weekly'),
      used: subscription.weekly_usage_usd || 0,
      limit: subscription.weekly_limit_usd,
      window_start: subscription.weekly_window_start,
      hours: 168
    },
    {
      key: 'monthly',
      label: t('userSubscriptions.monthly'),
      used: subscription.monthly_usage_usd || 0,
      limit: subscription.monthly_limit_usd,
      window_start: subscription.monthly_window_start,
      hours: 720
    }
  ].filter((window) => window.limit != null && window.limit > 0)
}

function canRevokeChain(chain: PlanChain): boolean {
  return chain.active != null && highestQuotaExhausted(chain.active)
}

const revokeDialogMessage = computed(() => {
  const target = revokeTarget.value
  if (!target) return ''
  if (target.pending_count > 0) {
    return t('userSubscriptions.revokeConfirmWithReplacement')
  }
  return t('userSubscriptions.revokeConfirmWithoutReplacement')
})

function openRevokeDialog(chain: PlanChain) {
  if (revoking.value || !canRevokeChain(chain)) return
  revokeTarget.value = chain
}

function closeRevokeDialog() {
  if (revoking.value) return
  revokeTarget.value = null
}

async function confirmRevoke() {
  const target = revokeTarget.value
  const subscriptionID = target?.active?.id
  if (!subscriptionID || revoking.value) return

  revoking.value = true
  try {
    let result: RevokeSubscriptionResponse
    try {
      result = await subscriptionsAPI.revokeExhaustedSubscription(subscriptionID)
    } catch (error) {
      console.error('Failed to revoke subscription:', error)
      revokeTarget.value = null
      appStore.showError(t('userSubscriptions.revokeFailed'))
      await loadSubscriptions()
      return
    }

    revokeTarget.value = null
    showRevokeSuccess(result)
    await loadSubscriptions()
    await subscriptionStore.fetchActiveSubscriptions(true).catch((error) => {
      console.error('Failed to refresh active subscriptions after revoke:', error)
    })
  } finally {
    revoking.value = false
  }
}

function showRevokeSuccess(result: RevokeSubscriptionResponse) {
  if (result.replacement_subscription_id != null) {
    appStore.showSuccess(
      t('userSubscriptions.revokeSuccessWithReplacement', {
        count: result.rebound_api_key_count
      })
    )
    return
  }
  appStore.showSuccess(t('userSubscriptions.revokeSuccess'))
}

function getProgressWidth(used: number, limit: number | null): string {
  if (!limit || limit === 0) return '0%'
  return `${Math.min((used / limit) * 100, 100)}%`
}

// getUsagePercentLabel 返回取整后的用量百分比，未配置上限时不显示。
function getUsagePercentLabel(used: number, limit: number | null): string {
  if (!limit || limit === 0) return ''
  return `${Math.floor(Math.min((used / limit) * 100, 100))}%`
}

function getProgressBarClass(used: number, limit: number | null): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const percentage = (used / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

// getExpirationParts 把到期信息拆成主次两行：主行是剩余天数或相对日期，次行是具体到分钟的时间。
function getExpirationParts(expiresAt: string): { primary: string; secondary: string } {
  const now = new Date()
  const expires = new Date(expiresAt)
  const relation = getExpirationDateRelation(expires, now)
  if (relation === null) return { primary: formatDateTimeToMinute(expires), secondary: '' }
  const date = formatDateTimeToMinute(expires)
  if (relation === 'expired') return { primary: t('userSubscriptions.status.expired'), secondary: date }
  if (relation === 'today') return { primary: t('common.today'), secondary: date }
  if (relation === 'tomorrow') return { primary: t('common.tomorrow'), secondary: date }
  const days = diffLocalCalendarDays(expires, now)
  return { primary: t('userSubscriptions.daysRemaining', { days }), secondary: date }
}

function getExpirationClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  if (!Number.isFinite(expires.getTime()) || expires.getTime() <= now.getTime()) {
    return 'font-medium text-red-600 dark:text-red-400'
  }
  const days = diffLocalCalendarDays(expires, now)
  if (days <= 0) return 'font-medium text-red-600 dark:text-red-400'
  if (days <= 3) return 'text-red-600 dark:text-red-400'
  if (days <= 7) return 'text-orange-600 dark:text-orange-400'
  return 'text-gray-900 dark:text-dark-100'
}

function diffLocalCalendarDays(target: Date, base: Date): number {
  const targetDay = Date.UTC(target.getFullYear(), target.getMonth(), target.getDate())
  const baseDay = Date.UTC(base.getFullYear(), base.getMonth(), base.getDate())
  return Math.round((targetDay - baseDay) / (1000 * 60 * 60 * 24))
}

function formatDurationParts(parts: RemainingDurationParts): string {
  const values: Array<[number, string]> = parts.days > 0
    ? [[parts.days, 'day'], [parts.hours, 'hour']]
    : parts.hours > 0 ? [[parts.hours, 'hour'], [parts.minutes, 'minute']] : [[parts.minutes, 'minute']]
  return values.map(([value, unit]) => new Intl.NumberFormat(getLocale(), { style: 'unit', unit, unitDisplay: 'narrow' }).format(value)).join(' ')
}

function formatUsageWindow(
  subscription: UserSubscription,
  window: ReturnType<typeof usageWindows>[number]
): string {
  if (window.key === 'daily' && isOneTimeDailyQuota(subscription)) {
    const parts = getRemainingDurationParts(subscription.expires_at)
    return parts
      ? t('userSubscriptions.quotaEndsIn', { time: formatDurationParts(parts) })
      : t('userSubscriptions.windowNotActive')
  }
  if (isQuotaWindowEndingAtSubscriptionExpiry(subscription, window)) {
    const parts = getRemainingDurationParts(subscription.expires_at)
    return parts
      ? t('userSubscriptions.quotaEndsIn', { time: formatDurationParts(parts) })
      : t('userSubscriptions.windowNotActive')
  }
  return t('userSubscriptions.resetIn', {
    time: formatResetTime(window.window_start, window.hours)
  })
}

function isQuotaWindowEndingAtSubscriptionExpiry(
  subscription: UserSubscription,
  window: ReturnType<typeof usageWindows>[number]
): boolean {
  if (!window.window_start) return false
  const windowStart = new Date(window.window_start).getTime()
  const expiresAt = new Date(subscription.expires_at).getTime()
  if (!Number.isFinite(windowStart) || !Number.isFinite(expiresAt)) return false

  const windowMs = window.hours * 60 * 60 * 1000
  const nextWindowStart = windowStart + windowMs
  return nextWindowStart + windowMs > expiresAt
}

function formatResetTime(windowStart: string | null, windowHours: number): string {
  if (!windowStart) return t('userSubscriptions.windowNotActive')
  const start = new Date(windowStart)
  const end = new Date(start.getTime() + windowHours * 60 * 60 * 1000)
  const parts = getRemainingDurationParts(end)
  return parts ? formatDurationParts(parts) : t('userSubscriptions.windowNotActive')
}

onMounted(() => {
  loadSubscriptions()
})
useLocaleRefresh(loadSubscriptions)
</script>
