<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import RequestIdSearch from '@/components/common/RequestIdSearch.vue'
import SettingsNotice from '@/components/common/settings/SettingsNotice.vue'
import RequestDetailCard from '@/components/requests/RequestDetailCard.vue'
import OpsErrorDetailModal from '@/views/admin/ops/components/OpsErrorDetailModal.vue'
import Icon from '@/components/icons/Icon.vue'
import { findRequests, type RequestDetail } from '@/api/requests'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const requestId = ref('')
const searchInput = ref<InstanceType<typeof RequestIdSearch> | null>(null)
const loading = ref(false)
const searched = ref(false)
const error = ref('')
const items = ref<RequestDetail[]>([])
const hasMore = ref(false)
const selectedError = ref<number | null>(null)
const showError = ref(false)
let controller: AbortController | undefined

// load 按地址栏里的 ID 查询，新的查询会取消还没返回的旧查询。
async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  const id = requestId.value.trim()
  items.value = []
  hasMore.value = false
  error.value = ''
  if (!id) {
    searched.value = false
    loading.value = false
    return
  }
  loading.value = true
  try {
    const response = await findRequests(id, current.signal)
    if (current.signal.aborted) return
    items.value = response.items || []
    hasMore.value = response.has_more
    searched.value = true
  } catch (cause: unknown) {
    if (current.signal.aborted) return
    const message = (cause as { message?: unknown })?.message
    error.value = typeof message === 'string' && message ? message : t('requests.loadFailed')
  } finally {
    if (controller === current) loading.value = false
  }
}

// search 把 ID 写进地址栏，同一个 ID 再次搜索时直接重新查询。
async function search() {
  const id = requestId.value.trim()
  if (id === (route.query.request_id ?? '')) {
    await load()
    return
  }
  await router.replace({ query: { request_id: id || undefined } })
}

function openError(id: number) {
  selectedError.value = id
  showError.value = true
}

watch(() => [route.path, route.query.request_id], () => {
  requestId.value = typeof route.query.request_id === 'string' ? route.query.request_id : ''
  void load()
}, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- 搜索栏 -->
      <div class="flex flex-wrap items-center gap-2">
        <RequestIdSearch ref="searchInput" v-model="requestId" class="flex-1 sm:w-64 sm:flex-none" @search="search" />
        <button type="button" class="btn btn-primary" :disabled="loading" @click="searchInput?.apply()">
          {{ t('common.search') }}
        </button>
      </div>

      <!-- 加载中：按详情卡片的结构占位 -->
      <div v-if="loading" class="card overflow-hidden" aria-busy="true" :aria-label="t('common.loading')">
        <div class="flex items-center gap-2 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <Skeleton width="4rem" height="1.25rem" />
          <Skeleton width="16rem" height="1.25rem" />
        </div>
        <div class="grid grid-cols-2 gap-4 px-6 py-4 md:grid-cols-4">
          <div v-for="index in 4" :key="index" class="space-y-2">
            <Skeleton width="4rem" height="0.75rem" />
            <Skeleton width="7rem" height="1.25rem" />
          </div>
        </div>
        <div class="space-y-2 border-t border-gray-100 px-6 py-5 dark:border-dark-700">
          <Skeleton v-for="index in 4" :key="index" height="0.75rem" />
        </div>
      </div>

      <!-- 查询失败 -->
      <div v-else-if="error" class="card" role="alert">
        <EmptyState :title="t('requests.loadFailed')" :description="error">
          <template #icon>
            <Icon name="exclamationTriangle" class="empty-state-icon h-10 w-10" />
          </template>
          <template #action>
            <button type="button" class="btn btn-secondary" @click="load">{{ t('common.retry') }}</button>
          </template>
        </EmptyState>
      </div>

      <!-- 还没输入 ID -->
      <div v-else-if="!searched" class="card">
        <EmptyState :title="t('requests.emptyTitle')" :description="t('requests.emptyDescription')">
          <template #icon>
            <Icon name="search" class="empty-state-icon h-10 w-10" />
          </template>
        </EmptyState>
      </div>

      <!-- 没有找到 -->
      <div v-else-if="!items.length" class="card">
        <EmptyState :title="t('requests.notFoundTitle')" :description="t('requests.notFound')" />
      </div>

      <template v-else>
        <p v-if="items.length > 1" class="text-sm text-gray-500 dark:text-dark-300">
          {{ t('requests.matchCount', { count: items.length }) }}
        </p>
        <SettingsNotice v-if="hasMore" tone="warning">{{ t('requests.more') }}</SettingsNotice>
        <RequestDetailCard
          v-for="item in items"
          :key="`${item.request_id}:${item.usage?.[0]?.id ?? ''}:${item.errors?.[0]?.id ?? ''}:${item.audit_ids?.[0] ?? ''}`"
          :item="item"
          @open-error="openError"
        />
      </template>
    </div>

    <OpsErrorDetailModal v-model:show="showError" :error-id="selectedError" error-type="request" />
  </AppLayout>
</template>
