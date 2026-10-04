<template>
  <BaseDialog
    :show="show"
    :title="t('admin.providers.tempUnschedulable.statusTitle')"
    width="normal"
    @close="handleClose"
  >
    <div class="space-y-4">
      <ContentSkeleton v-if="loading" variant="detail" :rows="4" class="py-4" />

      <div v-else-if="!isActive" class="rounded-control border border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
        {{ t('admin.providers.tempUnschedulable.notActive') }}
      </div>

      <div v-else class="space-y-4">
        <div class="rounded-control border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300">
          {{ t('admin.providers.recoverStateHint') }}
        </div>

        <div class="rounded-control border border-gray-200 p-4 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.providers.tempUnschedulable.providerName') }}
          </p>
          <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ provider?.name || '-' }}
          </p>
        </div>

        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.providers.tempUnschedulable.triggeredAt') }}
            </p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ triggeredAtText }}
            </p>
          </div>
          <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.providers.tempUnschedulable.until') }}
            </p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ untilText }}
            </p>
          </div>
          <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.providers.tempUnschedulable.remaining') }}
            </p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ remainingText }}
            </p>
          </div>
          <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.providers.tempUnschedulable.errorCode') }}
            </p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ state?.status_code || '-' }}
            </p>
          </div>
          <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.providers.tempUnschedulable.matchedKeyword') }}
            </p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ state?.matched_keyword || '-' }}
            </p>
          </div>
          <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.providers.tempUnschedulable.ruleOrder') }}
            </p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">
              {{ ruleIndexDisplay }}
            </p>
          </div>
        </div>

        <div class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.providers.tempUnschedulable.errorMessage') }}
          </p>
          <div class="mt-2 rounded-compact bg-gray-50 p-2 text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-300">
            {{ errorMessageText }}
          </div>
        </div>

        <div
          v-if="hasThresholdEvidence"
          class="rounded-control border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-300"
          data-testid="temp-unsched-trigger-evidence"
        >
          {{ triggerEvidenceText }}
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.close') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!isActive || resetting"
          @click="handleReset"
        >
          <Icon
            name="loader"
            size="sm"
            :animate-on-hover="false"
            v-if="resetting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
          />
          {{ t('admin.providers.recoverState') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import ContentSkeleton from '@/components/common/ContentSkeleton.vue'
import Icon from '@/components/icons/Icon.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { Provider, TempUnschedulableStatus } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { formatDateTime } from '@/utils/format'
import { displayTempUnschedErrorMessage } from '@/components/provider/tempUnschedErrorMessage'

const props = defineProps<{
  show: boolean
  provider: Provider | null
}>()

const emit = defineEmits<{
  close: []
  reset: [provider: Provider]
}>()

const { t, te } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const resetting = ref(false)
const status = ref<TempUnschedulableStatus | null>(null)

const state = computed(() => status.value?.state || null)

const isActive = computed(() => {
  if (!status.value?.active || !state.value) return false
  return state.value.until_unix * 1000 > Date.now()
})

const ruleIndexDisplay = computed(() => {
  if (!state.value || !state.value.matched_keyword || state.value.rule_index < 0) return '-'
  return state.value.rule_index + 1
})

const hasThresholdEvidence = computed(() => (state.value?.trigger_count || 0) > 1)

const triggerEvidenceText = computed(() => {
  const count = state.value?.trigger_count || 0
  const threshold = state.value?.trigger_threshold || 0
  const minutes = state.value?.trigger_window_minutes || 0
  if (threshold > 0 && minutes > 0) {
    return t('admin.providers.tempUnschedulable.multipleErrorTrigger', { count, threshold, minutes })
  }
  if (threshold > 0) {
    return t('admin.providers.tempUnschedulable.multipleErrorTriggerNoWindow', { count, threshold })
  }
  if (minutes > 0) {
    return t('admin.providers.tempUnschedulable.multipleErrorCountInWindow', { count, minutes })
  }
  return t('admin.providers.tempUnschedulable.multipleErrorCount', { count })
})

const errorMessageText = computed(() => displayTempUnschedErrorMessage(state.value?.error_message, t, te))

const triggeredAtText = computed(() => {
  if (!state.value?.triggered_at_unix) return '-'
  return formatDateTime(new Date(state.value.triggered_at_unix * 1000))
})

const untilText = computed(() => {
  if (!state.value?.until_unix) return '-'
  return formatDateTime(new Date(state.value.until_unix * 1000))
})

const remainingText = computed(() => {
  if (!state.value) return '-'
  const remainingMs = state.value.until_unix * 1000 - Date.now()
  if (remainingMs <= 0) {
    return t('admin.providers.tempUnschedulable.expired')
  }
  const minutes = Math.ceil(remainingMs / 60000)
  if (minutes < 60) {
    return t('admin.providers.tempUnschedulable.remainingMinutes', { minutes })
  }
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  if (rest === 0) {
    return t('admin.providers.tempUnschedulable.remainingHours', { hours })
  }
  return t('admin.providers.tempUnschedulable.remainingHoursMinutes', { hours, minutes: rest })
})

const loadStatus = async () => {
  if (!props.provider) return
  loading.value = true
  try {
    status.value = await adminAPI.providers.getTempUnschedulableStatus(props.provider.id)
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.providers.tempUnschedulable.failedToLoad'))
    status.value = null
  } finally {
    loading.value = false
  }
}

const handleClose = () => {
  emit('close')
}

const handleReset = async () => {
  if (!props.provider) return
  resetting.value = true
  try {
    const updated = await adminAPI.providers.recoverState(props.provider.id)
    appStore.showSuccess(t('admin.providers.recoverStateSuccess'))
    emit('reset', updated)
    handleClose()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.providers.recoverStateFailed'))
  } finally {
    resetting.value = false
  }
}

watch(
  () => [props.show, props.provider?.id],
  ([visible]) => {
    if (visible && props.provider) {
      loadStatus()
      return
    }
    status.value = null
  }
)
</script>
