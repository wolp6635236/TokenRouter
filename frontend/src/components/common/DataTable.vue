<template>
  <div v-if="!isDesktopViewport" class="space-y-4" :aria-busy="loading">
    <template v-if="loading">
      <div v-for="i in 5" :key="i" class="rounded-surface border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
        <div class="space-y-3">
          <div v-for="column in dataColumns" :key="column.key" class="flex justify-between">
            <Skeleton :width="80" :height="16" />
            <Skeleton :width="128" :height="16" />
          </div>
          <div v-if="hasActionsColumn" class="border-t border-gray-200 pt-3 dark:border-dark-700">
            <Skeleton :height="32" />
          </div>
        </div>
      </div>
    </template>

    <template v-else-if="!data || data.length === 0">
      <div class="rounded-surface border border-gray-200 bg-white p-12 text-center dark:border-dark-700 dark:bg-dark-900">
        <slot name="empty">
          <div class="flex flex-col items-center">
            <Icon
              name="inbox"
              size="xl"
              class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500"
            />
            <p class="text-lg font-medium text-gray-900 dark:text-gray-100">
              {{ t('empty.noData') }}
            </p>
          </div>
        </slot>
      </div>
    </template>

    <template v-else>
      <div v-if="selectable" class="flex items-center justify-end gap-2 px-1">
        <label class="flex items-center gap-2 text-sm font-medium text-gray-600 dark:text-gray-300">
          <input
            type="checkbox"
            class="h-4 w-4 rounded-compact border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
            :checked="allVisibleSelected"
            :indeterminate="someVisibleSelected"
            data-test="select-all-mobile"
            @change="toggleAllVisible(($event.target as HTMLInputElement).checked)"
          />
          <span>{{ t('common.selectAll') }}</span>
        </label>
      </div>
      <div
        v-for="(row, index) in sortedData"
        :key="resolveRowKey(row, index)"
        class="rounded-surface border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
        :class="{
          'cursor-pointer': clickableRows,
          'border-primary-300 bg-primary-50/40 dark:border-primary-700 dark:bg-primary-900/10': selectable && isRowSelected(row, index)
        }"
        @click="clickableRows && emit('rowClick', row)"
      >
        <div class="space-y-3">
          <div v-if="selectable" class="flex justify-end">
            <input
              type="checkbox"
              class="h-4 w-4 rounded-compact border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
              :checked="isRowSelected(row, index)"
              :aria-label="getRowSelectionLabel(row, index)"
              data-test="select-row"
              @click.stop
              @change="toggleRowSelection(row, index, ($event.target as HTMLInputElement).checked)"
            />
          </div>
          <!-- 标签和取值的字号不同，两边按首行文字的基线对齐。取值以图标或色条开头时，单元格给文字加 self-baseline。 -->
          <div
            v-for="column in dataColumns"
            :key="column.key"
            :data-field="column.key"
            class="flex min-w-0 items-baseline justify-between gap-4"
          >
            <span class="text-xs font-medium tracking-wider text-gray-500 dark:text-dark-300">
              {{ column.label }}
            </span>
            <div class="min-w-0 max-w-full text-right text-sm text-gray-900 dark:text-gray-100">
              <slot :name="`cell-${column.key}`" :row="row" :value="row[column.key]" :expanded="actionsExpanded">
                {{ column.formatter ? column.formatter(row[column.key], row) : row[column.key] }}
              </slot>
            </div>
          </div>
          <div v-if="hasActionsColumn" class="border-t border-gray-200 pt-3 dark:border-dark-700">
            <slot name="cell-actions" :row="row" :value="row['actions']" :expanded="actionsExpanded"></slot>
          </div>
        </div>
      </div>
    </template>
  </div>

  <div
    v-else
    ref="tableWrapperRef"
    class="table-wrapper sticky-boundary-line"
    :aria-busy="loading"
    :class="{
      'actions-expanded': actionsExpanded,
      'is-scrollable': isScrollable
    }"
    @dragover="handleTableDragOver"
    @dragenter="handleTableDragOver"
    @dragleave="handleTableDragLeave"
  >
    <table class="w-full min-w-max divide-y divide-gray-200 dark:divide-dark-700">
      <thead class="table-header bg-gray-50 dark:bg-dark-900">
        <tr>
          <th
            v-if="selectable"
            scope="col"
            class="sticky-header-cell table-selection-cell py-2"
          >
            <input
              type="checkbox"
              class="h-4 w-4 rounded-compact border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
              :checked="allVisibleSelected"
              :indeterminate="someVisibleSelected"
              :aria-label="t('common.selectAll')"
              data-test="select-all"
              @change="toggleAllVisible(($event.target as HTMLInputElement).checked)"
            />
          </th>
          <th data-icon-trigger
            v-for="(column, index) in orderedColumns"
            :key="column.key"
            :data-column-key="column.key"
            scope="col"
            :aria-sort="column.sortable ? getColumnAriaSort(column.key) : undefined"
            :class="[
              'sticky-header-cell py-2 text-left text-xs font-medium tracking-wider text-gray-500 dark:text-dark-300',
              getColumnLayoutClass(column),
              { 'cursor-pointer hover:bg-gray-100 dark:hover:bg-dark-700': column.sortable },
              {
                'opacity-50': draggingColumn === column.key,
                'column-drop-before': dropTarget?.key === column.key && dropTarget.side === 'before',
                'column-drop-after': dropTarget?.key === column.key && dropTarget.side === 'after'
              },
              getStickyColumnClass(column, index),
              column.class
            ]"
            @click="column.sortable && handleSort(column.key)"
            @dragover="handleColumnDragOver($event, column.key)"
            @dragenter="handleColumnDragOver($event, column.key)"
            @drop="handleColumnDrop($event, column.key)"
          >
            <div :class="['flex items-center space-x-1', getHeaderContentAlignmentClass(column)]">
              <button
                v-if="canReorder(column.key)"
                type="button"
                draggable="true"
                class="column-drag-handle"
                :data-column-key="column.key"
                :title="t('common.reorderColumn', { column: column.label })"
                :aria-label="t('common.reorderColumn', { column: column.label })"
                @click.stop
                @dragstart.stop="startColumnDrag($event, column.key)"
                @dragend="endColumnDrag"
                @keydown.left.prevent.stop="moveColumnByKeyboard(column.key, -1)"
                @keydown.right.prevent.stop="moveColumnByKeyboard(column.key, 1)"
              >
                <Icon name="grip" size="sm" class="pointer-events-none" :animate-on-hover="false" />
              </button>
              <slot
                :name="`header-${column.key}`"
                :column="column"
                :sort-key="sortKey"
                :sort-order="sortOrder"
              >
                <span>{{ column.label }}</span>
              </slot>
              <span
                v-if="column.sortable"
                class="inline-flex h-5 w-4 flex-col items-center justify-center"
                aria-hidden="true"
              >
                <Icon
                  name="chevronUp"
                  size="md"
                  :animate-on-hover="false"
                  class="h-2.5 w-2.5"
                  :class="getSortIndicatorClass(column.key, 'asc')"
                />
                <Icon
                  name="chevronDown"
                  size="md"
                  :animate-on-hover="false"
                  class="-mt-0.5 h-2.5 w-2.5"
                  :class="getSortIndicatorClass(column.key, 'desc')"
                />
              </span>
            </div>
          </th>
        </tr>
      </thead>
      <tbody class="table-body divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
        <!-- 表格按列占位，与移动端卡片共用骨架配方。 -->
        <tr v-if="loading" v-for="i in 5" :key="i">
          <td v-if="selectable" class="table-selection-cell py-3">
            <Skeleton :width="16" :height="16" class="mx-auto" />
          </td>
          <td v-for="column in orderedColumns" :key="column.key" :class="['whitespace-nowrap py-3', getColumnLayoutClass(column)]">
            <Skeleton
              :width="column.key === 'select' ? 16 : '75%'"
              :height="16"
              :class="column.key === 'select' ? 'mx-auto' : ''"
            />
          </td>
        </tr>

        <!-- Empty state -->
        <tr v-else-if="!data || data.length === 0">
          <td
            :colspan="tableColumnCount"
            :class="['py-12 text-center text-gray-500 dark:text-dark-400', getColumnLayoutClass()]"
          >
            <slot name="empty">
              <div class="flex flex-col items-center">
                <Icon
                  name="inbox"
                  size="xl"
                  class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500"
                />
                <p class="text-lg font-medium text-gray-900 dark:text-gray-100">
                  {{ t('empty.noData') }}
                </p>
              </div>
            </slot>
          </td>
        </tr>

        <!-- 数据行：大数据量按窗口渲染，小数据量全量渲染，两种模式共用行和单元格模板 -->
        <template v-else>
          <tr v-if="virtualPaddingTop > 0" aria-hidden="true">
            <td :colspan="tableColumnCount"
                :style="{ height: virtualPaddingTop + 'px', padding: 0, border: 'none' }">
            </td>
          </tr>
          <tr
            v-for="item in renderRows"
            :key="resolveRowKey(item.row, item.index)"
            :data-row-id="resolveRowKey(item.row, item.index)"
            :data-index="item.index"
            :ref="item.measure ? measureElement : undefined"
            class="hover:bg-gray-50 dark:hover:bg-dark-800"
            :class="{
              'cursor-pointer': clickableRows,
              'bg-primary-50/40 dark:bg-primary-900/10': selectable && isRowSelected(item.row, item.index)
            }"
            @click="clickableRows && emit('rowClick', item.row)"
          >
            <td v-if="selectable" class="table-selection-cell py-3">
              <input
                type="checkbox"
                class="h-4 w-4 rounded-compact border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                :checked="isRowSelected(item.row, item.index)"
                :aria-label="getRowSelectionLabel(item.row, item.index)"
                data-test="select-row"
                @click.stop
                @change="toggleRowSelection(item.row, item.index, ($event.target as HTMLInputElement).checked)"
              />
            </td>
            <td
              v-for="(column, colIndex) in orderedColumns"
              :key="column.key"
              :data-column-key="column.key"
              :class="[
                'whitespace-nowrap py-3 text-sm text-gray-900 dark:text-dark-100',
                getColumnLayoutClass(column),
                getStickyColumnClass(column, colIndex),
                column.class
              ]"
            >
              <slot :name="`cell-${column.key}`"
                    :row="item.row"
                    :value="item.row[column.key]"
                    :expanded="actionsExpanded">
                {{ column.formatter
                   ? column.formatter(item.row[column.key], item.row)
                   : item.row[column.key] }}
              </slot>
            </td>
          </tr>
          <tr v-if="virtualPaddingBottom > 0" aria-hidden="true">
            <td :colspan="tableColumnCount"
                :style="{ height: virtualPaddingBottom + 'px', padding: 0, border: 'none' }">
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useVirtualizer, observeElementRect as observeElementRectDefault } from '@tanstack/vue-virtual'
import { useI18n } from 'vue-i18n'
import type { Column } from './types'
import Icon from '@/components/icons/Icon.vue'
import Skeleton from './Skeleton.vue'
import { TABLE_DESKTOP_MEDIA_QUERY } from '@/constants/layout'
import { useTableColumnOrder } from '@/composables/useTableColumnOrder'

