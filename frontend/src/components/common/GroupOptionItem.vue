<template>
  <div class="flex min-w-0 flex-1 items-start justify-between gap-3">
    <!-- 左侧：分组名称与描述 -->
    <div
      class="flex min-w-0 flex-1 flex-col items-start"
      :title="description || undefined"
    >
      <!-- 第一行：品牌标签，分组名称加粗 -->
      <GroupBadge
        :name="name"
        :display-brand="displayBrand"
        :show-rate="false"
        class="groupOptionItemBadge"
      />
      <!-- 第二行：分组描述，和标题保持轻微间距 -->
      <span
        v-if="description"
        class="mt-1.5 w-full whitespace-pre-line [overflow-wrap:anywhere] text-left text-xs leading-relaxed text-gray-500 dark:text-gray-400 line-clamp-3"
      >
        {{ description }}
      </span>
    </div>

    <!-- 右侧同排展示负载、倍率和勾选状态。 -->
    <div class="flex shrink-0 flex-wrap items-start justify-end gap-2 pt-0.5">
      <GroupCapacityBadge
        v-if="capacity"
        class="max-w-[11rem] justify-end"
        layout="horizontal"
        :concurrency-used="capacity.concurrency_used"
        :concurrency-max="capacity.concurrency_max"
        :sessions-used="capacity.sessions_used"
        :sessions-max="capacity.sessions_max"
        :rpm-used="capacity.rpm_used"
        :rpm-max="capacity.rpm_max"
      />
      <!-- 倍率标签使用品牌配色 -->
      <span v-if="rateMultiplier !== undefined" :class="['inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold', ratePillClass]">
        <template v-if="hasCustomRate">
          <span class="mr-1 line-through opacity-50">{{ rateMultiplier }}x</span>
          <span class="font-bold">{{ userRateMultiplier }}x</span>
        </template>
        <template v-else>
          {{ rateMultiplier }}x {{ t('common.multiplier') }}
        </template>
      </span>
      <span
        v-if="hasPeakRate"
        class="inline-flex items-center whitespace-nowrap rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
        :title="peakRateTitle"
      >
        {{ peakRateText }}
      </span>
      <!-- 选中勾 -->
      <Icon
        name="check"
        size="sm"
        :animate-on-hover="false"
        v-if="showCheckmark && selected"
        class="h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import GroupCapacityBadge from './GroupCapacityBadge.vue'
import type { MarketplaceGroupCapacity } from '@/types'
import { currentServerTimezoneLabel, formatPeakRateWindow } from '@/utils/peak-rate'
import { resolveProviderBrand } from '@/utils/providerBrand'

const { t } = useI18n()

interface Props {
  name: string
  displayBrand?: string | null
  rateMultiplier?: number
  userRateMultiplier?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  description?: string | null
  capacity?: MarketplaceGroupCapacity
  selected?: boolean
  showCheckmark?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  selected: false,
  showCheckmark: true,
  userRateMultiplier: null,
  peakRateEnabled: false
})

const brandName = computed(() => props.displayBrand?.trim() || '')

// 是否存在不同于默认倍率的用户专属倍率。
const hasCustomRate = computed(() => {
  return (
    props.userRateMultiplier !== null &&
    props.userRateMultiplier !== undefined &&
    props.rateMultiplier !== undefined &&
    props.userRateMultiplier !== props.rateMultiplier
  )
})

const hasPeakRate = computed(() => {
  return Boolean(props.peakRateEnabled && props.peakStart && props.peakEnd)
})

const peakRateText = computed(() => {
  return formatPeakRateWindow(
    {
      peak_rate_enabled: props.peakRateEnabled,
      peak_start: props.peakStart,
      peak_end: props.peakEnd,
      peak_rate_multiplier: props.peakRateMultiplier
    },
    currentServerTimezoneLabel()
  )
})

const peakRateTitle = computed(() => {
  return t('common.peakRateTooltip', { window: peakRateText.value })
})

// 倍率标签跟随展示品牌；没有品牌时使用中性色。
const ratePillClass = computed(() => {
  if (brandName.value) {
    return `ring-1 ring-inset ${resolveProviderBrand(brandName.value).badgeClass}`
  }
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200'
})
</script>

<style scoped>
/* 下拉选项里的分组名称需要比普通标签更醒目。 */
.groupOptionItemBadge :deep(span.truncate) {
  font-weight: 600;
}
</style>
