<template>
  <span
    :class="[
      'inline-flex items-center gap-1.5 rounded-compact px-2 py-0.5 text-xs font-medium transition-colors',
      badgeClass
    ]"
  >
    <!-- 分组使用管理员配置的展示品牌。 -->
    <ProviderIcon v-if="brandName" :brand="brandName" size="14px" />
    <!-- 分组名加 self-baseline，徽章的基线取自分组名，移动卡片的标签和它对齐。 -->
    <span class="self-baseline truncate">{{ name }}</span>
    <!-- Right side label -->
    <span v-if="showLabel" :class="labelClass">
      <template v-if="hasCustomRate">
        <!-- 原倍率删除线 + 专属倍率高亮 -->
        <span class="line-through opacity-50 mr-0.5">{{ rateMultiplier }}x</span>
        <span class="font-bold">{{ userRateMultiplier }}x</span>
      </template>
      <template v-else>
        {{ labelText }}
      </template>
    </span>
    <span v-if="hasPeakRate" :class="peakRateClass" :title="peakRateTitle">
      {{ peakRateText }}
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { currentServerTimezoneLabel, formatPeakRateWindow } from '@/utils/peak-rate'
import ProviderIcon from './ProviderIcon.vue'
import { resolveProviderBrand } from '@/utils/providerBrand'

interface Props {
  name: string
  displayBrand?: string | null
  rateMultiplier?: number
  userRateMultiplier?: number | null // 用户专属倍率
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  showRate?: boolean
  daysRemaining?: number | null
}

const props = withDefaults(defineProps<Props>(), {
  showRate: true,
  daysRemaining: null,
  userRateMultiplier: null,
  peakRateEnabled: false
})

const { t } = useI18n()

const brandName = computed(() => props.displayBrand?.trim() || '')

// 是否有专属倍率（且与默认倍率不同）
const hasCustomRate = computed(() => {
  return (
    props.userRateMultiplier !== null &&
    props.userRateMultiplier !== undefined &&
    props.rateMultiplier !== undefined &&
    props.userRateMultiplier !== props.rateMultiplier
  )
})

const hasPeakRate = computed(() => {
  return Boolean(props.showRate && props.peakRateEnabled && props.peakStart && props.peakEnd)
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

// 是否显示右侧标签
const showLabel = computed(() => {
  if (!props.showRate) return false
  if (props.daysRemaining !== null && props.daysRemaining !== undefined) return true
  return props.rateMultiplier !== undefined || hasCustomRate.value
})

// Label text
const labelText = computed(() => {
  if (props.daysRemaining !== null && props.daysRemaining !== undefined) {
    if (props.daysRemaining <= 0) {
      return t('admin.users.expired')
    }
    return t('admin.users.daysRemaining', { days: props.daysRemaining })
  }
  return props.rateMultiplier !== undefined ? `${props.rateMultiplier}x` : ''
})

// Label style based on type and days remaining
const labelClass = computed(() => {
  const base = 'px-1.5 py-0.5 rounded-compact text-xs font-semibold'

  if (props.daysRemaining === null || props.daysRemaining === undefined) {
    return `${base} bg-black/10 dark:bg-white/10`
  }

  if (props.daysRemaining <= 0 || props.daysRemaining <= 3) {
    return `${base} bg-red-200/80 text-red-800 dark:bg-red-800/50 dark:text-red-300`
  }
  if (props.daysRemaining <= 7) {
    return `${base} bg-amber-200/80 text-amber-800 dark:bg-amber-800/50 dark:text-amber-300`
  }

  return `${base} bg-violet-200/60 text-violet-800 dark:bg-violet-800/40 dark:text-violet-300`
})

const peakRateClass = computed(() => {
  return 'px-1.5 py-0.5 rounded-compact text-xs font-semibold bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
})

const badgeClass = computed(() => {
  if (brandName.value) {
    return `ring-1 ring-inset ${resolveProviderBrand(brandName.value).badgeClass}`
  }
  // 未指定品牌时，与复合分组共用中性配色，并用内描边明确徽章边缘。
  return 'ring-1 ring-inset bg-gray-100 text-gray-900 ring-gray-200 dark:bg-dark-800 dark:text-dark-50 dark:ring-dark-600'
})
</script>
