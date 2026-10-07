<template>
  <span
    v-if="result"
    class="badge text-xs"
    :class="badgeClass"
    :title="titleText"
  >
    {{ label }}
  </span>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import { qualityProbeListResult } from './qualityProbeListResult'

const props = defineProps<{
  extra?: unknown
}>()

const { t } = useI18n()

const result = computed(() => qualityProbeListResult(props.extra))

const label = computed(() => {
  if (!result.value) {
    return ''
  }
  if (result.value.kind === 'degraded') {
    return t('admin.providers.qualityProbeList.degraded')
  }
  if (result.value.kind === 'upstream_error') {
    return t('admin.providers.qualityProbeList.upstreamError')
  }
  if (result.value.kind === 'skipped') {
    return t('admin.providers.qualityProbeList.skipped')
  }
  return t('admin.providers.qualityProbeList.passed')
})

const badgeClass = computed(() => {
  if (!result.value) {
    return ''
  }
  if (result.value.kind === 'degraded') {
    return 'badge-warning'
  }
  if (result.value.kind === 'upstream_error') {
    return 'badge-danger'
  }
  if (result.value.kind === 'skipped') {
    return 'badge-gray'
  }
  return 'badge-success'
})

const titleText = computed(() => {
  if (!result.value?.at) {
    return ''
  }
  return formatDateTime(result.value.at)
})
</script>