const { t, locale } = useI18n()

const isDesktopViewport = ref(
  typeof window === 'undefined' ? true : window.matchMedia(TABLE_DESKTOP_MEDIA_QUERY).matches
)

const emit = defineEmits<{
  sort: [key: string, order: 'asc' | 'desc']
  rowClick: [row: any]
  'update:selectedKeys': [keys: Array<string | number>]
  selectionChange: [keys: Array<string | number>]
}>()

// 表格容器引用
const tableWrapperRef = ref<HTMLElement | null>(null)
const isScrollable = ref(false)
const actionsColumnNeedsExpanding = ref(false)

// --- 虚拟滚动「整表空白」根治 ---
// 根因:本组件根 .table-wrapper 为 flex:1 / min-h-0,高度由父级 flex 链决定。@tanstack 虚拟化器
// 仅在 observeElementRect 回调里写 scrollRect;一旦该回调读到 0 高度(加载瞬间 flex 未结算,或
// 滚动中动态行高校正触发的 reflow),scrollRect 被钉死为 0 → calculateRange 返回 null → 整表空白。
// 对策(见下方 virtualizer 选项):
//   1) 覆写 observeElementRect,直接丢弃 height<=0 的读数,scrollRect 永不被钉成 0;
//   2) initialRect 给一屏兜底高度,首个有效读数到来前也有行可渲染,绝不空白。
// 兜底高度:表格区域大致 = 视口高度 - 顶栏/外边距/筛选/分页 ≈ 320px
const estimatedViewportHeight = () => {
  if (typeof window === 'undefined') return 600
  return Math.max(window.innerHeight - 320, 400)
}

