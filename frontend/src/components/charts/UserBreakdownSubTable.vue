<template>
  <div class="bg-gray-50/50 dark:bg-dark-700/30">
    <div v-if="!loading && items.length === 0" class="py-2 text-center text-xs text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
    <table v-else :aria-busy="loading" class="w-full text-xs">
      <TableSkeletonBody v-if="loading" :columns="4 + Number(showProviderCost) + Number(showStandardCost)" :rows="3" cell-class="px-3 py-2" />
      <tbody v-else>
        <tr
          v-for="user in items"
          :key="user.user_id"
          class="border-t border-gray-100/50 dark:border-dark-600/50"
        >
          <td class="max-w-[120px] truncate py-1 pl-6 text-gray-600 dark:text-gray-300" :title="user.email">
            {{ user.email || `User #${user.user_id}` }}
          </td>
          <td class="py-1 text-right text-gray-500 dark:text-gray-400">
            {{ user.requests.toLocaleString(getLocale()) }}
          </td>
          <td class="py-1 text-right text-gray-500 dark:text-gray-400">
            {{ formatTokens(user.total_tokens) }}
          </td>
          <td class="py-1 text-right text-green-600 dark:text-green-400">
            {{ balanceUnitSymbol }}{{ formatCost(user.actual_cost) }}
          </td>
          <td v-if="showProviderCost" class="py-1 text-right text-orange-500 dark:text-orange-400">
            {{ usdUnitSymbol }}{{ formatCost(user.provider_cost) }}
          </td>
          <td v-if="showStandardCost" class="py-1 pr-1 text-right text-gray-400 dark:text-gray-500">
            {{ usdUnitSymbol }}{{ formatCost(user.cost) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { getLocale } from '@/i18n'
import TableSkeletonBody from '@/components/common/TableSkeletonBody.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useBalanceDisplay } from '@/composables/useBalanceDisplay'
import type { UserBreakdownItem } from '@/types'
import { formatTokens } from '@/utils/format'

const { t } = useI18n()
const { balanceUnitSymbol, usdUnitSymbol } = useBalanceDisplay()

const props = withDefaults(defineProps<{
  items: UserBreakdownItem[]
  loading?: boolean
  showProviderCost?: boolean
  showStandardCost?: boolean
}>(), {
  loading: false,
  showProviderCost: true,
  showStandardCost: true,
})

const showProviderCost = computed(() => props.showProviderCost)
const showStandardCost = computed(() => props.showStandardCost)

const formatCost = (value: number | undefined | null): string => {
  if (value == null) return '0.0000'
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}
</script>
