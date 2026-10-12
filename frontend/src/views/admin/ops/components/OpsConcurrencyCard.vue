<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsAPI, type OpsProviderAvailabilityStatsResponse, type OpsConcurrencyStatsResponse, type OpsUserConcurrencyStatsResponse } from '@/api/admin/ops'
import Icon from '@/components/icons/Icon.vue'
import { vSegmented } from '@/directives/segmented'

interface Props {
  platformFilter?: string
  groupIdFilter?: number | null
  refreshToken: number
}

const props = withDefaults(defineProps<Props>(), {
  platformFilter: '',
  groupIdFilter: null
})

const { t } = useI18n()

const loading = ref(false)
const errorMessage = ref('')
const concurrency = ref<OpsConcurrencyStatsResponse | null>(null)
const availability = ref<OpsProviderAvailabilityStatsResponse | null>(null)
const userConcurrency = ref<OpsUserConcurrencyStatsResponse | null>(null)

type ConcurrencyDimension = 'platform' | 'group' | 'provider' | 'user'

// 首次打开时沿用筛选条件对应的明细层级，之后允许用户自由切换维度。
const displayDimension = ref<ConcurrencyDimension>(
  typeof props.groupIdFilter === 'number' && props.groupIdFilter > 0
    ? 'provider'
    : props.platformFilter
      ? 'group'
      : 'platform'
)

const dimensionOptions = computed(() => [
  { value: 'platform' as const, icon: 'globe' as const, label: t('admin.ops.concurrency.byPlatform') },
  { value: 'group' as const, icon: 'users' as const, label: t('admin.ops.concurrency.byGroup') },
  { value: 'provider' as const, icon: 'server' as const, label: t('admin.ops.concurrency.byProvider') },
  { value: 'user' as const, icon: 'user' as const, label: t('admin.ops.concurrency.byUser') }
])

const realtimeEnabled = computed(() => {
  if (displayDimension.value === 'user') {
    return userConcurrency.value?.enabled ?? true
  }
  return (concurrency.value?.enabled ?? true) && (availability.value?.enabled ?? true)
})

function safeNumber(n: unknown): number {
  return typeof n === 'number' && Number.isFinite(n) ? n : 0
}

// 平台/分组汇总行数据
interface SummaryRow {
  key: string
  name: string
  platform?: string
  // 提供商统计
  total_providers: number
  available_providers: number
  rate_limited_providers: number
  error_providers: number
  // 并发统计
  total_concurrency: number
  used_concurrency: number
  waiting_in_queue: number
  // 计算字段
  availability_percentage: number
  concurrency_percentage: number
}

// 提供商详细行数据
interface ProviderRow {
  key: string
  name: string
  platform: string
  group_name: string
  // 并发
  current_in_use: number
  max_capacity: number
  waiting_in_queue: number
  load_percentage: number
  // 状态
  is_available: boolean
  is_rate_limited: boolean
  rate_limit_remaining_sec?: number
  is_overloaded: boolean
  overload_remaining_sec?: number
  has_error: boolean
  error_message?: string
}

// 用户行数据
interface UserRow {
  key: string
  user_id: number
  user_email: string
  username: string
  current_in_use: number
  max_capacity: number
  waiting_in_queue: number
  load_percentage: number
}