// 覆写默认 observeElementRect:过滤掉 0 高度读数(根治整表空白的关键)
const observeElementRectNonZero = (
  instance: any,
  cb: (rect: { width: number; height: number }) => void
) => observeElementRectDefault(instance, (rect) => {
  if (rect.height > 0) cb(rect)
})

// 检查是否可滚动
const checkScrollable = () => {
  if (tableWrapperRef.value) {
    isScrollable.value = tableWrapperRef.value.scrollWidth > tableWrapperRef.value.clientWidth
  }
}

// 检查操作列是否需要展开
const checkActionsColumnWidth = () => {
  if (!props.expandableActions) {
    actionsColumnNeedsExpanding.value = false
    actionsExpanded.value = false
    return
  }
  if (!tableWrapperRef.value) return

  // 查找第一行的操作列单元格
  const firstActionCell = tableWrapperRef.value.querySelector('tbody tr:first-child td:last-child')
  if (!firstActionCell) return

  // 查找操作列内容的容器div
  const actionsContainer = firstActionCell.querySelector('div')
  if (!actionsContainer) return

  // 临时展开以测量完整宽度
  const wasExpanded = actionsExpanded.value
  actionsExpanded.value = true

  // 等待DOM更新
  nextTick(() => {
    // 测量所有按钮的总宽度
    const actionItems = actionsContainer.querySelectorAll('button, a, [role="button"]')
    if (actionItems.length <= 2) {
      actionsColumnNeedsExpanding.value = false
      actionsExpanded.value = wasExpanded
      return
    }

    // 计算所有按钮的总宽度（包括gap）
    let totalWidth = 0
    actionItems.forEach((item, index) => {
      totalWidth += (item as HTMLElement).offsetWidth
      if (index < actionItems.length - 1) {
        totalWidth += 4 // gap-1 = 4px
      }
    })

    // 获取单元格可用宽度（减去padding）
    const cellWidth = (firstActionCell as HTMLElement).clientWidth - 32 // 减去左右padding

    // 如果总宽度超过可用宽度，需要展开功能
    actionsColumnNeedsExpanding.value = totalWidth > cellWidth

    // 恢复原来的展开状态
    actionsExpanded.value = wasExpanded
  })
}

// 监听尺寸变化
let resizeObserver: ResizeObserver | null = null
let resizeHandler: (() => void) | null = null
let desktopViewportMediaQuery: MediaQueryList | null = null
let desktopViewportListener: ((event: MediaQueryListEvent) => void) | null = null

const detachDesktopTableTracking = () => {
  resizeObserver?.disconnect()
  resizeObserver = null
  if (resizeHandler) {
    window.removeEventListener('resize', resizeHandler)
    resizeHandler = null
  }
}

const attachDesktopTableTracking = () => {
  checkScrollable()
  checkActionsColumnWidth()
  if (tableWrapperRef.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      checkScrollable()
      checkActionsColumnWidth()
    })
    resizeObserver.observe(tableWrapperRef.value)
    // 外层尺寸不变时，异步数据仍可能撑宽表格；同时观察内容宽度才能及时恢复固定列阴影。
    const table = tableWrapperRef.value.querySelector('table')
    if (table) resizeObserver.observe(table)
  } else {
    // 降级方案：不支持 ResizeObserver 时使用 window resize
    resizeHandler = () => {
      checkScrollable()
      checkActionsColumnWidth()
    }
    window.addEventListener('resize', resizeHandler)
  }
}

