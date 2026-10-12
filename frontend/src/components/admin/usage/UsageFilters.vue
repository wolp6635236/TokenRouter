<template>
  <div :class="flat ? 'p-4' : 'card p-6'">
    <div class="space-y-4">
      <div class="flex items-center justify-between gap-2">
        <!-- 未设置的条件可能是 undefined，统一按 null 显示，才能匹配各下拉框的“全部”选项 -->
        <FilterDropdown :active-count="activeFilterCount" :columns="3" keep-mounted @reset="resetPanelFilters">
          <!-- 请求 ID 生效后查询不限日期范围，用户排行不按它筛选 -->
          <RequestIdFilterField v-if="mode !== 'ranking'" v-model="filters.request_id" @search="emitChange" />
          <FilterField v-if="mode === 'usage'" :label="t('admin.usage.teamFilter')">
            <Select :model-value="filters.team_id ?? null" @update:model-value="filters.team_id = $event" :options="teamOptions" searchable @change="emitChange" />
          </FilterField>

          <!-- 用户搜索 -->
          <FilterField :label="t('admin.usage.userFilter')" :value-text="filters.user_id ? userKeyword || `#${filters.user_id}` : ''" @clear="clearUser">
            <div ref="userSearchRef" class="usage-filter-dropdown relative">
              <input
                v-model="userKeyword"
                type="text"
                class="input pr-8"
                :placeholder="t('admin.usage.searchUserPlaceholder')"
                @input="debounceUserSearch"
                @focus="showUserDropdown = true"
              />
              <button
                v-if="filters.user_id"
                type="button"
                @click="clearUser"
                class="absolute right-2 top-0 flex h-9 items-center text-gray-400 hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200"
                aria-label="Clear user filter"
              >
                <Icon name="x" size="sm" />
              </button>
              <MotionTransition name="dropdown-fade">
                <div
                  v-if="showUserDropdown && (userResults.length > 0 || userKeyword)" :inert="!(showUserDropdown && (userResults.length > 0 || userKeyword)) || undefined"
                  class="dropdown z-50 mt-1 max-h-menu-sm w-full overflow-auto py-0"
                >
                  <button
                    v-for="u in userResults"
                    :key="u.id"
                    type="button"
                    @click="selectUser(u)"
                    class="dropdown-item"
                  >
                    <span>{{ u.email }}<span v-if="u.deleted" class="ml-1 text-xs text-gray-400">（{{ t('admin.usage.userDeletedBadge') }}）</span></span>
                    <span class="text-xs text-gray-400">#{{ u.id }}</span>
                  </button>
                </div>
              </MotionTransition>
            </div>
          </FilterField>

          <!-- API 密钥搜索 -->
          <FilterField :label="t('usage.apiKeyFilter')" :value-text="filters.api_key_id ? apiKeyKeyword || `#${filters.api_key_id}` : ''" @clear="onClearApiKey">
            <div ref="apiKeySearchRef" class="usage-filter-dropdown relative">
              <input
                v-model="apiKeyKeyword"
                type="text"
                class="input pr-8"
                :placeholder="t('admin.usage.searchApiKeyPlaceholder')"
                @input="debounceApiKeySearch"
                @focus="onApiKeyFocus"
              />
              <button
                v-if="filters.api_key_id"
                type="button"
                @click="onClearApiKey"
                class="absolute right-2 top-0 flex h-9 items-center text-gray-400 hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200"
                aria-label="Clear API key filter"
              >
                <Icon name="x" size="sm" />
              </button>
              <MotionTransition name="dropdown-fade">
                <div
                  v-if="showApiKeyDropdown && apiKeyResults.length > 0" :inert="!(showApiKeyDropdown && apiKeyResults.length > 0) || undefined"
                  class="dropdown z-50 mt-1 max-h-menu-sm w-full overflow-auto py-0"
                >
                  <button
                    v-for="k in apiKeyResults"
                    :key="k.id"
                    type="button"
                    @click="selectApiKey(k)"
                    class="dropdown-item"
                  >
                    <span class="truncate">{{ k.name || `#${k.id}` }}</span>
                    <span class="text-xs text-gray-400">#{{ k.id }}</span>
                  </button>
                </div>
              </MotionTransition>
            </div>
          </FilterField>

          <!-- 模型筛选 -->
          <FilterField :label="t('usage.model')">
            <Select :model-value="filters.model ?? null" @update:model-value="filters.model = $event" :options="modelOptions" searchable @change="emitChange" />
          </FilterField>

          <!-- 提供商搜索 -->
          <FilterField :label="t('admin.usage.provider')" :value-text="filters.provider_id ? providerKeyword || `#${filters.provider_id}` : ''" @clear="clearProvider">
            <div ref="providerSearchRef" class="usage-filter-dropdown relative">
              <input
                v-model="providerKeyword"
                type="text"
                class="input pr-8"
                :placeholder="t('admin.usage.searchProviderPlaceholder')"
                @input="debounceProviderSearch"
                @focus="showProviderDropdown = true"
              />
              <button
                v-if="filters.provider_id"
                type="button"
                @click="clearProvider"
                class="absolute right-2 top-0 flex h-9 items-center text-gray-400 hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200"
                aria-label="Clear provider filter"
              >
                <Icon name="x" size="sm" />
              </button>
              <MotionTransition name="dropdown-fade">
                <div
                  v-if="showProviderDropdown && (providerResults.length > 0 || providerKeyword)" :inert="!(showProviderDropdown && (providerResults.length > 0 || providerKeyword)) || undefined"
                  class="dropdown z-50 mt-1 max-h-menu-sm w-full overflow-auto py-0"
                >
                  <button
                    v-for="a in providerResults"
                    :key="a.id"
                    type="button"
                    @click="selectProvider(a)"
                    class="dropdown-item"
                  >
                    <span class="truncate">{{ a.name }}</span>
                    <span class="text-xs text-gray-400">#{{ a.id }}</span>
                  </button>
                </div>
              </MotionTransition>
            </div>
          </FilterField>

          <!-- 请求类型筛选，仅用于用量列表。 -->
          <FilterField v-if="mode !== 'errors'" :label="t('usage.type')">
            <Select :model-value="filters.request_type ?? null" @update:model-value="filters.request_type = $event" :options="requestTypeOptions" @change="emitChange" />
          </FilterField>

          <!-- 计费类型筛选，仅用于用量列表。 -->
          <FilterField v-if="mode !== 'errors'" :label="t('admin.usage.billingType')">
            <Select :model-value="filters.billing_type ?? null" @update:model-value="filters.billing_type = $event" :options="billingTypeOptions" @change="emitChange" />
          </FilterField>

          <!-- 计费模式筛选适用于用量列表，用户排行按其接口支持的维度筛选。 -->
          <FilterField v-if="mode === 'usage'" :label="t('admin.usage.billingMode')">
            <Select :model-value="filters.billing_mode ?? null" @update:model-value="filters.billing_mode = $event" :options="billingModeOptions" @change="emitChange" />
          </FilterField>

          <!-- 原生 compaction 筛选仅适用于用量记录。 -->
          <FilterField v-if="mode !== 'errors'" :label="t('usage.compactionFilter')">
            <Select :model-value="filters.native_compaction_v2 ?? null" @update:model-value="filters.native_compaction_v2 = $event" :options="compactionOptions" @change="emitChange" />
          </FilterField>

          <!-- 错误阶段筛选，仅用于错误列表。 -->
          <FilterField v-if="mode === 'errors'" :label="t('admin.ops.errorLog.type')">
            <Select :model-value="filters.error_phase ?? null" @update:model-value="filters.error_phase = $event" :options="errorPhaseOptions" @change="emitChange" />
          </FilterField>

          <!-- 错误分类筛选，仅用于错误列表。 -->
          <FilterField v-if="mode === 'errors'" :label="t('usage.errors.category')">
            <Select :model-value="filters.error_category ?? null" @update:model-value="filters.error_category = $event" :options="errorCategoryOptions" @change="emitChange" />
          </FilterField>

          <!-- 状态码筛选，仅用于错误列表。 -->
          <FilterField v-if="mode === 'errors'" :label="t('admin.ops.errorLog.status')">
            <Select :model-value="filters.status_code ?? null" @update:model-value="filters.status_code = $event" :options="statusCodeOptions" @change="emitChange" />
          </FilterField>

          <!-- 分组筛选 -->
          <FilterField :label="t('admin.usage.group')">
            <Select :model-value="filters.group_id ?? null" @update:model-value="filters.group_id = $event" :options="groupOptions" searchable @change="emitChange" />
          </FilterField>
        </FilterDropdown>

        <div v-if="showActions" class="flex flex-wrap items-center justify-end gap-2">
          <button type="button" @click="$emit('refresh')" class="btn btn-secondary btn-icon" :title="t('common.refresh')">
            <Icon name="refresh" size="sm" />
          </button>
          <slot name="after-reset" />
          <template v-if="mode === 'usage'">
            <button type="button" @click="$emit('cleanup')" class="btn btn-danger whitespace-nowrap px-3 sm:px-4">
              {{ t('admin.usage.cleanup.button') }}
            </button>
            <button type="button" @click="$emit('export')" :disabled="exporting" class="btn btn-primary whitespace-nowrap px-3 sm:px-4">
              {{ t('usage.exportExcel') }}
            </button>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import RequestIdFilterField from '@/components/common/RequestIdFilterField.vue'
