<template>
  <Teleport to="body">

      <!-- Backdrop: click anywhere outside to close -->
      <div v-if="show && position" class="fixed inset-0 z-menu-overlay" @click="emit('close')"></div>
      <MotionTransition name="dropdown-fade">
        <div
          v-if="show && position"
          class="action-menu action-menu-content max-h-[calc(100dvh-16px)] w-52 overflow-y-auto"
          :style="{ top: position.top + 'px', left: position.left + 'px' }"
          @click.stop
        >
          <div class="py-1">
            <template v-if="provider">
              <button @click="$emit('test', provider); $emit('close')" class="dropdown-item">
                <Icon name="play" size="sm" class="text-green-500" :stroke-width="2" />
                {{ t('admin.providers.testConnection') }}
              </button>
              <div
                v-if="provider.platform === 'openai'"
                ref="probeTriggerEl"
                @mouseenter="openProbeFlyout"
                @mouseleave="scheduleCloseProbeFlyout"
              >
                <button
                  type="button"
                  class="dropdown-item w-full justify-between"
                  @click.prevent="openProbeFlyout"
                >
                  <span class="flex min-w-0 items-center gap-2">
                    <Icon name="beaker" size="sm" class="text-amber-500" :stroke-width="2" />
                    {{ t('admin.providers.qualityProbe') }}
                  </span>
                  <Icon name="chevronRight" size="sm" class="text-gray-400 dark:text-dark-400" />
                </button>
              </div>
              <button @click="$emit('stats', provider); $emit('close')" class="dropdown-item">
                <Icon name="chart" size="sm" class="text-indigo-500" />
                {{ t('admin.providers.viewStats') }}
              </button>
              <button @click="$emit('advanced-scheduler-score', provider); $emit('close')" class="dropdown-item">
                <Icon name="calculator" size="sm" class="text-violet-500" />
                {{ t('admin.providers.advancedSchedulerScore.action') }}
              </button>
              <button @click="$emit('schedule', provider); $emit('close')" class="dropdown-item">
                <Icon name="clock" size="sm" class="text-orange-500" />
                {{ t('admin.scheduledTests.schedule') }}
              </button>
              <button v-if="canDuplicate" @click="$emit('duplicate', provider); $emit('close')" class="dropdown-item">
                <Icon name="copy" size="sm" class="text-sky-500" />
                {{ t('admin.providers.duplicateProvider') }}
              </button>
              <!-- 影子提供商不持凭据:重授权/刷新 token 对其无效(后端拒绝),故隐藏(外审 G4)。 -->
              <template v-if="supportsReauth">
                <button @click="$emit('reauth', provider); $emit('close')" class="dropdown-item text-blue-600">
                  <Icon name="link" size="sm" />
                  {{ t('admin.providers.reAuthorize') }}
                </button>
              </template>
              <template v-if="supportsTokenRefresh">
                <button @click="$emit('refresh-token', provider); $emit('close')" class="dropdown-item text-purple-600">
                  <Icon name="refresh" size="sm" />
                  {{ t('admin.providers.refreshToken') }}
                </button>
              </template>
              <button v-if="isOpenAIOAuthParent" @click="$emit('create-spark-shadow', provider); $emit('close')" class="dropdown-item text-amber-600">
                <Icon name="sparkles" size="sm" />
                {{ t('admin.providers.createSparkShadow') }}
              </button>
              <button v-if="supportsPrivacy" @click="$emit('set-privacy', provider); $emit('close')" class="dropdown-item text-emerald-600">
                <Icon name="shield" size="sm" />
                {{ t('admin.providers.setPrivacy') }}
              </button>
              <button v-if="isOpenAIOAuth" @click="$emit('invite-reset', provider); $emit('close')" class="dropdown-item text-cyan-600">
                <Icon name="gift" size="sm" />
                {{ t('admin.providers.inviteReset') }}
              </button>
              <div v-if="hasRecoverableState" class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
              <button v-if="hasRecoverableState" @click="$emit('recover-state', provider); $emit('close')" class="dropdown-item text-emerald-600">
                <Icon name="sync" size="sm" />
                {{ t('admin.providers.recoverState') }}
              </button>
              <button v-if="hasQuotaLimit" @click="$emit('reset-quota', provider); $emit('close')" class="dropdown-item text-teal-600">
                <Icon name="refresh" size="sm" />
                {{ t('admin.providers.resetQuota') }}
              </button>
              <!-- 删除置于菜单底部，并继续交由页面弹出确认框。 -->
              <div class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
              <button type="button" @click="$emit('delete', provider); $emit('close')" class="dropdown-item text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20">
                <Icon name="trash" size="sm" />
                {{ t('common.delete') }}
              </button>
            </template>
          </div>
        </div>
      </MotionTransition>
      <MotionTransition name="dropdown-fade">
        <div
          v-if="show && probeFlyoutOpen && probeFlyoutPos"
          class="action-menu max-h-64 w-56 overflow-y-auto"
          :style="{ top: probeFlyoutPos.top + 'px', left: probeFlyoutPos.left + 'px' }"
          @click.stop
          @mouseenter="cancelCloseProbeFlyout"
          @mouseleave="scheduleCloseProbeFlyout"
        >
          <div class="py-1">
            <p v-if="probeModelsLoading" class="px-4 py-2 text-xs text-gray-500 dark:text-dark-400">
              {{ t('common.loading') }}
            </p>
            <p v-else-if="probeModelsError" class="px-4 py-2 text-xs text-red-600 dark:text-red-400">
              {{ probeModelsError }}
            </p>
            <p v-else-if="!probeModels.length" class="px-4 py-2 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.providers.qualityProbeModelsEmpty') }}
            </p>
            <button
              v-for="model in probeModels"
              :key="model.id"
              type="button"
              class="dropdown-item-sm"
              @click="selectProbeModel(model.id)"
            >
              <span class="min-w-0 truncate">{{ model.display_name || model.id }}</span>
            </button>
          </div>
        </div>
      </MotionTransition>
  </Teleport>