// 平台维度汇总
const platformRows = computed((): SummaryRow[] => {
  const concStats = concurrency.value?.platform || {}
  const availStats = availability.value?.platform || {}

  const platforms = new Set([...Object.keys(concStats), ...Object.keys(availStats)])

  return Array.from(platforms).map(platform => {
    const conc = concStats[platform] || {}
    const avail = availStats[platform] || {}

    const totalProviders = safeNumber(avail.total_providers)
    const availableProviders = safeNumber(avail.available_count)
    const totalConcurrency = safeNumber(conc.max_capacity)
    const usedConcurrency = safeNumber(conc.current_in_use)

    return {
      key: platform,
      name: platform.toUpperCase(),
      total_providers: totalProviders,
      available_providers: availableProviders,
      rate_limited_providers: safeNumber(avail.rate_limit_count),

      error_providers: safeNumber(avail.error_count),
      total_concurrency: totalConcurrency,
      used_concurrency: usedConcurrency,
      waiting_in_queue: safeNumber(conc.waiting_in_queue),
      availability_percentage: totalProviders > 0 ? Math.round((availableProviders / totalProviders) * 100) : 0,
      concurrency_percentage: totalConcurrency > 0 ? Math.round((usedConcurrency / totalConcurrency) * 100) : 0
    }
  }).sort((a, b) => b.concurrency_percentage - a.concurrency_percentage)
})

// 分组维度汇总
const groupRows = computed((): SummaryRow[] => {
  const concStats = concurrency.value?.group || {}
  const availStats = availability.value?.group || {}

  const groupIds = new Set([...Object.keys(concStats), ...Object.keys(availStats)])

  const rows = Array.from(groupIds)
    .map(gid => {
      const conc = concStats[gid] || {}
      const avail = availStats[gid] || {}

      // 后端会按分组过滤；这里额外约束，兼容尚未升级的服务端响应。
      if (typeof props.groupIdFilter === 'number' && props.groupIdFilter > 0 && Number(gid) !== props.groupIdFilter) {
        return null
      }


      const totalProviders = safeNumber(avail.total_providers)
      const availableProviders = safeNumber(avail.available_count)
      const totalConcurrency = safeNumber(conc.max_capacity)
      const usedConcurrency = safeNumber(conc.current_in_use)

      return {
        key: gid,
        name: String(conc.group_name || avail.group_name || `Group ${gid}`),
        platform: '',
        total_providers: totalProviders,
        available_providers: availableProviders,
        rate_limited_providers: safeNumber(avail.rate_limit_count),

        error_providers: safeNumber(avail.error_count),
        total_concurrency: totalConcurrency,
        used_concurrency: usedConcurrency,
        waiting_in_queue: safeNumber(conc.waiting_in_queue),
        availability_percentage: totalProviders > 0 ? Math.round((availableProviders / totalProviders) * 100) : 0,
        concurrency_percentage: totalConcurrency > 0 ? Math.round((usedConcurrency / totalConcurrency) * 100) : 0
      }
    })
    .filter((row): row is NonNullable<typeof row> => row !== null)

  return rows.sort((a, b) => b.concurrency_percentage - a.concurrency_percentage)
})

// 提供商维度详细
const providerRows = computed((): ProviderRow[] => {
  const concStats = concurrency.value?.provider || {}
  const availStats = availability.value?.provider || {}

  const providerIds = new Set([...Object.keys(concStats), ...Object.keys(availStats)])

  const rows = Array.from(providerIds)
    .map(aid => {
      const conc = concStats[aid] || {}
      const avail = availStats[aid] || {}

      // 只显示匹配的分组
      if (typeof props.groupIdFilter === 'number' && props.groupIdFilter > 0) {
        if (conc.group_id !== props.groupIdFilter && avail.group_id !== props.groupIdFilter) {
          return null
        }
      }

      return {
        key: aid,
        name: String(conc.provider_name || avail.provider_name || `Provider ${aid}`),
        platform: String(conc.platform || avail.platform || ''),
        group_name: String(conc.group_name || avail.group_name || ''),
        current_in_use: safeNumber(conc.current_in_use),
        max_capacity: safeNumber(conc.max_capacity),
        waiting_in_queue: safeNumber(conc.waiting_in_queue),
        load_percentage: safeNumber(conc.load_percentage),
        is_available: avail.is_available || false,
        is_rate_limited: avail.is_rate_limited || false,
        rate_limit_remaining_sec: avail.rate_limit_remaining_sec,
        is_overloaded: avail.is_overloaded || false,
        overload_remaining_sec: avail.overload_remaining_sec,
        has_error: avail.has_error || false,
        error_message: avail.error_message || ''
      }
    })
    .filter((row): row is NonNullable<typeof row> => row !== null)

  return rows.sort((a, b) => {
    // 优先显示异常提供商
    if (a.has_error !== b.has_error) return a.has_error ? -1 : 1
    if (a.is_rate_limited !== b.is_rate_limited) return a.is_rate_limited ? -1 : 1
    // 然后按负载排序
    return b.load_percentage - a.load_percentage
  })
})