onMounted(() => {
  if (typeof window !== 'undefined') {
    desktopViewportMediaQuery = window.matchMedia(TABLE_DESKTOP_MEDIA_QUERY)
    isDesktopViewport.value = desktopViewportMediaQuery.matches
    desktopViewportListener = (event: MediaQueryListEvent) => {
      isDesktopViewport.value = event.matches
    }
    if (typeof desktopViewportMediaQuery.addEventListener === 'function') {
      desktopViewportMediaQuery.addEventListener('change', desktopViewportListener)
    } else {
      desktopViewportMediaQuery.addListener(desktopViewportListener)
    }
  }
})

onUnmounted(() => {
  detachDesktopTableTracking()
  if (desktopViewportMediaQuery && desktopViewportListener) {
    if (typeof desktopViewportMediaQuery.removeEventListener === 'function') {
      desktopViewportMediaQuery.removeEventListener('change', desktopViewportListener)
    } else {
      desktopViewportMediaQuery.removeListener(desktopViewportListener)
    }
    desktopViewportListener = null
  }
  desktopViewportMediaQuery = null
})

interface Props {
  columns: Column[]
  data: any[]
  loading?: boolean
  stickyFirstColumn?: boolean
  stickyActionsColumn?: boolean
  expandableActions?: boolean
  actionsCount?: number // 操作按钮总数，用于判断是否需要展开功能
  rowKey?: string | ((row: any) => string | number)
  /**
   * Default sort configuration (only applied when there is no persisted sort state)
   */
  defaultSortKey?: string
  defaultSortOrder?: 'asc' | 'desc'
  /**
   * Persist sort state (key + order) to localStorage using this key.
   * If provided, DataTable will load the stored sort state on mount.
   */
  sortStorageKey?: string
  /** 提供稳定的表格标识以启用列拖拽，并在当前浏览器保存列顺序。 */
  columnOrderStorageKey?: string
  /**
   * Enable server-side sorting mode. When true, clicking sort headers
   * will emit 'sort' events instead of performing client-side sorting.
   */
  serverSideSort?: boolean
  /** 点击行或卡片时触发 rowClick 并显示指针光标，内部交互元素应使用 @click.stop。 */
  clickableRows?: boolean
  /** Estimated row height in px for the virtualizer (default 56) */
  estimateRowHeight?: number
  /** Number of rows to render beyond the visible area (default 5) */
  overscan?: number
  /**
   * 仅当行数超过此阈值时启用虚拟化（默认 100）。
   * 小列表全量渲染，可变行高按实际内容计算。
   */
  virtualizeThreshold?: number
  /** 启用受控行选择；应提供稳定的行键。 */
  selectable?: boolean
  /** 已选行键；当前页以外的键会继续保留。 */
  selectedKeys?: Array<string | number>
  /** 行选择复选框的无障碍标签。 */
  selectionLabel?: string | ((row: any) => string)
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
  stickyFirstColumn: true,
  stickyActionsColumn: true,
  expandableActions: true,
  defaultSortOrder: 'asc',
  serverSideSort: false,
  selectable: false,
  selectedKeys: () => []
})

const sortKey = ref<string>('')
const sortOrder = ref<'asc' | 'desc'>('asc')
const actionsExpanded = ref(false)

const { orderedColumns, movableColumns, canReorder, moveColumn } = useTableColumnOrder(
  () => props.columns,
  computed(() => props.columnOrderStorageKey)
)
const draggingColumn = ref<string | null>(null)
const dropTarget = ref<{ key: string; side: 'before' | 'after' } | null>(null)
let dragScrollFrame: number | null = null
let dragScrollSpeed = 0

const endColumnDrag = () => {
  draggingColumn.value = null
  dropTarget.value = null
  dragScrollSpeed = 0
  if (dragScrollFrame !== null) cancelAnimationFrame(dragScrollFrame)
  dragScrollFrame = null
}

const startColumnDrag = (event: DragEvent, key: string) => {
  if (!canReorder(key) || !event.dataTransfer) {
    event.preventDefault()
    return
  }
  draggingColumn.value = key
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', key)
}

// 宽表格拖到边缘时继续滚动，便于把列移到当前视口以外。
const scrollDuringColumnDrag = () => {
  if (!draggingColumn.value || !tableWrapperRef.value || dragScrollSpeed === 0) {
    dragScrollFrame = null
    return
  }
  tableWrapperRef.value.scrollLeft += dragScrollSpeed
  dragScrollFrame = requestAnimationFrame(scrollDuringColumnDrag)
}

const handleTableDragOver = (event: DragEvent) => {
  if (!draggingColumn.value || !tableWrapperRef.value) return
  event.preventDefault()
  const rect = tableWrapperRef.value.getBoundingClientRect()
  dragScrollSpeed = event.clientX < rect.left + 48 ? -12 : event.clientX > rect.right - 48 ? 12 : 0
  if (dragScrollFrame === null && dragScrollSpeed !== 0) {
    dragScrollFrame = requestAnimationFrame(scrollDuringColumnDrag)
  }
}

const handleTableDragLeave = (event: DragEvent) => {
  if (event.relatedTarget instanceof Node && tableWrapperRef.value?.contains(event.relatedTarget)) return
  dragScrollSpeed = 0
  dropTarget.value = null
}

