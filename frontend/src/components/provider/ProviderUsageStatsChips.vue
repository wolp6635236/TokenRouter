<template>
  <div class="mb-0.5 flex items-center">
    <div class="flex flex-wrap items-center justify-end gap-1.5 text-xs lg:justify-start">
      <span
        v-for="chip in chips"
        :key="chip.key"
        :data-stat="chip.key"
        :title="chip.hint"
        class="whitespace-nowrap rounded-compact bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700"
      >
        <span class="text-gray-400 dark:text-gray-500">{{ chip.label }}</span> <span class="tabular-nums text-gray-600 dark:text-gray-300">{{ chip.value }}</span>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useBalanceDisplay } from '@/composables/useBalanceDisplay'
import { formatCompactNumber } from '@/utils/format'

const props = defineProps<{
  stats: {
    requests: number
    tokens: number
    cost: number
    user_cost?: number | null
  }
  // today 表示今日统计，window 表示当前用量窗口内的统计，只影响悬浮说明。
  scope: 'today' | 'window'
}>()

const { t } = useI18n()
const { formatBalanceAmount, formatUsdAmount } = useBalanceDisplay()

const chips = computed(() => {
  const period = t(`admin.providers.usageStats.period.${props.scope}`)
  const hint = (key: string) => t(`admin.providers.usageStats.hints.${key}`, { period })
  const items = [
    {
      key: 'requests',
      label: t('admin.providers.usageStats.requests'),
      value: formatCompactNumber(props.stats.requests, { allowBillions: false }),
      hint: hint('requests')
    },
    {
      key: 'tokens',
      label: t('admin.providers.usageStats.tokens'),
      value: formatCompactNumber(props.stats.tokens),
      hint: hint('tokens')
    },
    {
      key: 'cost',
      label: t('admin.providers.usageStats.cost'),
      value: formatUsdAmount(props.stats.cost, { fractionDigits: 2 }),
      hint: hint('cost')
    }
  ]
  // 数据包含 user_cost 时展示用户扣费，旧数据可能缺少该字段。
  if (props.stats.user_cost != null) {
    items.push({
      key: 'userCost',
      label: t('admin.providers.usageStats.userCost'),
      value: formatBalanceAmount(props.stats.user_cost, { fractionDigits: 2 }),
      hint: hint('userCost')
    })
  }
  return items
})
</script>
