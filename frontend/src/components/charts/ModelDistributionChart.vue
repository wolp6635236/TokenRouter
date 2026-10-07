<template>
  <div class="card p-4">
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ !enableRankingView || activeView === 'model_distribution'
          ? t('admin.dashboard.modelDistribution')
          : t('admin.dashboard.spendingRankingTitle') }}
      </h3>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <div
          v-segmented
          v-if="showSourceToggle"
          class="segmented"
        >
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': source === 'requested' }"
            @click="emit('update:source', 'requested')"
          >
            {{ t('usage.requestedModel') }}
          </button>
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': source === 'upstream' }"
            @click="emit('update:source', 'upstream')"
          >
            {{ t('usage.upstreamModel') }}
          </button>
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': source === 'mapping' }"
            @click="emit('update:source', 'mapping')"
          >
            {{ t('usage.mapping') }}
          </button>
        </div>
        <div
          v-segmented
          v-if="showMetricToggle"
          class="segmented"
        >
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': metric === 'tokens' }"
            @click="emit('update:metric', 'tokens')"
          >
            {{ t('admin.dashboard.metricTokens') }}
          </button>
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': metric === 'actual_cost' }"
            @click="emit('update:metric', 'actual_cost')"
          >
            {{ t('admin.dashboard.metricActualCost') }}
          </button>
        </div>
        <div v-segmented v-if="enableRankingView" class="segmented">
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': activeView === 'model_distribution' }"
            @click="activeView = 'model_distribution'"
          >
            {{ t('admin.dashboard.viewModelDistribution') }}
          </button>
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': activeView === 'spending_ranking' }"
            @click="activeView = 'spending_ranking'"
          >
            {{ t('admin.dashboard.viewSpendingRanking') }}
          </button>
        </div>
      </div>
    </div>

    <ChartSkeleton v-if="activeView === 'model_distribution' && loading" variant="distribution" />
    <div
      v-else-if="activeView === 'model_distribution' && displayModelStats.length > 0 && chartData"
      class="flex flex-col items-center gap-4 sm:flex-row sm:items-start sm:gap-6"
    >
      <div class="h-48 w-48 shrink-0">
        <Doughnut :data="chartData" :options="doughnutOptions" />
      </div>
      <div class="max-h-48 w-full min-w-0 flex-1 overflow-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-gray-500 dark:text-gray-400">
              <th class="pb-2 text-left">{{ t('admin.dashboard.model') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.requests') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.tokens') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.actual') }}</th>
              <th v-if="showProviderCost" class="pb-2 text-right">{{ t('admin.dashboard.providerCost') }}</th>
              <th v-if="showStandardCost" class="pb-2 text-right">{{ t('admin.dashboard.standard') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="model in displayModelStats" :key="model.model">
              <tr data-icon-trigger
                class="group/breakdown border-t border-gray-100 transition-colors dark:border-dark-600"
                :class="enableBreakdown ? 'cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-700/40' : ''"
                @click="enableBreakdown && toggleBreakdown('model', model.model)"
              >
                <!-- 模型名称随可展开行的悬停着色，箭头继承文字颜色。 -->
                <td
                  class="max-w-[100px] truncate py-1.5 font-medium text-gray-900 dark:text-white"
                  :class="enableBreakdown ? 'group-hover/breakdown:text-primary-600 dark:group-hover/breakdown:text-primary-500' : ''"
                  :title="model.model"
                >
                  <span class="inline-flex items-center gap-1">
                    <Icon
                      v-if="enableBreakdown"
                      name="chevronRight"
                      size="xs"
                      :animate-on-hover="false"
                      class="h-3 w-3 shrink-0 transition-transform duration-normal"
                      :class="{ 'rotate-90': expandedKey === `model-${model.model}` }"
                    />
                    {{ model.model }}
                  </span>
                </td>
                <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">
                  {{ formatNumber(model.requests) }}
                </td>
                <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">
                  {{ formatTokens(model.total_tokens) }}
                </td>
                <td class="py-1.5 text-right text-green-600 dark:text-green-400">
                  {{ balanceUnitSymbol }}{{ formatCost(model.actual_cost) }}
                </td>
                <td v-if="showProviderCost" class="py-1.5 text-right text-orange-500 dark:text-orange-400">
                  {{ usdUnitSymbol }}{{ formatCost(model.provider_cost) }}
                </td>
                <td v-if="showStandardCost" class="py-1.5 text-right text-gray-400 dark:text-gray-500">
                  {{ usdUnitSymbol }}{{ formatCost(model.cost) }}
                </td>
              </tr>
              <ExpandableTableRow :open="expandedKey === `model-${model.model}`" :colspan="distributionColspan">
                  <UserBreakdownSubTable
                    :items="breakdownItems"
                    :loading="breakdownLoading"
                    :show-provider-cost="showProviderCost"
                    :show-standard-cost="showStandardCost"
                  />
                </ExpandableTableRow>
            </template>
          </tbody>
        </table>
      </div>
    </div>
    <div
      v-else-if="activeView === 'model_distribution'"
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>

    <ChartSkeleton v-else-if="rankingLoading" variant="distribution" />
    <div
      v-else-if="rankingError"
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.failedToLoad') }}
    </div>
    <!-- 两个视图保持相同的桌面端顶部对齐规则。 -->
    <div v-else-if="rankingDisplayItems.length > 0 && rankingChartData" class="flex flex-col items-center gap-4 sm:flex-row sm:items-start sm:gap-6">
      <div class="h-48 w-48 shrink-0">
        <Doughnut :data="rankingChartData" :options="rankingDoughnutOptions" />
      </div>
      <div class="max-h-48 w-full min-w-0 flex-1 overflow-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-gray-500 dark:text-gray-400">
              <th class="pb-2 text-left">{{ t('admin.dashboard.spendingRankingUser') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.spendingRankingRequests') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.spendingRankingTokens') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.spendingRankingSpend') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(item, index) in rankingDisplayItems"
              :key="item.isOther ? 'others' : `${item.user_id}-${index}`"
              class="border-t border-gray-100 transition-colors dark:border-dark-600"
              :class="item.isOther
                ? 'bg-gray-50/70 dark:bg-dark-700/20'
                : 'cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-700/40'"
              @click="item.isOther ? undefined : emit('ranking-click', item)"
            >
              <td class="py-1.5">
                <div class="flex min-w-0 items-center gap-2">
                  <span class="shrink-0 text-xs font-semibold text-gray-500 dark:text-gray-400">
                    {{ item.isOther ? 'Σ' : `#${index + 1}` }}
                  </span>
                  <span
                    class="block max-w-[140px] truncate font-medium text-gray-900 dark:text-white"
                    :title="getRankingRowLabel(item)"
                  >
                    {{ getRankingRowLabel(item) }}
                  </span>
                </div>
              </td>
              <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">
                {{ formatNumber(item.requests) }}
              </td>
              <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">
                {{ formatTokens(item.tokens) }}
              </td>
              <td class="py-1.5 text-right text-green-600 dark:text-green-400">
                {{ balanceUnitSymbol }}{{ formatCost(item.actual_cost) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div
      v-else
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { getLocale } from '@/i18n'
import { vSegmented } from '@/directives/segmented'
import ExpandableTableRow from '@/components/common/ExpandableTableRow.vue'
import Icon from '@/components/icons/Icon.vue'
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import ChartSkeleton from '@/components/common/ChartSkeleton.vue'
import { useBalanceDisplay } from '@/composables/useBalanceDisplay'
import UserBreakdownSubTable from './UserBreakdownSubTable.vue'
import { externalTooltipHandler, hideExternalTooltip } from '@/utils/chartExternalTooltip'
import { CHART_PALETTE, CHART_OTHER_COLOR } from '@/composables/useChartTheme'
import type { ModelStat, UserSpendingRankingItem, UserBreakdownItem } from '@/types'
import { getUserBreakdown } from '@/api/admin/dashboard'
import { formatTokens } from '@/utils/format'

ChartJS.register(ArcElement, Tooltip, Legend)

onBeforeUnmount(hideExternalTooltip)

const { t } = useI18n()
const { balanceUnitSymbol, usdUnitSymbol } = useBalanceDisplay()

type DistributionMetric = 'tokens' | 'actual_cost'
type ModelSource = 'requested' | 'upstream' | 'mapping'
type RankingDisplayItem = UserSpendingRankingItem & { isOther?: boolean }
const props = withDefaults(defineProps<{
  modelStats: ModelStat[]
  upstreamModelStats?: ModelStat[]
  mappingModelStats?: ModelStat[]
  source?: ModelSource
  enableRankingView?: boolean
  rankingItems?: UserSpendingRankingItem[]
  rankingTotalActualCost?: number
  rankingTotalRequests?: number
  rankingTotalTokens?: number
  loading?: boolean
  metric?: DistributionMetric
  showSourceToggle?: boolean
  showMetricToggle?: boolean
  enableBreakdown?: boolean
  showProviderCost?: boolean
  showStandardCost?: boolean
  rankingLoading?: boolean
  rankingError?: boolean
  startDate?: string
  endDate?: string
  filters?: Record<string, any>
}>(), {
  upstreamModelStats: () => [],
  mappingModelStats: () => [],
  source: 'requested',
  enableRankingView: false,
  rankingItems: () => [],
  rankingTotalActualCost: 0,
  rankingTotalRequests: 0,
  rankingTotalTokens: 0,
  loading: false,
  metric: 'tokens',
  showSourceToggle: false,
  showMetricToggle: false,
  enableBreakdown: true,
  showProviderCost: true,
  showStandardCost: true,
  rankingLoading: false,
  rankingError: false
})

const expandedKey = ref<string | null>(null)
const breakdownItems = ref<UserBreakdownItem[]>([])
const breakdownLoading = ref(false)

const toggleBreakdown = async (type: string, id: string) => {
  const key = `${type}-${id}`
  if (expandedKey.value === key) {
    expandedKey.value = null
    return
  }
  expandedKey.value = key
  breakdownLoading.value = true
  breakdownItems.value = []
  try {
    const res = await getUserBreakdown({
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      model: id,
      model_source: props.source,
    })
    breakdownItems.value = res.users || []
  } catch {
    breakdownItems.value = []
  } finally {
    breakdownLoading.value = false
  }
}

const emit = defineEmits<{
  'update:metric': [value: DistributionMetric]
  'update:source': [value: ModelSource]
  'ranking-click': [item: UserSpendingRankingItem]
}>()

const enableRankingView = computed(() => props.enableRankingView)
const showProviderCost = computed(() => props.showProviderCost)
const showStandardCost = computed(() => props.showStandardCost)
const distributionColspan = computed(() => 4 + (showProviderCost.value ? 1 : 0) + (showStandardCost.value ? 1 : 0))
const activeView = ref<'model_distribution' | 'spending_ranking'>('model_distribution')

const chartColors = CHART_PALETTE

const displayModelStats = computed(() => {
  const sourceStats = props.source === 'upstream'
    ? props.upstreamModelStats
    : props.source === 'mapping'
      ? props.mappingModelStats
      : props.modelStats
  if (!sourceStats?.length) return []

  const metricKey = props.metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
  return [...sourceStats].sort((a, b) => toFiniteNumber(b[metricKey]) - toFiniteNumber(a[metricKey]))
})

// 圆环扇区与 tooltip 共用原始指标值，保证扇区大小与占比一致。
const chartValues = computed(() =>
  displayModelStats.value.map((m) => toFiniteNumber(props.metric === 'actual_cost' ? m.actual_cost : m.total_tokens))
)

const chartData = computed(() => {
  if (!displayModelStats.value.length) return null

  return {
    labels: displayModelStats.value.map((m) => m.model),
    datasets: [
      {
        data: chartValues.value,
        backgroundColor: chartColors.slice(0, displayModelStats.value.length),
        borderWidth: 0
      }
    ]
  }
})

const rankingValues = computed(() => {
  if (!props.rankingItems?.length) return []

  const values = props.rankingItems.map((item) => toFiniteNumber(item.actual_cost))
  if (otherRankingItem.value) values.push(toFiniteNumber(otherRankingItem.value.actual_cost))
  return values
})

const rankingChartData = computed(() => {
  if (!props.rankingItems?.length) return null

  const labels = props.rankingItems.map((item, index) => `#${index + 1} ${getRankingUserLabel(item)}`)
  const backgroundColor: string[] = [...chartColors.slice(0, props.rankingItems.length)]

  if (otherRankingItem.value) {
    labels.push(t('admin.dashboard.spendingRankingOther'))
    backgroundColor.push(CHART_OTHER_COLOR)
  }

  return {
    labels,
    datasets: [
      {
        data: rankingValues.value,
        backgroundColor,
        borderWidth: 0
      }
    ]
  }
})

const otherRankingItem = computed<RankingDisplayItem | null>(() => {
  if (!props.rankingItems?.length) return null

  const rankedActualCost = props.rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.actual_cost), 0)
  const rankedRequests = props.rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.requests), 0)
  const rankedTokens = props.rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.tokens), 0)

  const otherActualCost = Math.max((props.rankingTotalActualCost || 0) - rankedActualCost, 0)
  const otherRequests = Math.max((props.rankingTotalRequests || 0) - rankedRequests, 0)
  const otherTokens = Math.max((props.rankingTotalTokens || 0) - rankedTokens, 0)

  if (otherActualCost <= 0.000001 && otherRequests <= 0 && otherTokens <= 0) return null

  return {
    user_id: 0,
    email: '',
    username: '',
    actual_cost: otherActualCost,
    requests: otherRequests,
    tokens: otherTokens,
    isOther: true
  }
})