import MotionTransition from '@/components/common/MotionTransition.vue'
import { ref, onMounted, onUnmounted, toRef, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import FilterDropdown from '@/components/common/FilterDropdown.vue'
import FilterField from '@/components/common/FilterField.vue'
import Icon from '@/components/icons/Icon.vue'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import { SEARCH_DEBOUNCE_MS } from '@/constants/ui'
import type { SimpleApiKey, SimpleUser } from '@/api/admin/usage'

type ModelValue = Record<string, any>

interface Props {
  modelValue: ModelValue
  exporting: boolean
  startDate: string
  endDate: string
  showActions?: boolean
  modelOptions?: string[]
  /**
   * errors 模式:隐藏用量专属字段/按钮,显示错误类型+状态码(错误请求 tab 用)
   * ranking 模式:同 usage 但隐藏计费模式筛选与清理/导出按钮(用户排行 tab 用)
   */
  mode?: 'usage' | 'errors' | 'ranking'
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  showActions: true,
  mode: 'usage',
  flat: false
})
const emit = defineEmits([
  'update:modelValue',
  'change',
  'refresh',
  'reset',
  'export',
  'cleanup'
])

const { t } = useI18n()
const filters = toRef(props, 'modelValue')

const userSearchRef = ref<HTMLElement | null>(null)
const apiKeySearchRef = ref<HTMLElement | null>(null)
const providerSearchRef = ref<HTMLElement | null>(null)

