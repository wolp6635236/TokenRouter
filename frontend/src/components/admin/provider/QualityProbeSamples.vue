<template>
  <div v-if="samples.length" class="space-y-4">
    <section
      v-for="sample in samples"
      :key="sample.name"
      class="space-y-2"
    >
      <div class="flex items-center justify-between gap-2">
        <p class="font-medium text-gray-900 dark:text-white">{{ sampleLabel(sample.name) }}</p>
        <span
          class="badge"
          :class="sample.ok ? 'badge-success' : 'badge-warning'"
        >
          {{ sample.ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}
        </span>
      </div>
      <div>
        <p class="mb-1 text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.qualityProbeLogs.prompt') }}
        </p>
        <pre class="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-surface bg-gray-50 p-3 text-xs text-gray-800 dark:bg-dark-800 dark:text-dark-100">{{ sample.prompt || '—' }}</pre>
      </div>
      <div>
        <p class="mb-1 text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.qualityProbeLogs.answer') }}
        </p>
        <pre class="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-surface bg-gray-50 p-3 text-xs text-gray-800 dark:bg-dark-800 dark:text-dark-100">{{ sample.answer || '—' }}</pre>
      </div>
      <p v-if="sample.error" class="break-words text-xs text-red-600 dark:text-red-400">
        {{ sample.error }}
      </p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { QualityProbeSample } from '@/api/admin/providers'

defineProps<{
  samples: QualityProbeSample[]
}>()

const { t } = useI18n()

function sampleLabel(name: string) {
  if (name === 'candy') {
    return t('admin.providers.qualityProbeDialog.candy')
  }
  if (name === 'trace_1') {
    return t('admin.providers.qualityProbeLogs.traceN', { n: 1 })
  }
  if (name === 'trace_2') {
    return t('admin.providers.qualityProbeLogs.traceN', { n: 2 })
  }
  if (name === 'trace_3') {
    return t('admin.providers.qualityProbeLogs.traceN', { n: 3 })
  }
  return name
}
</script>
