<template>
  <BaseDialog
    :show="show"
    :title="t('admin.providers.qualityProbeDialog.title', { name: provider?.name ?? '' })"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4 text-sm">
      <p v-if="loading" class="text-gray-600 dark:text-dark-300" data-testid="quality-probe-running">
        {{ t('admin.providers.qualityProbeDialog.running', { name: provider?.name ?? '' }) }}
      </p>
      <p v-if="loading" class="text-xs text-gray-500 dark:text-dark-400">
        {{ t('admin.providers.qualityProbeDialog.runningHint') }}
      </p>

      <p v-if="error" class="text-red-600 dark:text-red-400" data-testid="quality-probe-error">
        {{ error }}
      </p>

      <template v-if="report && !loading">
        <p v-if="report.skipped" class="text-gray-800 dark:text-dark-100" data-testid="quality-probe-skipped">
          {{ skipReasonText }}
        </p>
        <p v-else-if="report.upstream_error" class="text-red-600 dark:text-red-400">
          {{ t('admin.providers.qualityProbeDialog.upstreamError') }}
        </p>
        <p v-else-if="report.degraded && report.kept_for_coverage" class="text-amber-700 dark:text-amber-300">
          {{ t('admin.providers.qualityProbeDialog.kept') }}
        </p>
        <p v-else-if="report.degraded" class="text-red-600 dark:text-red-400">
          {{ t('admin.providers.qualityProbeDialog.degraded') }}
        </p>
        <p v-else class="text-emerald-700 dark:text-emerald-300">
          {{ t('admin.providers.qualityProbeDialog.passed') }}
        </p>

        <dl v-if="!report.skipped" class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.model') }}</dt>
          <dd>{{ report.model || '—' }}</dd>
          <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.candy') }}</dt>
          <dd>{{ report.candy_ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}</dd>
          <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.trace') }}</dt>
          <dd>
            {{ report.trace_ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}
            <span v-if="report.trace_prediction" class="text-gray-500 dark:text-dark-400">
              （{{ fingerprintLabel(report.trace_prediction, report.trace_probability) }}）
            </span>
          </dd>
          <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.consecutive') }}</dt>
          <dd>{{ report.consecutive_fails }}</dd>
        </dl>

        <p v-if="report.temp_unscheduled" class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.qualityProbeDialog.tempUnscheduled') }}
        </p>
        <p v-if="report.cycle_stopped" class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.qualityProbeDialog.cycleStopped') }}
        </p>
        <p v-if="report.email_sent" class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.qualityProbeDialog.emailSent') }}
        </p>
        <p v-if="report.error" class="break-words text-xs text-red-600 dark:text-red-400">
          {{ report.error }}
        </p>
        <QualityProbeSamples v-if="report.samples?.length" :samples="report.samples" />
      </template>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ t('admin.providers.qualityProbeDialog.close') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import QualityProbeSamples from '@/components/admin/provider/QualityProbeSamples.vue'
import type { Provider } from '@/types'
import type { QualityProbeReport } from '@/api/admin/providers'

const props = defineProps<{
  show: boolean
  provider: Provider | null
  loading: boolean
  report: QualityProbeReport | null
  error: string
}>()

const emit = defineEmits<{ close: [] }>()
const { t, te } = useI18n()

function fingerprintLabel(prediction?: string, probability?: number) {
  if (!prediction) {
    return ''
  }
  if (probability == null || Number.isNaN(probability)) {
    return prediction
  }
  return `${prediction} ${Math.round(probability * 100)}%`
}

const skipReasonText = computed(() => {
  const reason = props.report?.skip_reason || ''
  const key = `admin.providers.qualityProbeDialog.skipReasons.${reason}`
  if (reason && te(key)) {
    return t(key)
  }
  return t('admin.providers.qualityProbeSkipped', {
    name: props.provider?.name ?? '',
    reason,
  })
})
</script>