const userKeyword = ref('')
const userResults = ref<SimpleUser[]>([])
const showUserDropdown = ref(false)
let userSearchTimeout: ReturnType<typeof setTimeout> | null = null
let userSearchSequence = 0

const apiKeyKeyword = ref('')
const apiKeyResults = ref<SimpleApiKey[]>([])
const showApiKeyDropdown = ref(false)
let apiKeySearchTimeout: ReturnType<typeof setTimeout> | null = null

interface SimpleProvider {
  id: number
  name: string
}
const providerKeyword = ref('')
const providerResults = ref<SimpleProvider[]>([])
const showProviderDropdown = ref(false)
let providerSearchTimeout: ReturnType<typeof setTimeout> | null = null

const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...(props.modelOptions ?? []).map((m) => ({ value: m, label: m })),
])
const groupOptions = ref<SelectOption[]>([{ value: null, label: t('admin.usage.allGroups') }])
const teamOptions = ref<SelectOption[]>([{ value: null, label: t('admin.usage.allTeams') }])
const activeFilterCount = computed(() => Object.entries(filters.value).filter(([key, value]) => !['start_date', 'end_date'].includes(key) && !(key === 'request_id' && props.mode === 'ranking') && value !== null && value !== undefined && String(value) !== '').length)

const requestTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
  { value: 'cyber', label: t('usage.cyber') }
])

const billingTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') }
])

// 错误类型对应后端 phase 参数，与错误表的“类型”徽章使用相同分类。
const errorPhaseOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'upstream', label: t('admin.ops.errorLog.typeUpstream') },
  { value: 'provider_auth', label: t('admin.ops.errorLog.typeProviderAuth') },
  { value: 'request', label: t('admin.ops.errorLog.typeRequest') },
  { value: 'auth', label: t('admin.ops.errorLog.typeAuth') },
  { value: 'routing', label: t('admin.ops.errorLog.typeRouting') },
  { value: 'internal', label: t('admin.ops.errorLog.typeInternal') },
])

// 分类码同用户端 /usage 错误筛选;"other" 无法反查为过滤条件,刻意不列
const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

const statusCodeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const billingModeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') }
])

const compactionOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allCompactions') },
  { value: true, label: t('usage.nativeCompactionV2') },
  { value: false, label: t('usage.legacyCompaction') },
])

const emitChange = () => emit('change')

const clearPendingUserSearch = () => {
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
    userSearchTimeout = null
  }
  userSearchSequence += 1
}

const debounceUserSearch = () => {
  clearPendingUserSearch()
  const query = userKeyword.value.trim()
  if (!query) {
    userResults.value = []
    return
  }

  const sequence = userSearchSequence
  userSearchTimeout = setTimeout(async () => {
    userSearchTimeout = null
    try {
      const results = await adminAPI.usage.searchUsers(query)
      if (sequence === userSearchSequence) {
        userResults.value = results.sort((a, b) => Number(a.deleted) - Number(b.deleted))
      }
    } catch {
      if (sequence === userSearchSequence) {
        userResults.value = []
      }
    }
  }, SEARCH_DEBOUNCE_MS)
}

const debounceApiKeySearch = () => {
  if (apiKeySearchTimeout) clearTimeout(apiKeySearchTimeout)
  apiKeySearchTimeout = setTimeout(async () => {
    try {
      apiKeyResults.value = await adminAPI.usage.searchApiKeys(
        filters.value.user_id,
        apiKeyKeyword.value || ''
      )
    } catch {
      apiKeyResults.value = []
    }
  }, SEARCH_DEBOUNCE_MS)
}

const selectUser = async (u: SimpleUser) => {
  clearPendingUserSearch()
  userKeyword.value = u.email
  showUserDropdown.value = false
  filters.value.user_id = u.id
  clearApiKey()

  // Auto-load API keys for this user
  try {
    apiKeyResults.value = await adminAPI.usage.searchApiKeys(u.id, '')
  } catch {
    apiKeyResults.value = []
  }

  emitChange()
}

const clearUser = () => {
  clearPendingUserSearch()
  userKeyword.value = ''
  userResults.value = []
  showUserDropdown.value = false
  filters.value.user_id = undefined
  clearApiKey()
  emitChange()
}

const selectApiKey = (k: SimpleApiKey) => {
  apiKeyKeyword.value = k.name || String(k.id)
  showApiKeyDropdown.value = false
  filters.value.api_key_id = k.id
  emitChange()
}

const clearApiKey = () => {
  apiKeyKeyword.value = ''
  apiKeyResults.value = []
  showApiKeyDropdown.value = false
  filters.value.api_key_id = undefined
}

const onClearApiKey = () => {
  clearApiKey()
  emitChange()
}

