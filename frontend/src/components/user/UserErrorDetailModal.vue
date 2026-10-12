<template>
  <BaseDialog :show="show" :title="t('usage.errors.detail.title')" width="wide" @close="emit('update:show', false)">
    <!-- 加载中 -->
    <ContentSkeleton v-if="loading" variant="detail" :rows="8" class="p-6" />

    <!-- 加载失败 -->
    <div v-else-if="loadError" class="py-8 text-center text-sm text-red-500">
      {{ t('usage.errors.detail.loadFailed') }}
    </div>

    <!-- 详情内容 -->
    <div v-else-if="detail" class="space-y-4 text-sm">
      <div class="grid grid-cols-2 gap-x-6 gap-y-3">
        <!-- 请求 ID -->
        <div v-if="detail.request_id" class="col-span-2">
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('requests.id') }}</span>
          <p class="mt-0.5"><RequestIdLink :value="detail.request_id" full /></p>
        </div>
        <!-- 时间 -->
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.time') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ formatDateTime(detail.created_at) }}</p>
        </div>
        <!-- 模型 -->
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.model') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.model || '-' }}</p>
        </div>
        <!-- 端点 -->
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.endpoint') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.inbound_endpoint || '-' }}</p>
        </div>
        <!-- 状态码 -->
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.status') }}</span>
          <p class="mt-0.5">
            <span class="badge" :class="statusClass(detail.status_code)">{{ detail.status_code || '-' }}</span>
          </p>
        </div>
        <!-- 分类 -->
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.category') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ t('usage.errors.categories.' + detail.category) }}</p>
        </div>
        <!-- 平台 -->
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.platform') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.platform || '-' }}</p>
        </div>
        <!-- 上游状态码 -->
        <div v-if="detail.upstream_status_code != null">
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.detail.upstreamStatus') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.upstream_status_code }}</p>
        </div>
      </div>

      <!-- 错误消息 -->
      <div v-if="detail.message">
        <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.message') }}</span>
        <p class="mt-0.5 text-gray-900 dark:text-dark-100 break-all">{{ detail.message }}</p>
      </div>

      <!-- 错误响应体 -->
      <div v-if="detail.error_body">
        <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.errors.detail.responseBody') }}</span>
        <pre class="mt-1 overflow-auto max-h-[40vh] whitespace-pre-wrap break-all rounded-surface bg-gray-50 dark:bg-dark-900 border border-gray-200 dark:border-dark-700 p-3 text-xs text-gray-800 dark:text-dark-200">{{ detail.error_body }}</pre>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import RequestIdLink from '@/components/common/RequestIdLink.vue'
import ContentSkeleton from '@/components/common/ContentSkeleton.vue'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getMyErrorDetail } from '@/api/usage'
import { formatDateTime } from '@/utils/format'
import type { UserErrorRequestDetail } from '@/types'

const props = defineProps<{
  show: boolean
  errorId: number | null
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
}>()

const { t } = useI18n()

const loading = ref(false)
const loadError = ref(false)
const detail = ref<UserErrorRequestDetail | null>(null)

watch(
  () => [props.show, props.errorId] as const,
  ([show, id]) => {
    if (show && id != null) {
      fetchDetail(id)
    } else if (!show) {
      detail.value = null
      loadError.value = false
    }
  }
)

async function fetchDetail(id: number) {
  loading.value = true
  loadError.value = false
  detail.value = null
  try {
    detail.value = await getMyErrorDetail(id)
  } catch (e) {
    console.error('[UserErrorDetailModal] Failed to load error detail:', e)
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function statusClass(code: number) {
  if (code >= 500) return 'badge-danger'
  if (code === 429) return 'badge-warning'
  return 'badge-gray'
}
</script>