const handleColumnDragOver = (event: DragEvent, key: string) => {
  if (!draggingColumn.value || !canReorder(key) || key === draggingColumn.value) {
    dropTarget.value = null
    return
  }
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  dropTarget.value = { key, side: event.clientX < rect.left + rect.width / 2 ? 'before' : 'after' }
}

const handleColumnDrop = (event: DragEvent, key: string) => {
  if (!draggingColumn.value) return
  event.preventDefault()
  if (dropTarget.value?.key === key) {
    moveColumn(draggingColumn.value, key, dropTarget.value.side)
  }
  endColumnDrag()
}

const moveColumnByKeyboard = async (key: string, direction: number) => {
  const index = movableColumns.value.findIndex(column => column.key === key)
  const target = movableColumns.value[index + direction]
  if (!target || !moveColumn(key, target.key, direction < 0 ? 'before' : 'after')) return
  await nextTick()
  // 移动 DOM 节点后恢复手柄焦点，允许连续用方向键调整。
  const handles = tableWrapperRef.value?.querySelectorAll<HTMLButtonElement>('.column-drag-handle')
  Array.from(handles ?? []).find(handle => handle.dataset.columnKey === key)?.focus()
}

watch([() => props.columnOrderStorageKey, isDesktopViewport], endColumnDrag)
onUnmounted(endColumnDrag)

type PersistedSortState = {
  key: string
  order: 'asc' | 'desc'
}

const collator = computed(() => new Intl.Collator(locale?.value, {
  numeric: true,
  sensitivity: 'base'
}))

const getSortableKeys = () => {
  const keys = new Set<string>()
  for (const col of props.columns) {
    if (col.sortable) keys.add(col.key)
  }
  return keys
}

const normalizeSortKey = (candidate: string) => {
  if (!candidate) return ''
  const sortableKeys = getSortableKeys()
  return sortableKeys.has(candidate) ? candidate : ''
}

const normalizeSortOrder = (candidate: any): 'asc' | 'desc' => {
  return candidate === 'desc' ? 'desc' : 'asc'
}

const readPersistedSortState = (): PersistedSortState | null => {
  if (!props.sortStorageKey) return null
  try {
    const raw = localStorage.getItem(props.sortStorageKey)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<PersistedSortState>
    const key = normalizeSortKey(typeof parsed.key === 'string' ? parsed.key : '')
    if (!key) return null
    return { key, order: normalizeSortOrder(parsed.order) }
  } catch (e) {
    console.error('[DataTable] Failed to read persisted sort state:', e)
    return null
  }
}

const writePersistedSortState = (state: PersistedSortState) => {
  if (!props.sortStorageKey) return
  try {
    localStorage.setItem(props.sortStorageKey, JSON.stringify(state))
  } catch (e) {
    console.error('[DataTable] Failed to persist sort state:', e)
  }
}

const resolveInitialSortState = (): PersistedSortState | null => {
  const persisted = readPersistedSortState()
  if (persisted) return persisted

  const key = normalizeSortKey(props.defaultSortKey || '')
  if (!key) return null
  return { key, order: normalizeSortOrder(props.defaultSortOrder) }
}

const applySortState = (state: PersistedSortState | null) => {
  if (!state) return
  sortKey.value = state.key
  sortOrder.value = state.order
}

// 同时展示升序/降序箭头，并只高亮当前生效方向。
const getSortIndicatorClass = (key: string, order: 'asc' | 'desc') => {
  return sortKey.value === key && sortOrder.value === order
    ? 'text-primary-600 dark:text-primary-400'
    : 'text-gray-300 transition-colors dark:text-dark-500'
}

// 给可排序表头同步无障碍排序状态，方便读屏和测试识别。
const getColumnAriaSort = (key: string) => {
  if (sortKey.value !== key) return 'none'
  return sortOrder.value === 'asc' ? 'ascending' : 'descending'
}

const getHeaderContentAlignmentClass = (column: Column) => {
  if (column.key === 'select') return 'justify-center'
  const className = column.class || ''
  if (className.includes('text-center')) return 'justify-center'
  if (className.includes('text-right')) return 'justify-end'
  return 'justify-start'
}

const isNullishOrEmpty = (value: any) => value === null || value === undefined || value === ''

const toFiniteNumberOrNull = (value: any): number | null => {
  if (typeof value === 'number') return Number.isFinite(value) ? value : null
  if (typeof value === 'boolean') return value ? 1 : 0
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) return null
    const n = Number(trimmed)
    return Number.isFinite(n) ? n : null
  }
  return null
}

