<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex items-center justify-end">
          <button
            type="button"
            class="btn btn-secondary btn-icon shrink-0"
            :disabled="loading"
            :title="t('common.refresh')"
            :aria-label="t('common.refresh')"
            @click="load"
          >
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>
      <template #table>
        <DataTable
          column-order-storage-key="admin-quality-probe-logs-column-order"
          :columns="columns"
          :data="rows"
          :loading="loading"
          default-sort-key="at"
          default-sort-order="desc"
          row-key="row_key"
        >
          <template #cell-at="{ value }">
            <span class="text-sm text-gray-600 dark:text-dark-300">{{ formatDateTime(value) }}</span>
          </template>
          <template #cell-provider_name="{ row }">
            <div class="min-w-0">
              <div class="truncate font-medium text-gray-900 dark:text-white">{{ row.provider_name || '—' }}</div>
              <div class="text-xs text-gray-500 dark:text-dark-400">#{{ row.provider_id }}</div>
            </div>
          </template>
          <template #cell-trigger="{ value }">
            {{ value === 'manual' ? t('admin.providers.qualityProbeLogs.triggerManual') : t('admin.providers.qualityProbeLogs.triggerAuto') }}
          </template>
          <template #cell-candy_ok="{ row }">
            <span v-if="row.skipped">—</span>
            <span v-else>{{ row.candy_ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}</span>
          </template>
          <template #cell-trace_ok="{ row }">
            <span v-if="row.skipped">—</span>
            <span v-else>
              {{ row.trace_ok ? t('admin.providers.qualityProbeDialog.pass') : t('admin.providers.qualityProbeDialog.fail') }}
              <span v-if="row.trace_prediction" class="block text-xs text-gray-500 dark:text-dark-400">
                {{ row.trace_prediction }}
              </span>
            </span>
          </template>
          <template #cell-verdict="{ row }">
            <span
              class="badge"
              :class="verdictClass(row)"
            >
              {{ verdictLabel(row) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="row.skipped || !row.samples?.length"
              @click="openDetail(row)"
            >
              {{ t('admin.providers.qualityProbeLogs.view') }}
            </button>
          </template>
          <template #empty>
            <p class="py-8 text-center text-sm text-gray-500 dark:text-dark-400">
              {{ t('admin.providers.qualityProbeLogs.empty') }}
            </p>
          </template>
        </DataTable>
      </template>
      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>
    <BaseDialog
      :show="Boolean(detail)"
      :title="detailTitle"
      width="wide"
      @close="detail = null"
    >
      <div v-if="detail" class="space-y-4 text-sm">
        <p v-if="detail.trace_prediction" class="text-gray-600 dark:text-dark-300">
          {{ t('admin.providers.qualityProbeLogs.fingerprint') }}：{{ detail.trace_prediction }}
        </p>
        <p v-if="detail.error" class="break-words text-xs text-red-600 dark:text-red-400">
          {{ detail.error }}
        </p>
        <QualityProbeSamples :samples="detail.samples || []" />
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="detail = null">
          {{ t('admin.providers.qualityProbeDialog.close') }}
        </button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { adminAPI } from '@/api/admin'
import type { QualityProbeLogItem } from '@/api/admin/settings'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import QualityProbeSamples from '@/components/admin/provider/QualityProbeSamples.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const logs = ref<QualityProbeLogItem[]>([])
const detail = ref<QualityProbeLogItem | null>(null)
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0
})

const detailTitle = computed(() => {
  if (!detail.value) {
    return t('admin.providers.qualityProbeLogs.title')
  }
  return t('admin.providers.qualityProbeLogs.detailTitle', {
    name: detail.value.provider_name || `#${detail.value.provider_id}`
  })
})

const rows = computed(() =>
  logs.value.map((item, index) => ({
    ...item,
    row_key: `${item.provider_id}-${item.at}-${index}`
  }))
)

const columns = computed<Column[]>(() => [
  { key: 'at', label: t('admin.providers.qualityProbeLogs.time'), sortable: true },
  { key: 'provider_name', label: t('admin.providers.qualityProbeLogs.provider') },
  { key: 'trigger', label: t('admin.providers.qualityProbeLogs.trigger') },
  { key: 'candy_ok', label: t('admin.providers.qualityProbeLogs.candy') },
  { key: 'trace_ok', label: t('admin.providers.qualityProbeLogs.trace') },
  { key: 'verdict', label: t('admin.providers.qualityProbeLogs.verdict') },
  { key: 'model', label: t('admin.providers.qualityProbeLogs.model') },
  { key: 'actions', label: t('common.actions') }
])

function verdictLabel(row: QualityProbeLogItem) {
  if (row.skipped) {
    return t('admin.providers.qualityProbeLogs.verdictSkipped')
  }
  if (row.degraded && row.kept_for_coverage) {
    return t('admin.providers.qualityProbeLogs.verdictKept')
  }
  if (row.degraded) {
    return t('admin.providers.qualityProbeLogs.verdictDegraded')
  }
  return t('admin.providers.qualityProbeLogs.verdictPass')
}

function verdictClass(row: QualityProbeLogItem) {
  if (row.skipped) {
    return 'badge-gray'
  }
  if (row.degraded) {
    return 'badge-warning'
  }
  return 'badge-success'
}

function openDetail(row: QualityProbeLogItem) {
  detail.value = row
}

async function load() {
  loading.value = true
  try {
    const res = await adminAPI.settings.listQualityProbeLogs(pagination.page, pagination.page_size)
    logs.value = res.items
    pagination.total = res.total
    pagination.page = res.page
    pagination.page_size = res.page_size
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.providers.qualityProbeLogs.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handlePageChange(next: number) {
  pagination.page = next
  load()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  load()
}

onMounted(load)
</script>