const rankingDisplayItems = computed<RankingDisplayItem[]>(() => {
  if (!props.rankingItems?.length) return []
  return otherRankingItem.value
    ? [...props.rankingItems, otherRankingItem.value]
    : [...props.rankingItems]
})

const doughnutOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: {
      display: false
    },
    tooltip: {
      enabled: false,
      external: externalTooltipHandler,
      callbacks: {
        label: (context: any) => {
          const value = chartValues.value[context.dataIndex] ?? 0
          const total = chartValues.value.reduce((a: number, b: number) => a + b, 0)
          const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
          const formattedValue = props.metric === 'actual_cost'
            ? `${balanceUnitSymbol.value}${formatCost(value)}`
            : formatTokens(value)
          return `${context.label}: ${formattedValue} (${percentage}%)`
        }
      }
    }
  }
}))

const rankingDoughnutOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: {
      display: false
    },
    tooltip: {
      enabled: false,
      external: externalTooltipHandler,
      callbacks: {
        label: (context: any) => {
          const value = rankingValues.value[context.dataIndex] ?? 0
          const total = rankingValues.value.reduce((a: number, b: number) => a + b, 0)
          const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
          return `${context.label}: ${balanceUnitSymbol.value}${formatCost(value)} (${percentage}%)`
        }
      }
    }
  }
}))

const formatNumber = (value: number): string => {
  return toFiniteNumber(value).toLocaleString(getLocale())
}

const getRankingUserLabel = (item: UserSpendingRankingItem): string => {
  // 排行标签优先使用用户名，并忽略仅包含空白字符的身份字段。
  if (item.username?.trim()) return item.username.trim()
  if (item.email?.trim()) return item.email.trim()
  return t('admin.redeem.userPrefix', { id: item.user_id })
}

const getRankingRowLabel = (item: RankingDisplayItem): string => {
  if (item.isOther) return t('admin.dashboard.spendingRankingOther')
  return getRankingUserLabel(item)
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}
</script>