const toSortableString = (value: any): string => {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (value instanceof Date) return value.toISOString()
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

const compareSortValues = (a: any, b: any): number => {
  const aEmpty = isNullishOrEmpty(a)
  const bEmpty = isNullishOrEmpty(b)
  if (aEmpty && bEmpty) return 0
  if (aEmpty) return 1
  if (bEmpty) return -1

  const aNum = toFiniteNumberOrNull(a)
  const bNum = toFiniteNumberOrNull(b)
  if (aNum !== null && bNum !== null) {
    if (aNum === bNum) return 0
    return aNum < bNum ? -1 : 1
  }

  const aStr = toSortableString(a)
  const bStr = toSortableString(b)
  const res = collator.value.compare(aStr, bStr)
  if (res === 0) return 0
  return res < 0 ? -1 : 1
}
const resolveStableRowKey = (row: any): string | number | undefined => {
  if (typeof props.rowKey === 'function') {
    const key = props.rowKey(row)
    return key ?? undefined
  }
  if (typeof props.rowKey === 'string' && props.rowKey) {
    const key = row?.[props.rowKey]
    return key ?? undefined
  }
  const key = row?.id
  return key ?? undefined
}

const resolveRowKey = (row: any, index: number) => resolveStableRowKey(row) ?? index

const dataColumns = computed(() => orderedColumns.value.filter((column) => column.key !== 'actions'))
const columnsSignature = computed(() =>
  orderedColumns.value.map((column) => `${column.key}:${column.sortable ? '1' : '0'}`).join('|')
)

watch(
  isDesktopViewport,
  async (isDesktop) => {
    detachDesktopTableTracking()
    if (!isDesktop) return
    await nextTick()
    attachDesktopTableTracking()
  },
  { immediate: true, flush: 'post' }
)

// 数据/列变化时重新检查滚动状态
// 注意：不能监听 actionsExpanded，因为 checkActionsColumnWidth 会临时修改它，会导致无限循环
watch(
  [() => props.data.length, columnsSignature],
  async () => {
    await nextTick()
    checkScrollable()
    checkActionsColumnWidth()
  },
  { flush: 'post' }
)

// 单独监听展开状态变化，只更新滚动状态
watch(actionsExpanded, async () => {
  await nextTick()
  checkScrollable()
})

const handleSort = (key: string) => {
  let newOrder: 'asc' | 'desc' = 'asc'
  if (sortKey.value === key) {
    newOrder = sortOrder.value === 'asc' ? 'desc' : 'asc'
  }

  if (props.serverSideSort) {
    // Server-side sort mode: emit event and update internal state for UI feedback
    sortKey.value = key
    sortOrder.value = newOrder
    emit('sort', key, newOrder)
  } else {
    // Client-side sort mode: just update internal state
    sortKey.value = key
    sortOrder.value = newOrder
  }
}

const sortedData = computed(() => {
  // Server-side sort mode: return data as-is (server handles sorting)
  if (props.serverSideSort || !sortKey.value || !props.data) return props.data

  const key = sortKey.value
  const order = sortOrder.value

  // Stable sort (tie-break with original index) to avoid jitter when values are equal.
  return props.data
    .map((row, index) => ({ row, index }))
    .sort((a, b) => {
      const cmp = compareSortValues(a.row?.[key], b.row?.[key])
      if (cmp !== 0) return order === 'asc' ? cmp : -cmp
      return a.index - b.index
    })
    .map(item => item.row)
})

const tableColumnCount = computed(() => props.columns.length + (props.selectable ? 1 : 0))
const selectedKeySet = computed(() => new Set(props.selectedKeys))
const visibleRowKeys = computed(() =>
  (sortedData.value ?? []).map((row, index) => resolveRowKey(row, index))
)
const allVisibleSelected = computed(() =>
  visibleRowKeys.value.length > 0
  && visibleRowKeys.value.every((key) => selectedKeySet.value.has(key))
)
const someVisibleSelected = computed(() => {
  if (allVisibleSelected.value) return false
  return visibleRowKeys.value.some((key) => selectedKeySet.value.has(key))
})

const emitSelection = (next: Set<string | number>) => {
  const keys = Array.from(next)
  emit('update:selectedKeys', keys)
  emit('selectionChange', keys)
}

const isRowSelected = (row: any, index: number) =>
  selectedKeySet.value.has(resolveRowKey(row, index))

const getRowSelectionLabel = (row: any, index: number) => {
  if (typeof props.selectionLabel === 'function') return props.selectionLabel(row)
  if (props.selectionLabel) return props.selectionLabel
  return `${t('common.selectOption')} ${resolveRowKey(row, index)}`
}

const toggleRowSelection = (row: any, index: number, checked: boolean) => {
  const next = new Set(props.selectedKeys)
  const key = resolveRowKey(row, index)
  if (checked) next.add(key)
  else next.delete(key)
  emitSelection(next)
}

const toggleAllVisible = (checked: boolean) => {
  const next = new Set(props.selectedKeys)
  for (const key of visibleRowKeys.value) {
    if (checked) next.add(key)
    else next.delete(key)
  }
  emitSelection(next)
}

// --- Virtual scrolling ---
// 是否启用虚拟化:仅桌面端且行数超过阈值时开启。小列表全量渲染,彻底绕开虚拟器的
// 估算/测量/滚动补偿链路,消除可变行高导致的滚动抖动。
const shouldVirtualize = computed(() =>
  isDesktopViewport.value && (sortedData.value?.length ?? 0) > (props.virtualizeThreshold ?? 100)
)

const rowVirtualizer = useVirtualizer(computed(() => ({
  count: shouldVirtualize.value ? (sortedData.value?.length ?? 0) : 0,
  getScrollElement: () => tableWrapperRef.value,
  // itemSizeCache 使用与模板 :key 相同的行主键。
  // 排序、筛选和跨虚拟化阈值切换时，已测行高仍对应同一行，减少高度校正抖动。
  getItemKey: (index: number) => {
    const row = sortedData.value?.[index]
    return row != null ? resolveRowKey(row, index) : index
  },
  estimateSize: () => props.estimateRowHeight ?? 56,
  overscan: props.overscan ?? 5,
  // 首次测得有效高度前，按一屏高度渲染。
  initialRect: { width: 0, height: estimatedViewportHeight() },
  // 关键:过滤 0 高度读数,杜绝 scrollRect 被钉成 0 → calculateRange 返回 null → 整表空白
  observeElementRect: observeElementRectNonZero,
  // ResizeObserver 的测量回调合并到 rAF，滚动时按帧处理重排。
  useAnimationFrameWithResizeObserver: true,
})))

const virtualItems = computed(() => rowVirtualizer.value.getVirtualItems())

const virtualPaddingTop = computed(() => {
  const items = virtualItems.value
  return items.length > 0 ? items[0].start : 0
})

const virtualPaddingBottom = computed(() => {
  const items = virtualItems.value
  if (items.length === 0) return 0
  return rowVirtualizer.value.getTotalSize() - items[items.length - 1].end
})

const measureElement = (el: any) => {
  if (el) {
    rowVirtualizer.value.measureElement(el as Element)
  }
}

type RowIdentityToken = string | number | object | symbol

const rowIdentityKeys = computed<RowIdentityToken[]>(() =>
  (sortedData.value ?? []).map((row) => {
    const stableKey = resolveStableRowKey(row)
    if (stableKey !== undefined) return stableKey

    // 对象引用在单纯排序时保持不变，但会随分页或筛选结果变化。
    // 原始值行没有稳定标识，因此需要保守地让缓存失效。
    return row !== null && typeof row === 'object' ? row : Symbol('unstable-row')
  })
)

const hasSameRowIdentitySet = (
  current: RowIdentityToken[],
  previous: RowIdentityToken[]
) => {
  if (current.length !== previous.length) return false
  const currentKeys = new Set(current)
  const previousKeys = new Set(previous)
  // 重复主键会让行与缓存的归属不明确，即使去重后的集合没有变化也必须清理，
  // 例如 [1, 1, 2] 变为 [1, 2, 2]。
  if (currentKeys.size !== current.length || previousKeys.size !== previous.length) return false
  return [...currentKeys].every(key => previousKeys.has(key))
}

watch(
  rowIdentityKeys,
  (current, previous) => {
    if (hasSameRowIdentitySet(current, previous)) return

    // 虚拟器会跨选项更新持有缓存。分页或筛选结果变化时释放已脱离的行与高度，
    // 单纯排序则继续复用缓存。
    rowVirtualizer.value.measureElement(null)
    rowVirtualizer.value.measure()
  },
  { flush: 'post' }
)

// 统一的渲染行列表:虚拟化开启时只取窗口内的行(需 measure 交给虚拟器测量),
// 关闭时取全部行(无需测量)。模板据此渲染,两种模式共用同一套单元格结构。
const renderRows = computed<Array<{ index: number; row: any; measure: boolean }>>(() => {
  const data = sortedData.value ?? []
  if (shouldVirtualize.value) {
    return virtualItems.value.map(vr => ({ index: vr.index, row: data[vr.index], measure: true }))
  }
  return data.map((row, index) => ({ index, row, measure: false }))
})

const hasActionsColumn = computed(() => {
  return props.columns.some(column => column.key === 'actions')
})

const hasSelectColumn = computed(() => {
  return props.columns.length > 0 && props.columns[0].key === 'select'
})

// 生成固定列的 CSS 类
const getStickyColumnClass = (column: Column, index: number) => {
  const classes: string[] = []

  // 选择列随横向滚动移出，首个数据列在到达左边缘后固定。
  const firstDataColumnIndex = hasSelectColumn.value ? 1 : 0
  if (props.stickyFirstColumn && index === firstDataColumnIndex) {
    classes.push('sticky-col sticky-col-left')
  }

  // 操作列固定（最后一列）
  if (props.stickyActionsColumn && column.key === 'actions') {
    classes.push('sticky-col sticky-col-right')
  }

  return classes.join(' ')
}

// 选择列单独控制宽度，其余列按列数调整内边距。
const getColumnLayoutClass = (column?: Column) => {
  // 自定义选择列与内置行选择使用相同的宽度和居中布局。
  if (column?.key === 'select') return 'table-selection-cell'
  const columnCount = props.columns.length

  // 列数越多，内边距越小
  if (columnCount >= 10) {
    return 'px-2' // 8px
  } else if (columnCount >= 7) {
    return 'px-3' // 12px
  } else if (columnCount >= 5) {
    return 'px-4' // 16px
  } else {
    return 'px-6' // 24px (原始值)
  }
}

// Init + keep persisted sort state consistent with current columns
const didInitSort = ref(false)

onMounted(() => {
  const initial = resolveInitialSortState()
  applySortState(initial)
  didInitSort.value = true
})

watch(
  columnsSignature,
  () => {
    // If current sort key is no longer sortable/visible, fall back to default/persisted.
    const normalized = normalizeSortKey(sortKey.value)
    if (!sortKey.value) {
      const initial = resolveInitialSortState()
      applySortState(initial)
      return
    }

    if (!normalized) {
      const fallback = resolveInitialSortState()
      if (fallback) {
        applySortState(fallback)
      } else {
        sortKey.value = ''
        sortOrder.value = 'asc'
      }
    }
  },
  { flush: 'post' }
)

watch(
  [sortKey, sortOrder],
  ([nextKey, nextOrder]) => {
    if (!didInitSort.value) return
    if (!props.sortStorageKey) return
    const key = normalizeSortKey(nextKey)
    if (!key) return
    writePersistedSortState({ key, order: normalizeSortOrder(nextOrder) })
  },
  { flush: 'post' }
)

defineExpose({
  virtualizer: rowVirtualizer,
  shouldVirtualize,
  sortedData,
  resolveRowKey,
  tableWrapperEl: tableWrapperRef,
})
</script>

<style scoped>
/* 表格横向滚动 */
.table-wrapper {
  --sticky-boundary-line-color: rgb(228 228 231);
  position: relative;
  overflow-x: auto;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
  isolation: isolate;
}

.dark .table-wrapper {
  --sticky-boundary-line-color: theme('borderColor.dark.600');
}

/* 选择列使用表格自身的单元格样式。 */
.table-wrapper .table-selection-cell {
  @apply w-11 min-w-11 px-3 text-center;
}

/* 拖拽手柄与排序指示器同高，保持表头密度。 */
.column-drag-handle {
  @apply inline-flex h-5 w-4 shrink-0 cursor-grab items-center justify-center rounded-compact text-gray-400 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 active:cursor-grabbing dark:text-dark-400 dark:hover:text-primary-400;
}

.column-drop-before {
  box-shadow: inset 3px 0 0 theme('colors.primary.500');
}

.column-drop-after {
  box-shadow: inset -3px 0 0 theme('colors.primary.500');
}

/* 表头容器，确保在滚动时覆盖表体内容 */
.table-wrapper .table-header {
  position: sticky;
  top: 0;
  z-index: 200; /* check-ui-allow: 表格内部局部堆叠上下文(固定列/表头),不入全局阶梯 */
  background-color: rgb(249 250 251);
}

.dark .table-wrapper .table-header {
  background-color: rgb(15 15 16);
}

/* 表体保持在表头下方 */
.table-body {
  position: relative;
  z-index: 0; /* check-ui-allow: 局部堆叠 */
}

/* 所有表头单元格固定在顶部 */
.sticky-header-cell {
  position: sticky;
  top: 0;
  z-index: 210; /* 必须高于所有表体内容 */ /* check-ui-allow: 局部堆叠 */
  background-color: rgb(249 250 251);
}

.dark .sticky-header-cell {
  background-color: rgb(15 15 16);
}

/* Sticky 列基础样式 */
.sticky-col {
  position: sticky;
  z-index: 20; /* 表体固定列 */ /* check-ui-allow: 局部堆叠 */
}

/* 首个数据列贴住左边缘，选择列不占用固定区域。 */
.sticky-col-left {
  left: 0;
}

/* 操作列固定 */
.sticky-col-right {
  right: 0;
}

/* 表头 sticky 列 - 需要比普通表头单元格更高的 z-index */
.sticky-header-cell.sticky-col {
  z-index: 220; /* 高于普通表头单元格和表体固定列 */ /* check-ui-allow: 局部堆叠 */
}

/* 表体 sticky 列背景 */
tbody .sticky-col {
  background-color: white;
}

.dark tbody .sticky-col {
  background-color: rgb(15 15 16);
}

/* hover 状态保持 */
tbody tr:hover .sticky-col {
  background-color: rgb(249 250 251);
}

.dark tbody tr:hover .sticky-col {
  background-color: rgb(23 23 26);
}

/* 固定列之间使用细线分隔。 */
.sticky-boundary-line.is-scrollable .sticky-col-left::after {
  content: '';
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 1px;
  transform: translateX(100%);
  background-color: var(--sticky-boundary-line-color);
  pointer-events: none;
}

.sticky-boundary-line.is-scrollable .sticky-col-right::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  width: 1px;
  transform: translateX(-100%);
  background-color: var(--sticky-boundary-line-color);
  pointer-events: none;
}
</style>