</template>

<script setup lang="ts">
import MotionTransition from '@/components/common/MotionTransition.vue'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'
import { adminAPI } from '@/api/admin'
import type { ClaudeModel, Provider } from '@/types'

const props = defineProps<{ show: boolean; provider: Provider | null; position: { top: number; left: number } | null }>()
const emit = defineEmits(['close', 'test', 'quality-probe', 'stats', 'advanced-scheduler-score', 'schedule', 'duplicate', 'reauth', 'refresh-token', 'recover-state', 'reset-quota', 'set-privacy', 'invite-reset', 'create-spark-shadow', 'delete'])
const { t } = useI18n()
const probeTriggerEl = ref<HTMLElement | null>(null)
const probeFlyoutOpen = ref(false)
const probeFlyoutPos = ref<{ top: number; left: number } | null>(null)
const probeModels = ref<ClaudeModel[]>([])
const probeModelsLoading = ref(false)
const probeModelsError = ref('')
let probeCloseTimer: number | undefined
let probeLoadSeq = 0
const canDuplicate = computed(() => {
  if (!props.provider || props.provider.parent_provider_id != null) return false
  return ['apikey', 'upstream', 'bedrock', 'service_account'].includes(props.provider.type)
})
const isRateLimited = computed(() => {
  if (props.provider?.rate_limit_reset_at && new Date(props.provider.rate_limit_reset_at) > new Date()) {
    return true
  }
  const modelLimits = (props.provider?.extra as Record<string, unknown> | undefined)?.model_rate_limits as
    | Record<string, { rate_limit_reset_at: string }>
    | undefined
  if (modelLimits) {
    const now = new Date()
    return Object.values(modelLimits).some(info => new Date(info.rate_limit_reset_at) > now)
  }
  return false
})
const isOverloaded = computed(() => props.provider?.overload_until && new Date(props.provider.overload_until) > new Date())
const isTempUnschedulable = computed(() => props.provider?.temp_unschedulable_until && new Date(props.provider.temp_unschedulable_until) > new Date())
const hasRecoverableState = computed(() => {
  return props.provider?.status === 'error' || Boolean(isRateLimited.value) || Boolean(isOverloaded.value) || Boolean(isTempUnschedulable.value)
})
const isAntigravityOAuth = computed(() => props.provider?.platform === 'antigravity' && props.provider?.type === 'oauth')
const isOpenAIOAuth = computed(() => props.provider?.platform === 'openai' && props.provider?.type === 'oauth')
// 影子提供商(链接型,持 parent_provider_id)不持凭据、type 不可变,凭据/隐私类操作对其无效。
const isShadow = computed(() => props.provider?.parent_provider_id != null)
const supportsReauth = computed(() =>
  (props.provider?.type === 'oauth' || props.provider?.type === 'setup-token') && !isShadow.value
)
const supportsTokenRefresh = computed(() =>
  supportsReauth.value || (
    props.provider?.platform === 'qoder' &&
    props.provider?.type === 'cosy' &&
    props.provider.credentials_status?.has_refresh_token === true &&
    !isShadow.value
  )
)
// OpenAI OAuth 母提供商指自身不是影子提供商(parent_provider_id == null)的提供商。
const isOpenAIOAuthParent = computed(() => isOpenAIOAuth.value && !isShadow.value)
const supportsPrivacy = computed(() => (isAntigravityOAuth.value || isOpenAIOAuth.value) && !isShadow.value)
const hasQuotaLimit = computed(() => {
  return (props.provider?.type === 'apikey' || props.provider?.type === 'bedrock') && (
    (props.provider?.quota_limit ?? 0) > 0 ||
    (props.provider?.quota_daily_limit ?? 0) > 0 ||
    (props.provider?.quota_weekly_limit ?? 0) > 0
  )
})

const handleKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape') emit('close')
}

function cancelCloseProbeFlyout() {
  if (probeCloseTimer !== undefined) {
    window.clearTimeout(probeCloseTimer)
    probeCloseTimer = undefined
  }
}

function closeProbeFlyout() {
  cancelCloseProbeFlyout()
  probeFlyoutOpen.value = false
  probeFlyoutPos.value = null
}

function scheduleCloseProbeFlyout() {
  cancelCloseProbeFlyout()
  probeCloseTimer = window.setTimeout(() => {
    closeProbeFlyout()
  }, 120)
}

function placeProbeFlyout() {
  const rect = probeTriggerEl.value?.getBoundingClientRect()
  if (!rect) {
    return
  }
  const width = 224
  let left = rect.left - width - 4
  if (left < 8) {
    left = rect.right + 4
  }
  const maxLeft = window.innerWidth - width - 8
  if (left > maxLeft) {
    left = Math.max(8, maxLeft)
  }
  probeFlyoutPos.value = {
    top: Math.max(8, rect.top),
    left
  }
}

async function openProbeFlyout() {
  if (!props.provider) {
    return
  }
  cancelCloseProbeFlyout()
  probeFlyoutOpen.value = true
  await nextTick()
  placeProbeFlyout()
  const seq = ++probeLoadSeq
  const providerId = props.provider.id
  probeModelsLoading.value = true
  probeModelsError.value = ''
  try {
    const models = await adminAPI.providers.getAvailableModels(providerId)
    if (seq !== probeLoadSeq) {
      return
    }
    probeModels.value = models
  } catch (error: any) {
    if (seq !== probeLoadSeq) {
      return
    }
    probeModels.value = []
    probeModelsError.value = error?.message || t('admin.providers.qualityProbeModelsLoadFailed')
  } finally {
    if (seq === probeLoadSeq) {
      probeModelsLoading.value = false
    }
  }
}

function selectProbeModel(model: string) {
  if (!props.provider) {
    return
  }
  emit('quality-probe', props.provider, model)
  emit('close')
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      window.addEventListener('keydown', handleKeydown)
    } else {
      window.removeEventListener('keydown', handleKeydown)
      closeProbeFlyout()
      probeModels.value = []
      probeModelsError.value = ''
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  closeProbeFlyout()
})
</script>