// 用户维度详细
const userRows = computed((): UserRow[] => {
  const userStats = userConcurrency.value?.user || {}

  return Object.keys(userStats)
    .map(uid => {
      const u = userStats[uid] || {}
      return {
        key: uid,
        user_id: safeNumber(u.user_id),
        user_email: u.user_email || `User ${uid}`,
        username: u.username || '',
        current_in_use: safeNumber(u.current_in_use),
        max_capacity: safeNumber(u.max_capacity),
        waiting_in_queue: safeNumber(u.waiting_in_queue),
        load_percentage: safeNumber(u.load_percentage)
      }
    })
    .sort((a, b) => b.current_in_use - a.current_in_use || b.load_percentage - a.load_percentage)
})

// 根据维度选择数据
const displayRows = computed(() => {
  if (displayDimension.value === 'user') return userRows.value
  if (displayDimension.value === 'provider') return providerRows.value
  if (displayDimension.value === 'group') return groupRows.value
  return platformRows.value
})

const displayTitle = computed(() => {
  if (displayDimension.value === 'user') return t('admin.ops.concurrency.byUser')
  if (displayDimension.value === 'provider') return t('admin.ops.concurrency.byProvider')
  if (displayDimension.value === 'group') return t('admin.ops.concurrency.byGroup')
  return t('admin.ops.concurrency.byPlatform')
})

async function loadData() {
  loading.value = true
  errorMessage.value = ''
  try {
    if (displayDimension.value === 'user') {
      // 用户视图模式只加载用户并发数据
      const userData = await opsAPI.getUserConcurrencyStats()
      userConcurrency.value = userData
    } else {
      // 常规模式加载提供商/平台/分组数据
      const [concData, availData] = await Promise.all([
        opsAPI.getConcurrencyStats(props.platformFilter, props.groupIdFilter),
        opsAPI.getProviderAvailabilityStats(props.platformFilter, props.groupIdFilter)
      ])
      concurrency.value = concData
      availability.value = availData
    }
  } catch (err: any) {
    console.error('[OpsConcurrencyCard] Failed to load data', err)
    errorMessage.value = err?.response?.data?.detail || t('admin.ops.concurrency.loadFailed')
  } finally {
    loading.value = false
  }
}

// 刷新节奏由父组件统一控制（OpsDashboard Header 的刷新状态/倒计时）
watch(
  () => props.refreshToken,
  () => {
    if (!realtimeEnabled.value) return
    loadData()
  }
)

// 切换维度时加载对应的数据源。
watch(
  () => displayDimension.value,
  () => {
    loadData()
  }
)

// 平台或分组变化后，常规维度需要重新获取匹配的数据。
watch(
  () => [props.platformFilter, props.groupIdFilter] as const,
  () => {
    if (displayDimension.value !== 'user') {
      loadData()
    }
  }
)

function getLoadBarClass(loadPct: number): string {
  if (loadPct >= 90) return 'bg-red-500 dark:bg-red-600'
  if (loadPct >= 70) return 'bg-orange-500 dark:bg-orange-600'
  if (loadPct >= 50) return 'bg-yellow-500 dark:bg-yellow-600'
  return 'bg-green-500 dark:bg-green-600'
}

function getLoadBarStyle(loadPct: number): string {
  return `width: ${Math.min(100, Math.max(0, loadPct))}%`
}

