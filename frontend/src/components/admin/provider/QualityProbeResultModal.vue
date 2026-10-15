<template>
  <BaseDialog
    :show="show"
    :title="t('admin.providers.qualityProbeDialog.title', { name: provider?.name ?? '' })"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4 text-sm">
      <template v-if="runningCount">
        <p class="text-gray-600 dark:text-dark-300" data-testid="quality-probe-running">
          {{
            runs.length > 1
              ? t('admin.providers.qualityProbeDialog.runningMany', { count: runningCount, total: runs.length })
              : t('admin.providers.qualityProbeDialog.running', { name: provider?.name ?? '' })
          }}
        </p>
        <p class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.qualityProbeDialog.runningHint') }}
        </p>
      </template>

      <article
        v-for="run in runs"
        :key="run.model"
        class="space-y-3 rounded-control border border-gray-100 p-3 dark:border-dark-700"
        data-testid="quality-probe-run"
      >
        <div class="flex items-center justify-between gap-3">
          <h3 class="min-w-0 truncate font-medium text-gray-900 dark:text-white">{{ run.model }}</h3>
          <span :class="statusClass(run)">{{ statusText(run) }}</span>
        </div>

        <p v-if="run.phase === 'error'" class="text-red-600 dark:text-red-400" data-testid="quality-probe-error">
          {{ run.error }}
        </p>

        <template v-else-if="run.report">
          <p v-if="run.report.skipped" class="text-gray-800 dark:text-dark-100" data-testid="quality-probe-skipped">
            {{ skipReasonText(run.report) }}
          </p>
          <p v-else-if="run.report.upstream_error" class="text-red-600 dark:text-red-400">
            {{ t('admin.providers.qualityProbeDialog.upstreamError') }}
          </p>
          <p v-else-if="run.report.degraded && run.report.kept_for_coverage" class="text-amber-700 dark:text-amber-300">
            {{ t('admin.providers.qualityProbeDialog.kept') }}
          </p>
          <p v-else-if="run.report.degraded" class="text-red-600 dark:text-red-400">
            {{ t('admin.providers.qualityProbeDialog.degraded') }}
          </p>
          <p v-else class="text-emerald-700 dark:text-emerald-300">
            {{ t('admin.providers.qualityProbeDialog.passed') }}
          </p>

          <dl v-if="!run.report.skipped" class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
            <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.candy') }}</dt>
            <dd>{{ run.report.candy_ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}</dd>
            <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.trace') }}</dt>
            <dd>
              {{ run.report.trace_ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}
              <span v-if="run.report.trace_prediction" class="text-gray-500 dark:text-dark-400">
                （{{ fingerprintLabel(run.report.trace_prediction, run.report.trace_probability) }}）
              </span>
            </dd>
            <dt class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.qualityProbeDialog.consecutive') }}</dt>
            <dd>{{ run.report.consecutive_fails }}</dd>
          </dl>

          <p v-if="run.report.temp_unscheduled" class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.providers.qualityProbeDialog.tempUnscheduled') }}
          </p>
          <p v-if="run.report.cycle_stopped" class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.providers.qualityProbeDialog.cycleStopped') }}
          </p>
          <p v-if="run.report.email_sent" class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.providers.qualityProbeDialog.emailSent') }}
          </p>
          <p v-if="run.report.error" class="break-words text-xs text-red-600 dark:text-red-400">
            {{ run.report.error }}
          </p>
          <QualityProbeSamples v-if="runs.length === 1 && run.report.samples?.length" :samples="run.report.samples" />
          <details v-else-if="run.report.samples?.length" class="text-xs">
            <summary class="cursor-pointer text-gray-500 dark:text-dark-400">
              {{ t('admin.providers.qualityProbeDialog.samples') }}
            </summary>
            <QualityProbeSamples :samples="run.report.samples" />
          </details>
        </template>
      </article>
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
import type { QualityProbeRun } from '@/components/admin/provider/qualityProbeRun'
import type { Provider } from '@/types'
import type { QualityProbeReport } from '@/api/admin/providers'

const props = defineProps<{
  show: boolean
  provider: Provider | null
  runs: QualityProbeRun[]
}>()

const emit = defineEmits<{ close: [] }>()
const { t, te } = useI18n()

const runningCount = computed(() => props.runs.filter(run => run.phase === 'running').length)

function fingerprintLabel(prediction?: string, probability?: number) {
  if (!prediction) {
    return ''
  }
  if (probability == null || Number.isNaN(probability)) {
    return prediction
  }
  return `${prediction} ${Math.round(probability * 100)}%`
}

function skipReasonText(report: QualityProbeReport) {
  const reason = report.skip_reason || ''
  const key = `admin.providers.qualityProbeDialog.skipReasons.${reason}`
  if (reason && te(key)) {
    return t(key)
  }
  return t('admin.providers.qualityProbeSkipped', {
    name: props.provider?.name ?? '',
    reason,
  })
}

function statusText(run: QualityProbeRun) {
  if (run.phase === 'running') {
    return t('admin.providers.qualityProbeDialog.pending')
  }
  if (run.phase === 'error') {
    return t('admin.providers.qualityProbeDialog.requestFailed')
  }
  const report = run.report
  if (!report || report.skipped) {
    return t('admin.providers.qualityProbeList.skipped')
  }
  if (report.upstream_error) {
    return t('admin.providers.qualityProbeList.upstreamError')
  }
  if (report.degraded) {
    return t('admin.providers.qualityProbeList.degraded')
  }
  return t('admin.providers.qualityProbeList.passed')
}

function statusClass(run: QualityProbeRun) {
  if (run.phase === 'running') {
    return 'shrink-0 text-gray-500 dark:text-dark-400'
  }
  if (run.phase === 'error' || run.report?.upstream_error) {
    return 'shrink-0 text-red-600 dark:text-red-400'
  }
  if (run.report?.skipped) {
    return 'shrink-0 text-gray-500 dark:text-dark-400'
  }
  if (run.report?.degraded && run.report.kept_for_coverage) {
    return 'shrink-0 text-amber-700 dark:text-amber-300'
  }
  if (run.report?.degraded) {
    return 'shrink-0 text-red-600 dark:text-red-400'
  }
  return 'shrink-0 text-emerald-700 dark:text-emerald-300'
}
</script>
