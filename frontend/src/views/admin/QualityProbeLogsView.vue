<!-- QualityProbeLogsView.vue lists automatic and manual quality-probe records for administrators. Provider and action columns remain visible while optional log fields can be hidden in browser storage. -->
<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex items-center justify-end gap-2">
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
          <div ref="columnDropdownRef" class="relative">
            <button
              type="button"
              class="btn btn-secondary btn-icon shrink-0"
              :title="t('admin.providers.viewColumns')"
              :aria-label="t('admin.providers.viewColumns')"
              @click.stop="showColumnDropdown = !showColumnDropdown"
            >
              <Icon name="grid" size="sm" />
            </button>
            <MotionTransition name="dropdown-fade">
              <div
                v-if="showColumnDropdown"
                :inert="!showColumnDropdown || undefined"
                class="dropdown right-0 top-full z-50 mt-1 max-h-menu w-48 overflow-y-auto"
              >
                <button
                  v-for="col in toggleableColumns"
                  :key="col.key"
                  type="button"
                  class="dropdown-item justify-between"
                  @click="toggleColumn(col.key)"
                >
                  <span>{{ col.label }}</span>
                  <Icon
                    v-if="isColumnVisible(col.key)"
                    name="check"
                    size="sm"
                    class="text-primary-500"
                    :stroke-width="2"
                    :animate-on-hover="false"
                  />
                </button>
              </div>
            </MotionTransition>
          </div>
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
            <span class="text-sm text-gray-600 dark:text-dark-300">{{
              formatDateTime(value)
            }}</span>
          </template>
          <template #cell-provider_name="{ row }">
            <div class="min-w-0">
              <div class="truncate font-medium text-gray-900 dark:text-white">{{
                row.provider_name || '—'
              }}</div>
              <div class="text-xs text-gray-500 dark:text-dark-400">#{{ row.provider_id }}</div>
            </div>
          </template>
          <template #cell-trigger="{ value }">
            {{
              value === 'manual'
                ? t('admin.providers.qualityProbeLogs.triggerManual')
                : t('admin.providers.qualityProbeLogs.triggerAuto')
            }}
          </template>
          <template #cell-candy_ok="{ row }">
            <span v-if="row.skipped">—</span>
            <span v-else>{{
              row.candy_ok
                ? t('admin.providers.qualityProbeDialog.pass')
                : t('admin.providers.qualityProbeDialog.fail')
            }}</span>
          </template>
          <template #cell-trace_ok="{ row }">
            <span v-if="row.skipped">—</span>
            <span v-else>
              {{
                row.trace_ok
                  ? t('admin.providers.qualityProbeDialog.pass')
                  : t('admin.providers.qualityProbeDialog.fail')
              }}
              <span
                v-if="row.trace_prediction"
                class="block text-xs text-gray-500 dark:text-dark-400"
              >
                {{ row.trace_prediction }}
              </span>
            </span>
          </template>
          <template #cell-verdict="{ row }">
            <span class="badge" :class="verdictClass(row)">
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
    <BaseDialog :show="Boolean(detail)" :title="detailTitle" width="wide" @close="detail = null">
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
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { adminAPI } from '@/api/admin'
import type { QualityProbeLogItem } from '@/api/admin/settings'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/table/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/table/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import QualityProbeSamples from '@/components/admin/provider/QualityProbeSamples.vue'
import {
  QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY,
  migrateQualityProbeLogsProviderFirst,
} from './qualityProbeLogsColumns'
import MotionTransition from '@/components/common/MotionTransition.vue'
import Icon from '@/components/icons/Icon.vue'

if (typeof window !== 'undefined') {
  migrateQualityProbeLogsProviderFirst(localStorage)
}

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const logs = ref<QualityProbeLogItem[]>([])
const detail = ref<QualityProbeLogItem | null>(null)
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
})

const detailTitle = computed(() => {
  if (!detail.value) {
    return t('admin.providers.qualityProbeLogs.title')
  }
  return t('admin.providers.qualityProbeLogs.detailTitle', {
    name: detail.value.provider_name || `#${detail.value.provider_id}`,
  })
})

const rows = computed(() =>
  logs.value.map((item, index) => ({
    ...item,
    row_key: `${item.provider_id}-${item.at}-${index}`,
  })),
)

const ALWAYS_VISIBLE_COLUMNS = new Set(['provider_name', 'actions'])
const allColumns = computed<Column[]>(() => [
  { key: 'provider_name', label: t('admin.providers.qualityProbeLogs.provider') },
  { key: 'at', label: t('admin.providers.qualityProbeLogs.time'), sortable: true },
  { key: 'trigger', label: t('admin.providers.qualityProbeLogs.trigger') },
  { key: 'candy_ok', label: t('admin.providers.qualityProbeLogs.candy') },
  { key: 'trace_ok', label: t('admin.providers.qualityProbeLogs.trace') },
  { key: 'verdict', label: t('admin.providers.qualityProbeLogs.verdict') },
  { key: 'model', label: t('admin.providers.qualityProbeLogs.model') },
  { key: 'actions', label: t('common.actions') },
])
const toggleableColumns = computed(() =>
  allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key)),
)
const hiddenColumns = reactive(new Set<string>())
const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

function loadSavedColumns() {
  hiddenColumns.clear()
  try {
    const saved = localStorage.getItem(QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY)
    const validKeys = new Set(toggleableColumns.value.map((col) => col.key))
    if (!saved) {
      return
    }
    const parsed: unknown = JSON.parse(saved)
    if (!Array.isArray(parsed)) {
      return
    }
    parsed
      .filter((key): key is string => typeof key === 'string' && validKeys.has(key))
      .forEach((key) => hiddenColumns.add(key))
  } catch (error) {
    console.error('Failed to load quality probe log columns:', error)
  }
}

function saveColumnsToStorage() {
  try {
    const validKeys = new Set(toggleableColumns.value.map((col) => col.key))
    const keys = [...hiddenColumns].filter((key) => validKeys.has(key))
    localStorage.setItem(QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY, JSON.stringify(keys))
  } catch (error) {
    console.error('Failed to save quality probe log columns:', error)
  }
}

function isColumnVisible(key: string) {
  return !hiddenColumns.has(key)
}

function toggleColumn(key: string) {
  if (ALWAYS_VISIBLE_COLUMNS.has(key) || !toggleableColumns.value.some((col) => col.key === key)) {
    return
  }
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  saveColumnsToStorage()
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Node)) {
    return
  }
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(target)) {
    showColumnDropdown.value = false
  }
}

const columns = computed<Column[]>(() =>
  allColumns.value.filter(
    (col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key),
  ),
)

loadSavedColumns()

function verdictLabel(row: QualityProbeLogItem) {
  if (row.skipped) {
    return t('admin.providers.qualityProbeLogs.verdictSkipped')
  }
  if (row.upstream_error) {
    return t('admin.providers.qualityProbeLogs.verdictUpstreamError')
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
  if (row.upstream_error) {
    return 'badge-danger'
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

onMounted(() => {
  load()
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>