// 面板内重置只清空筛选条件，日期范围保持不变；关键词由下方的 watch 跟随清空。
const resetPanelFilters = () => {
  clearPendingUserSearch()
  for (const key of Object.keys(filters.value)) {
    if (key !== 'start_date' && key !== 'end_date') filters.value[key] = undefined
  }
  emitChange()
}

const debounceProviderSearch = () => {
  if (providerSearchTimeout) clearTimeout(providerSearchTimeout)
  providerSearchTimeout = setTimeout(async () => {
    if (!providerKeyword.value) {
      providerResults.value = []
      return
    }
    try {
      const res = await adminAPI.providers.list(1, 20, { search: providerKeyword.value })
      providerResults.value = res.items.map((a) => ({ id: a.id, name: a.name }))
    } catch {
      providerResults.value = []
    }
  }, SEARCH_DEBOUNCE_MS)
}

const selectProvider = (a: SimpleProvider) => {
  providerKeyword.value = a.name
  showProviderDropdown.value = false
  filters.value.provider_id = a.id
  emitChange()
}

const clearProvider = () => {
  providerKeyword.value = ''
  providerResults.value = []
  showProviderDropdown.value = false
  filters.value.provider_id = undefined
  emitChange()
}

const onApiKeyFocus = () => {
  showApiKeyDropdown.value = true
  // Trigger search if no results yet
  if (apiKeyResults.value.length === 0) {
    debounceApiKeySearch()
  }
}

const onDocumentClick = (e: MouseEvent) => {
  const target = e.target as Node | null
  if (!target) return
  if (target instanceof Element && target.closest('.select-dropdown-portal')) return

  const clickedInsideUser = userSearchRef.value?.contains(target) ?? false
  const clickedInsideApiKey = apiKeySearchRef.value?.contains(target) ?? false
  const clickedInsideProvider = providerSearchRef.value?.contains(target) ?? false

  if (!clickedInsideUser) showUserDropdown.value = false
  if (!clickedInsideApiKey) showApiKeyDropdown.value = false
  if (!clickedInsideProvider) showProviderDropdown.value = false
}

watch(
  () => props.startDate,
  (value) => {
    filters.value.start_date = value
  },
  { immediate: true }
)

watch(
  () => props.endDate,
  (value) => {
    filters.value.end_date = value
  },
  { immediate: true }
)

watch(
  () => filters.value.user_id,
  (userId) => {
    if (!userId) {
      clearPendingUserSearch()
      userKeyword.value = ''
      userResults.value = []
    }
  }
)

watch(
  () => filters.value.api_key_id,
  (apiKeyId) => {
    if (!apiKeyId) {
      apiKeyKeyword.value = ''
      apiKeyResults.value = []
    }
  }
)

watch(
  () => filters.value.provider_id,
  (providerId) => {
    if (!providerId) {
      providerKeyword.value = ''
      providerResults.value = []
    }
  }
)

onMounted(async () => {
  document.addEventListener('click', onDocumentClick)
  // 团队和分组选项相互独立，任一加载失败都不影响其他筛选器使用。
  await Promise.allSettled([
    adminAPI.groups.list(1, 1000).then((gs) => {
      groupOptions.value.push(...gs.items.map((g: any) => ({ value: g.id, label: g.name })))
    }),
    adminAPI.teams.list().then((teams) => {
      teamOptions.value.push(...teams.map((team) => ({ value: team.id, label: team.name })))
    }),
  ])
})

onUnmounted(() => {
  clearPendingUserSearch()
  document.removeEventListener('click', onDocumentClick)
})

// 供外部(如用户排行下钻)在程序化设置 user_id 后回显选中的用户邮箱
const setUserKeyword = (email: string) => {
  clearPendingUserSearch()
  userKeyword.value = email
  userResults.value = []
  showUserDropdown.value = false
}

// 以搜索修订号识别异步结果，管理员继续输入后过期的查询结果失效。
const getUserSearchRevision = () => userSearchSequence

defineExpose({ getUserSearchRevision, setUserKeyword })
</script>
