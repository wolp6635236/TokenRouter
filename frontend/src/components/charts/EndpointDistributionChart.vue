<template>
  <div class="card p-4">
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ title || t('usage.endpointDistribution') }}
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
            :class="{ 'segmented-item-active': source === 'inbound' }"
            @click="emit('update:source', 'inbound')"
          >
            {{ t('usage.inbound') }}
          </button>
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': source === 'upstream' }"
            @click="emit('update:source', 'upstream')"
          >
            {{ t('usage.upstream') }}
          </button>
          <button
            type="button"
            class="segmented-item px-2.5 py-1 text-xs"
            :class="{ 'segmented-item-active': source === 'path' }"
            @click="emit('update:source', 'path')"
          >
            {{ t('usage.path') }}
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
      </div>
    </div>
    <ChartSkeleton v-if="loading" variant="distribution" />
    <!-- 桌面端表格与圆环图顶部对齐。 -->
    <div v-else-if="displayEndpointStats.length > 0 && chartData" class="flex flex-col items-center gap-4 sm:flex-row sm:items-start sm:gap-6">
      <div class="h-48 w-48 shrink-0">
        <Bar v-if="chartType === 'bar'" :data="chartData" :options="barOptions" />
        <Doughnut v-else :data="doughnutChartData" :options="doughnutOptions" />
      </div>
      <div class="max-h-48 w-full min-w-0 flex-1 overflow-auto">
        <table class="w-full text-xs">
          <thead>
            <tr class="text-gray-500 dark:text-gray-400">
              <th class="pb-2 text-left">{{ t('usage.endpoint') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.requests') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.tokens') }}</th>
              <th class="pb-2 text-right">{{ t('admin.dashboard.actual') }}</th>
              <th v-if="showStandardCost" class="pb-2 text-right">{{ t('admin.dashboard.standard') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="item in displayEndpointStats" :key="item.endpoint">
              <tr data-icon-trigger
                class="group/breakdown border-t border-gray-100 transition-colors dark:border-dark-600"
                :class="enableBreakdown ? 'cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-700/40' : ''"
                @click="enableBreakdown && toggleBreakdown(item.endpoint)"
              >
                <!-- 端点名称默认使用普通文字色，悬停可展开行时提示可点击。 -->
                <td
                  class="max-w-[180px] truncate py-1.5 font-medium text-gray-900 dark:text-white"
                  :class="enableBreakdown ? 'group-hover/breakdown:text-primary-600 dark:group-hover/breakdown:text-primary-500' : ''"
                  :title="item.endpoint"
                >
                  <span class="inline-flex items-center gap-1">
                    <Icon
                      v-if="enableBreakdown"
                      name="chevronRight"
                      size="xs"
                      :animate-on-hover="false"
                      class="h-3 w-3 shrink-0 transition-transform duration-normal"
                      :class="{ 'rotate-90': expandedKey === item.endpoint }"
                    />
                    {{ item.endpoint }}
                  </span>
                </td>
                <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">
                  {{ formatNumber(item.requests) }}
                </td>
                <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">
                  {{ formatTokens(item.total_tokens) }}
                </td>
                <td class="py-1.5 text-right text-green-600 dark:text-green-400">
                  {{ balanceUnitSymbol }}{{ formatCost(item.actual_cost) }}
                </td>
                <td v-if="showStandardCost" class="py-1.5 text-right text-gray-400 dark:text-gray-500">
                  {{ usdUnitSymbol }}{{ formatCost(item.cost) }}
                </td>
              </tr>
              <ExpandableTableRow :open="expandedKey === item.endpoint" :colspan="distributionColspan">
                  <UserBreakdownSubTable
                    :items="breakdownItems"
                    :loading="breakdownLoading"
                  />
                </ExpandableTableRow>
            </template>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
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
import { Chart as ChartJS, ArcElement, BarElement, CategoryScale, LinearScale, Tooltip, Legend } from 'chart.js'
import { Bar, Doughnut } from 'vue-chartjs'
import ChartSkeleton from '@/components/common/ChartSkeleton.vue'
import { useBalanceDisplay } from '@/composables/useBalanceDisplay'
import UserBreakdownSubTable from './UserBreakdownSubTable.vue'
import { externalTooltipHandler, hideExternalTooltip } from '@/utils/chartExternalTooltip'
import { CHART_PALETTE } from '@/composables/useChartTheme'
import type { EndpointStat, UserBreakdownItem } from '@/types'
import { getUserBreakdown } from '@/api/admin/dashboard'
import { formatTokens } from '@/utils/format'

ChartJS.register(ArcElement, BarElement, CategoryScale, LinearScale, Tooltip, Legend)

onBeforeUnmount(hideExternalTooltip)

const { t } = useI18n()
const { balanceUnitSymbol, usdUnitSymbol } = useBalanceDisplay()

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
// 图表默认为圆环图，用户用量页使用水平条形图。
type EndpointChartType = 'doughnut' | 'bar'

const props = withDefaults(
  defineProps<{
    endpointStats: EndpointStat[]
    upstreamEndpointStats?: EndpointStat[]
    endpointPathStats?: EndpointStat[]
    loading?: boolean
    title?: string
    metric?: DistributionMetric
    chartType?: EndpointChartType
    source?: EndpointSource
    showMetricToggle?: boolean
    showSourceToggle?: boolean
    enableBreakdown?: boolean
    showStandardCost?: boolean
    startDate?: string
    endDate?: string
    filters?: Record<string, any>
  }>(),
  {
    upstreamEndpointStats: () => [],
    endpointPathStats: () => [],
    loading: false,
    title: '',
    metric: 'tokens',
    chartType: 'doughnut',
    source: 'inbound',
    showMetricToggle: false,
    showSourceToggle: false,
    enableBreakdown: true,
    showStandardCost: true,
  }
)

const emit = defineEmits<{
  'update:metric': [value: DistributionMetric]
  'update:source': [value: EndpointSource]
}>()

const expandedKey = ref<string | null>(null)
const breakdownItems = ref<UserBreakdownItem[]>([])
const breakdownLoading = ref(false)
const showStandardCost = computed(() => props.showStandardCost)
const distributionColspan = computed(() => 4 + (showStandardCost.value ? 1 : 0))

const toggleBreakdown = async (endpoint: string) => {
  if (expandedKey.value === endpoint) {
    expandedKey.value = null
    return
  }
  expandedKey.value = endpoint
  breakdownLoading.value = true
  breakdownItems.value = []
  try {
    const res = await getUserBreakdown({
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      endpoint,
      endpoint_type: props.source,
    })
    breakdownItems.value = res.users || []
  } catch {
    breakdownItems.value = []
  } finally {
    breakdownLoading.value = false
  }
}

const chartColors = CHART_PALETTE

const displayEndpointStats = computed(() => {
  const sourceStats = props.source === 'upstream'
    ? props.upstreamEndpointStats
    : props.source === 'path'
      ? props.endpointPathStats
      : props.endpointStats
  if (!sourceStats?.length) return []

  const metricKey = props.metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
  return [...sourceStats].sort((a, b) => b[metricKey] - a[metricKey])
})

// 圆环扇区与 tooltip 共用原始指标值，保证扇区大小与占比一致。
const chartValues = computed(() =>
  displayEndpointStats.value.map((item) =>
    props.metric === 'actual_cost' ? item.actual_cost : item.total_tokens
  )
)

const chartData = computed(() => {
  if (!displayEndpointStats.value?.length) return null

  return {
    labels: displayEndpointStats.value.map((item) => item.endpoint),
    datasets: [
      {
        data: chartValues.value,
        backgroundColor: chartColors.slice(0, displayEndpointStats.value.length),
        borderWidth: 0
      }
    ]
  }
})

// 与 chartData 同生命周期（无数据时模板分支不会渲染），保持非空类型以通过图表组件的 data 校验。
const doughnutChartData = computed(() => ({
  labels: displayEndpointStats.value.map((item) => item.endpoint),
  datasets: [
    {
      data: chartValues.value,
      backgroundColor: chartColors.slice(0, displayEndpointStats.value.length),
      borderWidth: 0
    }
  ]
}))

// 圆环图与柱状图共用 tooltip，按原始指标值展示「名称: 数值 (占比)」。
const tooltipLabel = (context: any) => {
  const value = chartValues.value[context.dataIndex] ?? 0
  const total = chartValues.value.reduce((a: number, b: number) => a + b, 0)
  const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
  const formattedValue = props.metric === 'actual_cost'
    ? `${balanceUnitSymbol.value}${formatCost(value)}`
    : formatTokens(value)
  return `${context.label}: ${formattedValue} (${percentage}%)`
}

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
        label: tooltipLabel
      }
    }
  }
}))

const barOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  scales: {
    // 端点名通常较长，交给 tooltip 与右侧表格展示，坐标轴只保留柱形本身。
    x: {
      ticks: {
        display: false
      },
      grid: {
        display: false
      }
    },
    y: {
      // 数值轴从零开始，柱形高度与原始用量成正比。
      type: 'linear' as const,
      beginAtZero: true,
      ticks: {
        display: false
      },
      grid: {
        display: false
      }
    }
  },
  plugins: {
    legend: {
      display: false
    },
    tooltip: {
      enabled: false,
      external: externalTooltipHandler,
      callbacks: {
        label: tooltipLabel
      }
    }
  }
}))

const formatNumber = (value: number): string => {
  return value.toLocaleString(getLocale())
}

const formatCost = (value: number): string => {
  if (value >= 1000) {
    return (value / 1000).toFixed(2) + 'K'
  } else if (value >= 1) {
    return value.toFixed(2)
  } else if (value >= 0.01) {
    return value.toFixed(3)
  }
  return value.toFixed(4)
}
</script>