<style>
/* 常驻显示滚动条，提示表格可以横向滚动。 */
.table-wrapper {
  scrollbar-width: auto !important;
}

.table-wrapper::-webkit-scrollbar {
  height: 12px !important;
  width: 12px !important;
  display: block !important;
  background-color: transparent !important;
}

.table-wrapper::-webkit-scrollbar-track {
  background-color: rgba(0, 0, 0, 0.03) !important;
  border-radius: 9999px !important;
  margin: 0 4px !important;
}
.dark .table-wrapper::-webkit-scrollbar-track {
  background-color: rgba(255, 255, 255, 0.05) !important;
}

.table-wrapper::-webkit-scrollbar-thumb {
  background-color: rgba(107, 114, 128, 0.75) !important; 
  border-radius: 9999px !important;
  border: 2px solid transparent !important;
  background-clip: padding-box !important;
  -webkit-appearance: none !important;
}
.table-wrapper::-webkit-scrollbar-thumb:hover {
  background-color: rgba(75, 85, 99, 0.9) !important;
}

.dark .table-wrapper::-webkit-scrollbar-thumb {
  background-color: rgba(161, 161, 170, 0.45) !important;
}
.dark .table-wrapper::-webkit-scrollbar-thumb:hover {
  background-color: rgba(161, 161, 170, 0.65) !important;
}

@supports (-moz-appearance:none) {
  .table-wrapper {
    scrollbar-width: thin !important;
    scrollbar-color: rgba(156, 163, 175, 0.5) rgba(0, 0, 0, 0.03) !important;
  }
  .dark .table-wrapper {
    scrollbar-color: rgba(75, 85, 99, 0.5) rgba(255, 255, 255, 0.05) !important;
  }
}
</style>