function getLoadTextClass(loadPct: number): string {
  if (loadPct >= 90) return 'text-red-600 dark:text-red-400'
  if (loadPct >= 70) return 'text-orange-600 dark:text-orange-400'
  if (loadPct >= 50) return 'text-yellow-600 dark:text-yellow-400'
  return 'text-green-600 dark:text-green-400'
}

function formatDuration(seconds: number): string {
  if (seconds <= 0) return '0s'
  if (seconds < 60) return `${Math.round(seconds)}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h`
}


watch(
  () => realtimeEnabled.value,
  async (enabled) => {
    if (enabled) {
      await loadData()
    }
  },
  { immediate: true }
)
</script>

<template>
  <div class="flex h-full flex-col rounded-surface bg-white p-6 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-900 dark:ring-dark-700">
    <!-- 头部 -->
    <div class="mb-3 flex shrink-0 flex-col gap-2">
      <div class="flex items-center justify-between gap-3">
        <h3 class="flex items-center gap-2 whitespace-nowrap text-sm font-bold text-gray-900 dark:text-white">
          <Icon name="bolt" size="sm" class="h-4 w-4 text-blue-500" />
          {{ t('admin.ops.concurrency.title') }}
        </h3>
        <!-- 刷新按钮 -->
        <button
          class="flex h-7 w-7 shrink-0 items-center justify-center rounded-control bg-gray-100 p-0 text-xs font-semibold text-gray-700 transition-colors hover:bg-gray-200 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-dark-950 dark:text-gray-300 dark:hover:bg-dark-800"
          :disabled="loading"
          :title="t('common.refresh')"
          data-test="concurrency-refresh"
          @click="loadData"
        >
          <Icon
            name="refresh"
            size="xs"
            :animate-on-hover="false"
            class="h-3 w-3"
            :class="{ 'animate-spin': loading }"
          />
        </button>
      </div>
      <!-- 四种统计维度独占一行，窄列下标题也不会被挤压换行。 -->
      <div
        v-segmented
        class="segmented grid grid-cols-4 items-center"
        role="group"
        :aria-label="t('admin.ops.concurrency.title')"
      >
        <button
          v-for="option in dimensionOptions"
          :key="option.value"
          type="button"
          class="segmented-item flex h-7 w-full items-center justify-center"
          :class="{ 'segmented-item-active': displayDimension === option.value }"
          :title="option.label"
          :aria-label="option.label"
          :aria-pressed="displayDimension === option.value"
          :data-test="`concurrency-dimension-${option.value}`"
          @click="displayDimension = option.value"
        >
          <Icon :name="option.icon" size="xs" :stroke-width="2" />
        </button>
      </div>
    </div>

    <!-- 错误提示 -->
    <div v-if="errorMessage" class="mb-3 shrink-0 rounded-surface bg-red-50 p-2.5 text-xs text-red-600 dark:bg-red-900/20 dark:text-red-400">
      {{ errorMessage }}
    </div>

    <!-- 禁用状态 -->
    <div
      v-if="!realtimeEnabled"
      class="flex flex-1 items-center justify-center rounded-surface border border-dashed border-gray-200 text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
    >
      {{ t('admin.ops.concurrency.disabledHint') }}
    </div>

    <!-- 数据展示区域 -->
    <div v-else class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-surface border border-gray-200 dark:border-dark-700">
      <!-- 维度标题栏 -->
      <div class="flex shrink-0 items-center justify-between border-b border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-950">
        <span class="text-xs font-bold uppercase tracking-wider text-gray-500 dark:text-gray-400">
          {{ displayTitle }}
        </span>
        <span class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.ops.concurrency.totalRows', { count: displayRows.length }) }}
        </span>
      </div>

      <!-- 空状态 -->
      <div v-if="displayRows.length === 0" class="flex flex-1 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.ops.concurrency.empty') }}
      </div>

      <!-- 用户视图 -->
      <div v-else-if="displayDimension === 'user'" class="custom-scrollbar min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
        <div v-for="row in (displayRows as UserRow[])" :key="row.key" class="rounded-control bg-gray-50 p-2.5 dark:bg-dark-950">
          <!-- 用户信息和并发 -->
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <div class="flex min-w-0 flex-1 items-center gap-1.5">
              <span class="truncate text-xs font-bold text-gray-900 dark:text-white" :title="row.username || row.user_email">
                {{ row.username || row.user_email }}
              </span>
              <span v-if="row.username" class="shrink-0 truncate text-xs text-gray-400 dark:text-gray-500" :title="row.user_email">
                {{ row.user_email }}
              </span>
            </div>
            <div class="flex shrink-0 items-center gap-2 text-xs">
              <span class="font-mono font-bold text-gray-900 dark:text-white"> {{ row.current_in_use }}/{{ row.max_capacity }} </span>
              <span :class="['font-bold', getLoadTextClass(row.load_percentage)]"> {{ Math.round(row.load_percentage) }}% </span>
            </div>
          </div>

          <!-- 进度条 -->
          <div class="h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div class="h-full rounded-full transition-[width,background-color] duration-layout" :class="getLoadBarClass(row.load_percentage)" :style="getLoadBarStyle(row.load_percentage)"></div>
          </div>

          <!-- 等待队列 -->
          <div v-if="row.waiting_in_queue > 0" class="mt-1.5 flex justify-end">
            <span class="rounded-full bg-purple-100 px-1.5 py-0.5 text-xs font-semibold text-purple-700 dark:bg-purple-900/30 dark:text-purple-400">
              {{ t('admin.ops.concurrency.queued', { count: row.waiting_in_queue }) }}
            </span>
          </div>
        </div>
      </div>

      <!-- 汇总视图（平台/分组） -->
      <div v-else-if="displayDimension === 'platform' || displayDimension === 'group'" class="custom-scrollbar min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
        <div v-for="row in (displayRows as SummaryRow[])" :key="row.key" class="rounded-control bg-gray-50 p-3 dark:bg-dark-950">
          <!-- 标题行 -->
          <div class="mb-2 flex items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <div class="truncate text-xs font-bold text-gray-900 dark:text-white" :title="row.name">
                {{ row.name }}
              </div>

            </div>
            <div class="flex shrink-0 items-center gap-2 text-xs">
              <span class="font-mono font-bold text-gray-900 dark:text-white"> {{ row.used_concurrency }}/{{ row.total_concurrency }} </span>
              <span :class="['font-bold', getLoadTextClass(row.concurrency_percentage)]"> {{ row.concurrency_percentage }}% </span>
            </div>
          </div>

          <!-- 进度条 -->
          <div class="mb-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div
              class="h-full rounded-full transition-[width,background-color] duration-layout"
              :class="getLoadBarClass(row.concurrency_percentage)"
              :style="getLoadBarStyle(row.concurrency_percentage)"
            ></div>
          </div>

          <!-- 统计信息 -->
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
            <!-- 提供商统计 -->
            <div class="flex items-center gap-1">
              <Icon name="users" size="xs" class="h-3 w-3 text-gray-400" />
              <span class="text-gray-600 dark:text-gray-300">
                <span class="font-bold text-green-600 dark:text-green-400">{{ row.available_providers }}</span
                >/{{ row.total_providers }}
              </span>
              <span class="text-gray-400 dark:text-gray-500">{{ row.availability_percentage }}%</span>
            </div>

            <!-- 限流提供商 -->
            <span
              v-if="row.rate_limited_providers > 0"
              class="rounded-full bg-amber-100 px-1.5 py-0.5 font-semibold text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
            >
              {{ t('admin.ops.concurrency.rateLimited', { count: row.rate_limited_providers }) }}
            </span>

            <!-- 异常提供商 -->
            <span
              v-if="row.error_providers > 0"
              class="rounded-full bg-red-100 px-1.5 py-0.5 font-semibold text-red-700 dark:bg-red-900/30 dark:text-red-400"
            >
              {{ t('admin.ops.concurrency.errorProviders', { count: row.error_providers }) }}
            </span>

            <!-- 等待队列 -->
            <span
              v-if="row.waiting_in_queue > 0"
              class="rounded-full bg-purple-100 px-1.5 py-0.5 font-semibold text-purple-700 dark:bg-purple-900/30 dark:text-purple-400"
            >
              {{ t('admin.ops.concurrency.queued', { count: row.waiting_in_queue }) }}
            </span>
          </div>
        </div>
      </div>

      <!-- 提供商详细视图 -->
      <div v-else class="custom-scrollbar min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
        <div v-for="row in (displayRows as ProviderRow[])" :key="row.key" class="rounded-control bg-gray-50 p-2.5 dark:bg-dark-950">
          <!-- 提供商名称和并发 -->
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <div class="min-w-0 flex-1">
              <div class="truncate text-xs font-bold text-gray-900 dark:text-white" :title="row.name">
                {{ row.name }}
              </div>
              <div class="mt-0.5 text-xs text-gray-400 dark:text-gray-500">
                {{ row.group_name }}
              </div>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <!-- 并发使用 -->
              <span class="font-mono text-xs font-bold text-gray-900 dark:text-white"> {{ row.current_in_use }}/{{ row.max_capacity }} </span>
              <!-- 状态徽章 -->
              <span
                v-if="row.is_available"
                class="inline-flex items-center gap-1 rounded-compact bg-green-100 px-1.5 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400"
              >
                <Icon name="check" size="xs" :animate-on-hover="false" class="h-3 w-3" />
                {{ t('admin.ops.providerAvailability.available') }}
              </span>
              <span
                v-else-if="row.is_rate_limited"
                class="inline-flex items-center gap-1 rounded-compact bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
              >
                <Icon name="clock" size="xs" class="h-3 w-3" />
                {{ formatDuration(row.rate_limit_remaining_sec || 0) }}
              </span>
              <span
                v-else-if="row.is_overloaded"
                class="inline-flex items-center gap-1 rounded-compact bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
              >
                <Icon name="exclamationTriangle" size="xs" class="h-3 w-3" />
                {{ formatDuration(row.overload_remaining_sec || 0) }}
              </span>
              <span
                v-else-if="row.has_error"
                class="inline-flex items-center gap-1 rounded-compact bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
              >
                <Icon name="x" size="xs" class="h-3 w-3" />
                {{ t('admin.ops.providerAvailability.providerError') }}
              </span>
              <span
                v-else
                class="inline-flex items-center gap-1 rounded-compact bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-300"
              >
                {{ t('admin.ops.providerAvailability.unavailable') }}
              </span>
            </div>
          </div>

          <!-- 进度条 -->
          <div class="h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div class="h-full rounded-full transition-[width,background-color] duration-layout" :class="getLoadBarClass(row.load_percentage)" :style="getLoadBarStyle(row.load_percentage)"></div>
          </div>

          <!-- 等待队列 -->
          <div v-if="row.waiting_in_queue > 0" class="mt-1.5 flex justify-end">
            <span class="rounded-full bg-purple-100 px-1.5 py-0.5 text-xs font-semibold text-purple-700 dark:bg-purple-900/30 dark:text-purple-400">
              {{ t('admin.ops.concurrency.queued', { count: row.waiting_in_queue }) }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.custom-scrollbar {
  scrollbar-width: thin;
  scrollbar-color: rgba(156, 163, 175, 0.3) transparent;
}

.custom-scrollbar::-webkit-scrollbar {
  width: 6px;
}

.custom-scrollbar::-webkit-scrollbar-track {
  background: transparent;
}

.custom-scrollbar::-webkit-scrollbar-thumb {
  background-color: rgba(156, 163, 175, 0.3);
  border-radius: 9999px;
}

.custom-scrollbar::-webkit-scrollbar-thumb:hover {
  background-color: rgba(156, 163, 175, 0.5);
}
</style>
