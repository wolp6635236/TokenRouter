<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div
          class="flex flex-col justify-between gap-2 lg:flex-row lg:items-start"
        >
          <!-- 左侧：模糊搜索和筛选项，可自动换行。 -->
          <div class="flex min-w-0 flex-1 flex-nowrap items-center gap-2">
            <div class="input-icon-wrap min-w-0 flex-1 sm:flex-none sm:w-64">
              <Icon
                name="search"
                size="md"
                class="input-icon text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('admin.groups.searchGroups')"
                class="input input-has-icon"
                @input="handleSearch"
              />
            </div>
            <FilterDropdown :active-count="activeFilterCount" @reset="resetGroupFilters">
              <FilterField :label="t('admin.groups.columns.status')">
                <Select v-model="filters.status" :options="statusOptions" :placeholder="t('admin.groups.allStatus')" @change="loadGroups" />
              </FilterField>
            </FilterDropdown>
          </div>

          <!-- 右侧：刷新、排序和创建等操作。 -->
          <div
            class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-2 lg:w-auto"
          >
            <button
              @click="loadGroups"
              :disabled="loading"
              class="btn btn-secondary shrink-0 btn-icon"
              :title="t('common.refresh')"
            >
              <Icon
                name="refresh"
                size="sm"
                :class="loading ? 'animate-spin' : ''"
              />
            </button>
            <div class="relative" ref="columnDropdownRef">
              <button
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary shrink-0 btn-icon"
                :title="t('admin.groups.columnSettings')"
              >
                <Icon name="grid" size="sm" />
                <span class="hidden">{{ t("admin.groups.columnSettings") }}</span>
              </button>
              <MotionTransition name="dropdown-fade">
                <div
                  v-if="showColumnDropdown" :inert="!(showColumnDropdown) || undefined"
                  class="dropdown right-0 top-full z-50 mt-1 max-h-menu w-48 overflow-y-auto"
                >
                  <button
                    v-for="col in toggleableColumns"
                    :key="col.key"
                    @click="toggleColumn(col.key)"
                    class="dropdown-item justify-between"
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
            <button
              @click="openSortModal"
              class="btn btn-secondary shrink-0 btn-icon"
              :title="t('admin.groups.sortOrder')"
            >
              <Icon name="arrowsUpDown" size="sm" :animate-on-hover="false" />
            </button>
            <button
              @click="openCreateModal"
              class="btn btn-primary whitespace-nowrap"
              data-tour="groups-create-btn"
            >
              <Icon name="plus" size="sm" class="mr-2" />
              {{ t("admin.groups.createGroup") }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          column-order-storage-key="admin-groups-column-order"
          :columns="columns"
          :data="groups"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="sort_order"
          default-sort-order="asc"
          @sort="handleSort"
        >
          <template #cell-name="{ value }">
            <div class="flex items-center gap-2">
              <span class="font-medium text-gray-900 dark:text-white">{{
                value
              }}</span>

            </div>
          </template>

          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-gray-400"
              >#{{ value }}</span
            >
          </template>

          <template #cell-display_brand="{ value }">
            <span v-if="value" :class="displayBrandBadgeClass(value)">
              <ProviderIcon :brand="String(value)" size="14px" />
              <!-- 品牌名加 self-baseline，移动卡片的标签对齐品牌名。 -->
              <span class="self-baseline">{{ displayBrandLabel(value) }}</span>
            </span>
            <span v-else class="text-sm text-gray-700 dark:text-gray-300">-</span>
          </template>

          <template #cell-rate_multiplier="{ value }">
            <span class="text-sm text-gray-700 dark:text-gray-300"
              >{{ value }}x</span
            >
          </template>

          <template #cell-is_exclusive="{ value }">
            <span :class="['badge', value ? 'badge-primary' : 'badge-gray']">
              {{
                value ? t("admin.groups.exclusive") : t("admin.groups.public")
              }}
            </span>
          </template>

          <template #cell-session_isolation_enabled="{ value }">
            <span :class="['badge', value ? 'badge-warning' : 'badge-gray']">
              {{
                value
                  ? t("admin.groups.sessionIsolation.enabled")
                  : t("admin.groups.sessionIsolation.disabled")
              }}
            </span>
          </template>

          <template #cell-provider_count="{ row }">
            <div class="space-y-0.5 text-xs">
              <div>
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.providersAvailable")
                }}</span>
                <span
                  class="ml-1 font-medium text-emerald-600 dark:text-emerald-400"
                  >{{ row.active_provider_count || 0 }}</span
                >
              </div>
              <div v-if="row.rate_limited_provider_count">
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.providersRateLimited")
                }}</span>
                <span
                  class="ml-1 font-medium text-amber-600 dark:text-amber-400"
                  >{{ row.rate_limited_provider_count }}</span
                >
              </div>
              <div>
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.providersTotal")
                }}</span>
                <span
                  class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{ row.provider_count || 0 }}</span
                >
              </div>
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <GroupCapacityBadge
              v-if="capacityMap.get(row.id)"
              :concurrency-used="capacityMap.get(row.id)!.concurrencyUsed"
              :concurrency-max="capacityMap.get(row.id)!.concurrencyMax"
              :sessions-used="capacityMap.get(row.id)!.sessionsUsed"
              :sessions-max="capacityMap.get(row.id)!.sessionsMax"
              :rpm-used="capacityMap.get(row.id)!.rpmUsed"
              :rpm-max="capacityMap.get(row.id)!.rpmMax"
            />
            <span v-else class="text-xs text-gray-400">—</span>
          </template>

          <template #cell-usage="{ row }">
            <div v-if="usageLoading" class="text-xs text-gray-400">—</div>
            <div v-else class="space-y-0.5 text-xs">
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageToday")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{
                    formatGroupBalance(usageMap.get(row.id)?.today_cost ?? 0)
                  }}</span
                >
              </div>
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageYesterday")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{
                    formatGroupBalance(usageMap.get(row.id)?.yesterday_cost ?? 0)
                  }}</span
                >
              </div>
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageTotal")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{
                    formatGroupBalance(usageMap.get(row.id)?.total_cost ?? 0)
                  }}</span
                >
              </div>
            </div>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'active' ? 'badge-success' : 'badge-danger',
              ]"
            >
              {{ t("admin.providers.status." + value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                @click="handleEdit(row)"
                class="flex flex-col items-center gap-0.5 rounded-control p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t("common.edit") }}</span>
              </button>
              <button
                type="button"
                data-testid="group-more"
                :title="t('common.more')"
                :aria-label="t('common.more')"
                aria-haspopup="menu"
                :aria-expanded="actionMenuGroup?.id === row.id"
                :aria-controls="actionMenuGroup?.id === row.id ? `group-action-menu-${row.id}` : undefined"
                @click="openGroupActionMenu(row, $event)"
                class="flex flex-col items-center gap-0.5 rounded-control p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:hover:bg-dark-700 dark:hover:text-white"
              >
                <Icon name="more" size="sm" />
                <span class="text-xs">{{ t("common.more") }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.groups.noGroupsYet')"
              :description="t('admin.groups.createFirstGroup')"
              :action-text="t('admin.groups.createGroup')"
              @action="openCreateModal"
            />
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

    <GroupActionMenu
      :show="actionMenuGroup !== null"
      :group="actionMenuGroup"
      :position="actionMenuPosition"
      :duplicating="actionMenuGroup !== null && duplicatingGroupIds.has(actionMenuGroup.id)"
      @close="closeGroupActionMenu"
      @duplicate="handleDuplicate"
      @rate-multipliers="handleRateMultipliers"
      @rpm-overrides="handleRPMOverrides"
      @delete="handleDelete"
    />

    <!-- Create Group Modal -->
    <BaseDialog
      :show="showCreateModal"
      :body-scroll="false"
      :close-on-escape="!showAdvancedSchedulerOverridesModal"
      :title="t('admin.groups.createGroup')"
      width="wide"
      @close="closeCreateModal"
    >
      <form
        id="create-group-form"
        @submit.prevent="handleCreateGroup"
        novalidate
        class="flex min-h-0 flex-1 flex-col"
      >
        <GroupSettingsForm
          ref="createSettingsRef"
          mode="create"
          :model-value="createForm"
          :options="{
            copyProviders: copyProvidersGroupOptions,
            unavailableFallback: unavailableFallbackGroupOptions,
            invalidRequestFallback: invalidRequestFallbackOptions,
            clientFallback: fallbackGroupOptions,
            probeModels: createAvailabilityProbeModelOptions,
          }"
          :routing-rules="createModelRoutingRules"
          :models-list="createModelsListState"
          :models-list-loading="createModelsListLoading"
          :provider-search="{ keywords: providerSearchKeyword, results: providerSearchResults, open: showProviderDropdown }"
          :get-rule-key="getCreateRuleSearchKey"
          @patch="Object.assign(createForm, $event)"
          @configure-scheduler="openAdvancedSchedulerOverrides('create')"
          @add-rule="addCreateRoutingRule"
          @remove-rule="removeCreateRoutingRule"
          @rule-pattern="(rule, value) => rule.pattern = value"
          @search-providers="(rule, keyword) => updateProviderSearch(rule, keyword)"
          @focus-providers="rule => onProviderSearchFocus(rule)"
          @select-provider="(rule, provider) => selectProvider(rule, provider)"
          @remove-provider="(rule, id) => removeSelectedProvider(rule, id)"
          @models-enabled="createModelsListState.enabled = $event"
          @select-model="(id, value) => setModelSelection(createModelsListState, id, value)"
          @select-all-models="selectAllModelsListItems(createModelsListState)"
          @invert-models="invertModelsListSelection(createModelsListState)"
          @move-model="(from, to) => moveModelsListItem(createModelsListState, from, to)"
        />
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            @click="closeCreateModal"
            type="button"
            class="btn btn-secondary"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            type="submit"
            form="create-group-form"
            :disabled="submitting || !protocolCatalog"
            class="btn btn-primary"
            data-tour="group-form-submit"
          >
            <Icon
              name="loader"
              size="sm"
              :animate-on-hover="false"
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
            />
            {{ submitting ? t("admin.groups.creating") : t("common.create") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Edit Group Modal -->
    <BaseDialog
      :show="showEditModal"
      :body-scroll="false"
      :close-on-escape="!showAdvancedSchedulerOverridesModal"
      :title="t('admin.groups.editGroup')"
      width="wide"
      @close="closeEditModal"
    >
      <form
        v-if="editingGroup"
        id="edit-group-form"
        @submit.prevent="handleUpdateGroup"
        novalidate
        class="flex min-h-0 flex-1 flex-col"
      >
        <GroupSettingsForm
          ref="editSettingsRef"
          mode="edit"
          :model-value="editForm"
          :options="{
            copyProviders: copyProvidersGroupOptionsForEdit,
            unavailableFallback: unavailableFallbackGroupOptionsForEdit,
            invalidRequestFallback: invalidRequestFallbackOptionsForEdit,
            clientFallback: fallbackGroupOptionsForEdit,
            probeModels: editAvailabilityProbeModelOptions,
          }"
          :routing-rules="editModelRoutingRules"
          :models-list="editModelsListState"
          :models-list-loading="editModelsListLoading"
          :provider-search="{ keywords: providerSearchKeyword, results: providerSearchResults, open: showProviderDropdown }"
          :get-rule-key="getEditRuleSearchKey"
          @patch="Object.assign(editForm, $event)"
          @configure-scheduler="openAdvancedSchedulerOverrides('edit')"
          @add-rule="addEditRoutingRule"
          @remove-rule="removeEditRoutingRule"
          @rule-pattern="(rule, value) => rule.pattern = value"
          @search-providers="(rule, keyword) => updateProviderSearch(rule, keyword, true)"
          @focus-providers="rule => onProviderSearchFocus(rule, true)"
          @select-provider="(rule, provider) => selectProvider(rule, provider, true)"
          @remove-provider="(rule, id) => removeSelectedProvider(rule, id, true)"
          @models-enabled="editModelsListState.enabled = $event"
          @select-model="(id, value) => setModelSelection(editModelsListState, id, value)"
          @select-all-models="selectAllModelsListItems(editModelsListState)"
          @invert-models="invertModelsListSelection(editModelsListState)"
          @move-model="(from, to) => moveModelsListItem(editModelsListState, from, to)"
        />
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            @click="closeEditModal"
            type="button"
            class="btn btn-secondary"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            type="submit"
            form="edit-group-form"
            :disabled="submitting || !protocolCatalog"
            class="btn btn-primary"
            data-tour="group-form-submit"
          >
            <Icon
              name="loader"
              size="sm"
              :animate-on-hover="false"
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
            />
            {{ submitting ? t("admin.groups.updating") : t("common.update") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.groups.deleteGroup')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />

    <ConfirmDialog
      :show="showUnsupportedLiveConfirm"
      :title="t('admin.groups.openaiLive.unsupportedTitle')"
      :message="t('admin.groups.openaiLive.unsupportedMessage')"
      :confirm-text="t('admin.groups.openaiLive.enableAnyway')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmUnsupportedLive"
      @cancel="cancelUnsupportedLive"
    />

    <!-- Sort Order Modal -->
    <BaseDialog
      :show="showSortModal"
      :title="t('admin.groups.sortOrder')"
      width="normal"
      @close="closeSortModal"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.sortOrderHint") }}
        </p>
        <VueDraggable
          v-model="sortableGroups"
          :animation="200"
          class="space-y-2"
        >
          <div
            v-for="group in sortableGroups"
            :key="group.id"
            class="flex cursor-grab items-center gap-3 rounded-surface border border-gray-200 bg-white p-3 transition-shadow hover:shadow-md active:cursor-grabbing dark:border-dark-600 dark:bg-dark-700"
          >
            <div class="text-gray-400">
              <Icon name="menu" size="md" />
            </div>
            <div class="flex-1">
              <div class="font-medium text-gray-900 dark:text-white">
                {{ group.name }}
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">

              </div>
            </div>
            <div class="text-sm text-gray-400">#{{ group.id }}</div>
          </div>
        </VueDraggable>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            @click="closeSortModal"
            type="button"
            class="btn btn-secondary"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            @click="saveSortOrder"
            :disabled="sortSubmitting"
            class="btn btn-primary"
          >
            <Icon
              name="loader"
              size="sm"
              :animate-on-hover="false"
              v-if="sortSubmitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
            />
            {{ sortSubmitting ? t("common.saving") : t("common.save") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Group Rate Multipliers Modal -->
    <GroupRateMultipliersModal
      :show="showRateMultipliersModal"
      :group="rateMultipliersGroup"
      @close="showRateMultipliersModal = false"
      @success="loadGroups"
    />

    <!-- Group RPM Overrides Modal -->
    <GroupRPMOverridesModal
      :show="showRPMOverridesModal"
      :group="rpmOverridesGroup"
      @close="showRPMOverridesModal = false"
      @success="loadGroups"
    />

    <GroupAdvancedSchedulerOverridesModal
      :show="showAdvancedSchedulerOverridesModal"
      :model-value="advancedSchedulerOverridesDraft"
      @close="closeAdvancedSchedulerOverrides"
      @save="saveAdvancedSchedulerOverrides"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import type { LocalizedUpdate } from '@/i18n/content'
import MotionTransition from '@/components/common/MotionTransition.vue'
import { ref, reactive, computed, onMounted, onUnmounted, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useAppStore } from "@/stores/app";
import { useOnboardingStore } from "@/stores/onboarding";
import { adminAPI } from "@/api/admin";
import { useBalanceDisplay } from "@/composables/useBalanceDisplay";
import { SEARCH_DEBOUNCE_MS } from "@/constants/ui";
import type {
  AdminGroup,
  GroupAvailabilityProbeConfig,
  ProtocolID,
  GroupSchedulerType,
  GroupAdvancedSchedulerOverrides,
} from "@/types";
import type { Column } from "@/components/common/types";
import AppLayout from "@/components/layout/AppLayout.vue";
import TablePageLayout from "@/components/layout/TablePageLayout.vue";
import DataTable from "@/components/common/DataTable.vue";
import Pagination from "@/components/common/Pagination.vue";
import BaseDialog from "@/components/common/BaseDialog.vue";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import Select from "@/components/common/Select.vue";
import FilterDropdown from "@/components/common/FilterDropdown.vue";
import FilterField from "@/components/common/FilterField.vue";
import ProviderIcon from "@/components/common/ProviderIcon.vue";
import Icon from "@/components/icons/Icon.vue";
import GroupRateMultipliersModal from "@/components/admin/group/GroupRateMultipliersModal.vue";
import GroupActionMenu from "@/components/admin/group/GroupActionMenu.vue";
import GroupRPMOverridesModal from "@/components/admin/group/GroupRPMOverridesModal.vue";
import GroupCapacityBadge from "@/components/common/GroupCapacityBadge.vue";
import { loadProtocolCatalog, protocolCatalog } from '@/api/admin/protocolCapabilities';
import GroupAdvancedSchedulerOverridesModal from "@/components/admin/group/GroupAdvancedSchedulerOverridesModal.vue";
import { defaultRoutingPolicy, cloneRoutingPolicy } from '@/components/admin/group/routingPolicy';
import GroupSettingsForm from "@/components/admin/group/GroupSettingsForm.vue";
import type { GroupModelRoutingRule as ModelRoutingRule, GroupRoutingProvider as SimpleProvider } from "@/components/admin/group/groupSettingsTypes";
import { VueDraggable } from "vue-draggable-plus";
import { createStableObjectKeyResolver } from "@/utils/stableObjectKey";
import { getFloatingPanelPosition } from "@/utils/floatingPanel";
import {
  providerBrandDisplayName,
  resolveProviderBrand,
} from "@/utils/providerBrand";
import { extractApiErrorMessage } from "@/utils/apiError";
import {
  effectiveGroupClientProtocols,
  sanitizeGroupProtocolFallbacks,
} from "@/utils/groupClientProtocols";
import { useKeyedDebouncedSearch } from "@/composables/useKeyedDebouncedSearch";
import { getPersistedPageSize } from "@/composables/usePersistedPageSize";
import {
  buildModelsListConfig,
  createModelsListState as createInitialModelsListState,
  getAvailabilityProbeCandidateModels,
  invertModelsListSelection,
  moveModelsListItem,
  selectAllModelsListItems,
  setModelsListCandidates,
} from "./groupsModelsList";
import { createModelsListCandidatesTracker } from "./groupsModelsListCandidates";
import {
  normalizeGroupOpenAIFastPolicy,
} from "./groupsOpenAIFast";
import {
  normalizeReasoningEffortForPlatform,
  normalizeReasoningEffortOverLimit,
  reasoningEffortMappingsToAPI,
  reasoningEffortMappingsToRows,
  reasoningEffortOverLimitDowngrade,
  type ReasoningEffortMappingRow,
} from "./groupsReasoningEffort";

const { t } = useI18n();
const appStore = useAppStore();
const onboardingStore = useOnboardingStore();
const { formatBalanceAmount } = useBalanceDisplay();

const ALWAYS_VISIBLE_COLUMNS = new Set(["name", "actions"]);
// 首次加载或列结构升级后默认隐藏的列。
const DEFAULT_HIDDEN_COLUMNS = ["id"];
const HIDDEN_COLUMNS_KEY = "group-hidden-columns";
// 新增默认隐藏列时递增版本，让已有管理员只执行一次迁移。
const COLUMN_SETTINGS_VERSION_KEY = "group-column-settings-version";
const COLUMN_SETTINGS_VERSION = 2;
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ["id"],
};

const allColumns = computed<Column[]>(() => [
  { key: "name", label: t("admin.groups.columns.name"), sortable: true },
  { key: "id", label: t("admin.groups.columns.id"), sortable: true },
  {
    key: "display_brand",
    label: t("admin.groups.columns.displayBrand"),
    sortable: true,
  },
  {
    key: "rate_multiplier",
    label: t("admin.groups.columns.rateMultiplier"),
    sortable: true,
  },
  {
    key: "is_exclusive",
    label: t("admin.groups.columns.exclusive"),
    sortable: true,
  },
  {
    key: "session_isolation_enabled",
    label: t("admin.groups.columns.sessionIsolation"),
    sortable: true,
  },
  {
    key: "provider_count",
    label: t("admin.groups.columns.providers"),
    sortable: true,
  },
  {
    key: "capacity",
    label: t("admin.groups.columns.capacity"),
    sortable: false,
  },
  { key: "usage", label: t("admin.groups.columns.usage"), sortable: false },
  { key: "status", label: t("admin.groups.columns.status"), sortable: true },
  { key: "actions", label: t("admin.groups.columns.actions"), sortable: false },
]);

const toggleableColumns = computed(() =>
  allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key)),
);
const hiddenColumns = reactive<Set<string>>(new Set());
const showColumnDropdown = ref(false);
const columnDropdownRef = ref<HTMLElement | null>(null);

const getValidHiddenColumnKeys = () =>
  new Set(toggleableColumns.value.map((col) => col.key));

const activeFilterCount = computed(
  () => [filters.status].filter(Boolean).length,
);

const resetGroupFilters = () => {
  filters.status = "";
  loadGroups();
};

const loadSavedColumns = () => {
  hiddenColumns.clear();
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY);
    const validKeys = getValidHiddenColumnKeys();

    if (saved) {
      const parsed = JSON.parse(saved);
      if (Array.isArray(parsed)) {
        parsed
          .filter(
            (key): key is string =>
              typeof key === "string" && validKeys.has(key),
          )
          .forEach((key) => hiddenColumns.add(key));
      }

      // 已有管理员自动隐藏本次升级新增的默认隐藏列。
      const parsedVersion = Number(
        localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? "1",
      );
      const storedVersion = Number.isSafeInteger(parsedVersion) && parsedVersion >= 1
        ? parsedVersion
        : 1;
      if (storedVersion < COLUMN_SETTINGS_VERSION) {
        let mutated = false;
        for (let version = storedVersion + 1; version <= COLUMN_SETTINGS_VERSION; version++) {
          for (const key of VERSION_NEW_HIDDEN_COLUMNS[version] ?? []) {
            if (validKeys.has(key) && !hiddenColumns.has(key)) {
              hiddenColumns.add(key);
              mutated = true;
            }
          }
        }
        if (mutated) {
          saveColumnsToStorage();
        } else {
          localStorage.setItem(
            COLUMN_SETTINGS_VERSION_KEY,
            String(COLUMN_SETTINGS_VERSION),
          );
        }
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        if (validKeys.has(key)) hiddenColumns.add(key);
      });
      saveColumnsToStorage();
    }
  } catch (error) {
    console.error("Failed to load group column settings:", error);
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key));
  }
};

const saveColumnsToStorage = () => {
  try {
    const validKeys = getValidHiddenColumnKeys();
    const keys = [...hiddenColumns].filter((key) => validKeys.has(key));
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(keys));
    localStorage.setItem(
      COLUMN_SETTINGS_VERSION_KEY,
      String(COLUMN_SETTINGS_VERSION),
    );
  } catch (error) {
    console.error("Failed to save group column settings:", error);
  }
};

const isColumnVisible = (key: string) => !hiddenColumns.has(key);
const hasVisibleUsageColumn = computed(() => isColumnVisible("usage"));
const hasVisibleCapacityColumn = computed(() => isColumnVisible("capacity"));

const toggleColumn = (key: string) => {
  const validKeys = getValidHiddenColumnKeys();
  if (!validKeys.has(key)) return;

  const wasHidden = hiddenColumns.has(key);
  if (wasHidden) {
    hiddenColumns.delete(key);
  } else {
    hiddenColumns.add(key);
  }
  saveColumnsToStorage();

  if (wasHidden && key === "usage") {
    loadUsageSummary();
  }
  if (wasHidden && key === "capacity") {
    loadCapacitySummary();
  }
};

const columns = computed<Column[]>(() =>
  allColumns.value.filter(
    (col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key),
  ),
);

if (typeof window !== "undefined") {
  loadSavedColumns();
}

// Filter options
const statusOptions = computed(() => [
  { value: "", label: t("admin.groups.allStatus") },
  { value: "active", label: t("admin.providers.status.active") },
  { value: "inactive", label: t("admin.providers.status.inactive") },
]);

const cloneAdvancedSchedulerOverrides = (
  value?: GroupAdvancedSchedulerOverrides,
): GroupAdvancedSchedulerOverrides => ({ ...(value || {}) });

const openAdvancedSchedulerOverrides = (target: "create" | "edit") => {
  advancedSchedulerOverridesTarget.value = target;
  advancedSchedulerOverridesDraft.value = cloneAdvancedSchedulerOverrides(
    target === "create"
      ? createForm.advanced_scheduler_overrides
      : editForm.advanced_scheduler_overrides,
  );
  showAdvancedSchedulerOverridesModal.value = true;
};

const closeAdvancedSchedulerOverrides = () => {
  showAdvancedSchedulerOverridesModal.value = false;
  advancedSchedulerOverridesTarget.value = null;
};

const saveAdvancedSchedulerOverrides = (value: GroupAdvancedSchedulerOverrides) => {
  if (advancedSchedulerOverridesTarget.value === "create") {
    createForm.advanced_scheduler_overrides = cloneAdvancedSchedulerOverrides(value);
  } else if (advancedSchedulerOverridesTarget.value === "edit") {
    editForm.advanced_scheduler_overrides = cloneAdvancedSchedulerOverrides(value);
  }
  closeAdvancedSchedulerOverrides();
};

// 降级分组选项（创建时）- 仅包含未启用 claude_code_only 的分组
const fallbackGroupOptions = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.claudeCode.noFallback") },
  ];
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) =>
      !g.claude_code_only &&
      g.status === "active",
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 降级分组选项（编辑时）- 排除自身
const fallbackGroupOptionsForEdit = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.claudeCode.noFallback") },
  ];
  const currentId = editingGroup.value?.id;
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) =>
      !g.claude_code_only &&
      g.status === "active" &&
      g.id !== currentId,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 不可用回退分组选项（创建时）：仅允许启用中的分组。
const unavailableFallbackGroupOptions = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.unavailableFallback.noFallback") },
  ];
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) => g.status === "active",
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 编辑回退分组时从选项中排除当前分组。
const unavailableFallbackGroupOptionsForEdit = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.unavailableFallback.noFallback") },
  ];
  const currentId = editingGroup.value?.id;
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) =>
      g.status === "active" &&
      g.id !== currentId,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 无效请求兜底分组选项（创建时）- 仅包含未配置兜底的分组
const invalidRequestFallbackOptions = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.invalidRequestFallback.noFallback") },
  ];
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) =>
      g.status === "active" &&
      g.fallback_group_id_on_invalid_request === null,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 无效请求兜底分组选项（编辑时）- 排除自身
const invalidRequestFallbackOptionsForEdit = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.invalidRequestFallback.noFallback") },
  ];
  const currentId = editingGroup.value?.id;
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) =>
      g.status === "active" &&
      g.fallback_group_id_on_invalid_request === null &&
      g.id !== currentId,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 复制提供商的源分组选项（创建时）- 仅包含有提供商的分组
const copyProvidersGroupOptions = computed(() => {
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) => (g.provider_count || 0) > 0,
  );
  return eligibleGroups.map((g) => ({
    value: g.id,
    label: t("admin.groups.settings.groupProviders", { name: g.name, count: g.provider_count || 0 }),
  }));
});

// 复制提供商的源分组选项（编辑时）- 仅包含有提供商的分组，排除自身
const copyProvidersGroupOptionsForEdit = computed(() => {
  const currentId = editingGroup.value?.id;
  const eligibleGroups = unavailableFallbackGroups.value.filter(
    (g) =>
      (g.provider_count || 0) > 0 &&
      g.id !== currentId,
  );
  return eligibleGroups.map((g) => ({
    value: g.id,
    label: t("admin.groups.settings.groupProviders", { name: g.name, count: g.provider_count || 0 }),
  }));
});

const groups = ref<AdminGroup[]>([]);
// 不可用回退分组需要跨分页选择，因此单独保存全量 active 分组选项来源。
const unavailableFallbackGroups = ref<AdminGroup[]>([]);
const loading = ref(false);
const usageMap = ref<Map<number, { today_cost: number; yesterday_cost: number; total_cost: number }>>(
  new Map(),
);
const usageLoading = ref(false);
const capacityMap = ref<
  Map<
    number,
    {
      concurrencyUsed: number;
      concurrencyMax: number;
      sessionsUsed: number;
      sessionsMax: number;
      rpmUsed: number;
      rpmMax: number;
    }
  >
>(new Map());
const searchQuery = ref("");
const filters = reactive({
  status: "",
});
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0,
});
const sortState = reactive({
  sort_by: "sort_order",
  sort_order: "asc" as "asc" | "desc",
});

let abortController: AbortController | null = null;

const showCreateModal = ref(false);
const showEditModal = ref(false);
const showDeleteDialog = ref(false);
const pendingLiveForm = ref<"create" | "edit" | null>(null);
const showUnsupportedLiveConfirm = computed(
  () => pendingLiveForm.value !== null,
);
const liveCapability = ref<{ supported: boolean; reason?: string } | null>(null);
let liveCapabilityRequest: Promise<{
  supported: boolean;
  reason?: string;
}> | null = null;
const showSortModal = ref(false);
const submitting = ref(false);
const sortSubmitting = ref(false);
const editingGroup = ref<AdminGroup | null>(null);
const deletingGroup = ref<AdminGroup | null>(null);
const duplicatingGroupIds = reactive(new Set<number>());
const actionMenuGroup = ref<AdminGroup | null>(null);
const actionMenuPosition = ref<{ top: number; left: number } | null>(null);

// 菜单按视口坐标定位到 body，脱离卡片和固定操作列的裁剪区域。
const openGroupActionMenu = (group: AdminGroup, event: MouseEvent) => {
  if (actionMenuGroup.value?.id === group.id) {
    closeGroupActionMenu();
    return;
  }
  const target = event.currentTarget as HTMLElement | null;
  if (!target) return;
  const rect = target.getBoundingClientRect();
  // 固定高菜单:下方放不下即整体上翻;窄屏保持右缘对齐触发器,不钉视口左缘。
  const position = getFloatingPanelPosition(rect, window.innerWidth, window.innerHeight, {
    maxWidth: 192,
    fixedHeight: 162,
    viewportPadding: 8,
    gap: 4,
    pinLeftOnMobile: false
  });
  // fixedHeight 模式下 top 恒非空。
  actionMenuPosition.value = { top: position.top ?? 8, left: position.left };
  actionMenuGroup.value = group;
};

const closeGroupActionMenu = () => {
  actionMenuGroup.value = null;
  actionMenuPosition.value = null;
};
const showRateMultipliersModal = ref(false);
const rateMultipliersGroup = ref<AdminGroup | null>(null);
const showRPMOverridesModal = ref(false);
const rpmOverridesGroup = ref<AdminGroup | null>(null);
const showAdvancedSchedulerOverridesModal = ref(false);
const advancedSchedulerOverridesTarget = ref<"create" | "edit" | null>(null);
const advancedSchedulerOverridesDraft = ref<GroupAdvancedSchedulerOverrides>({});
const sortableGroups = ref<AdminGroup[]>([]);
const createModelsListState = reactive(createInitialModelsListState());
const editModelsListState = reactive(createInitialModelsListState());
const createModelsListLoading = ref(false);
const editModelsListLoading = ref(false);
const createSettingsRef = ref<InstanceType<typeof GroupSettingsForm> | null>(null);
const editSettingsRef = ref<InstanceType<typeof GroupSettingsForm> | null>(null);
const modelsListCandidatesTracker = createModelsListCandidatesTracker();
const createAvailabilityProbeModelOptions = computed(() =>
  buildAvailabilityProbeModelOptions(getAvailabilityProbeCandidateModels(createModelsListState)),
);
const editAvailabilityProbeModelOptions = computed(() =>
  buildAvailabilityProbeModelOptions(getAvailabilityProbeCandidateModels(editModelsListState)),
);

const createForm = reactive({
  localization: undefined as LocalizedUpdate<{ display_name: string; description: string }> | undefined,
  name: "",
  description: "",
  display_brand: "",
  scheduler_type: "basic" as GroupSchedulerType,
  advanced_scheduler_overrides: {} as GroupAdvancedSchedulerOverrides,
  protocol_fallbacks: {} as Partial<Record<ProtocolID, ProtocolID[]>>,
  responses_image_policy: "inherit" as "inherit" | "enabled" | "disabled" | "block",
  allowed_protocols: [] as ProtocolID[],
  rate_multiplier: 1.0,
  is_exclusive: false,
  // 会话隔离开关
  session_isolation_enabled: false,

  routing_policy: defaultRoutingPolicy(),
  // 图片生成权限
  allow_image_generation: false,
  allow_batch_image_generation: false,

  // Claude Code 客户端限制（仅 anthropic 平台使用）
  claude_code_only: false,
  fallback_group_id: null as number | null,
  fallback_group_id_on_invalid_request: null as number | null,
  // 分组不可用时优先使用的指定回退分组。
  unavailable_fallback_group_id: null as number | null,
  // OpenAI Messages 模型映射（仅 openai 平台使用）
  allow_live: false,
  // OpenAI 分组级 Fast 强制策略
  openai_fast_policy: "follow_request",

  // 提供商过滤控制（OpenAI/Antigravity 平台）
  require_oauth_only: false,
  require_privacy_set: false,
  // 模型路由开关
  model_routing_enabled: false,
  // 从分组复制提供商
  copy_providers_from_group_ids: [] as number[],
  // 分组级 RPM 限制（每用户每分钟最大请求数；0 = 不限制）
  rpm_limit: 0 as number,
  max_reasoning_effort: "",
  max_reasoning_effort_over_limit: reasoningEffortOverLimitDowngrade,
  reasoning_effort_mappings: [] as ReasoningEffortMappingRow[],
  // 分组主动可用性探测配置
  availability_probe_enabled: false,
  availability_probe_model_id: "",
  availability_probe_prompt: "hi",
  availability_probe_interval_minutes: 30,
  availability_probe_timeout_seconds: 30,
  availability_probe_max_retries: 3,
  availability_probe_user_agent: "",
});

// 创建表单的模型路由规则
const createModelRoutingRules = ref<ModelRoutingRule[]>([]);

// 编辑表单的模型路由规则
const editModelRoutingRules = ref<ModelRoutingRule[]>([]);

// 规则对象稳定 key（避免使用 index 导致状态错位）
const resolveCreateRuleKey =
  createStableObjectKeyResolver<ModelRoutingRule>("create-rule");
const resolveEditRuleKey =
  createStableObjectKeyResolver<ModelRoutingRule>("edit-rule");

const getCreateRuleSearchKey = (rule: ModelRoutingRule) =>
  `create-${resolveCreateRuleKey(rule)}`;
const getEditRuleSearchKey = (rule: ModelRoutingRule) =>
  `edit-${resolveEditRuleKey(rule)}`;

const getRuleSearchKey = (rule: ModelRoutingRule, isEdit: boolean = false) => {
  return isEdit ? getEditRuleSearchKey(rule) : getCreateRuleSearchKey(rule);
};

// 提供商搜索相关状态
const providerSearchKeyword = ref<Record<string, string>>({});
const providerSearchResults = ref<Record<string, SimpleProvider[]>>({});
const showProviderDropdown = ref<Record<string, boolean>>({});

const clearProviderSearchStateByKey = (key: string) => {
  delete providerSearchKeyword.value[key];
  delete providerSearchResults.value[key];
  delete showProviderDropdown.value[key];
};

const clearAllProviderSearchState = () => {
  providerSearchKeyword.value = {};
  providerSearchResults.value = {};
  showProviderDropdown.value = {};
};

const providerSearchRunner = useKeyedDebouncedSearch<SimpleProvider[]>({
  delay: SEARCH_DEBOUNCE_MS,
  search: async (keyword, { signal }) => {
    const res = await adminAPI.providers.list(
      1,
      20,
      {
        search: keyword,
      },
      { signal },
    );
    return res.items.map((provider) => ({ id: provider.id, name: provider.name }));
  },
  onSuccess: (key, result) => {
    providerSearchResults.value[key] = result;
  },
  onError: (key) => {
    providerSearchResults.value[key] = [];
  },
});

// 模型路由可指向分组内任意平台的提供商。
const searchProviders = (key: string) => {
  providerSearchRunner.trigger(key, providerSearchKeyword.value[key] || "");
};

const updateProviderSearch = (
  rule: ModelRoutingRule,
  keyword: string,
  isEdit: boolean = false,
) => {
  const key = getRuleSearchKey(rule, isEdit);
  providerSearchKeyword.value[key] = keyword;
  searchProviders(key);
};

// 选择提供商
const selectProvider = (
  rule: ModelRoutingRule,
  provider: SimpleProvider,
  isEdit: boolean = false,
) => {
  if (!rule) return;

  // 检查是否已选择
  if (!rule.providers.some((a) => a.id === provider.id)) {
    rule.providers.push(provider);
  }

  // 清空搜索
  const key = getRuleSearchKey(rule, isEdit);
  providerSearchKeyword.value[key] = "";
  showProviderDropdown.value[key] = false;
};

// 移除已选提供商
const removeSelectedProvider = (
  rule: ModelRoutingRule,
  providerId: number,
  _isEdit: boolean = false,
) => {
  if (!rule) return;

  rule.providers = rule.providers.filter((a) => a.id !== providerId);
};

// 处理提供商搜索输入框聚焦
const onProviderSearchFocus = (
  rule: ModelRoutingRule,
  isEdit: boolean = false,
) => {
  const key = getRuleSearchKey(rule, isEdit);
  showProviderDropdown.value[key] = true;
  // 如果没有搜索结果，触发一次搜索
  if (!providerSearchResults.value[key]?.length) {
    searchProviders(key);
  }
};

// 添加创建表单的路由规则
const addCreateRoutingRule = () => {
  createModelRoutingRules.value.push({ pattern: "", providers: [] });
};

// 删除创建表单的路由规则
const removeCreateRoutingRule = (rule: ModelRoutingRule) => {
  const index = createModelRoutingRules.value.indexOf(rule);
  if (index === -1) return;

  const key = getCreateRuleSearchKey(rule);
  providerSearchRunner.clearKey(key);
  clearProviderSearchStateByKey(key);
  createModelRoutingRules.value.splice(index, 1);
};

// 添加编辑表单的路由规则
const addEditRoutingRule = () => {
  editModelRoutingRules.value.push({ pattern: "", providers: [] });
};

// 删除编辑表单的路由规则
const removeEditRoutingRule = (rule: ModelRoutingRule) => {
  const index = editModelRoutingRules.value.indexOf(rule);
  if (index === -1) return;

  const key = getEditRuleSearchKey(rule);
  providerSearchRunner.clearKey(key);
  clearProviderSearchStateByKey(key);
  editModelRoutingRules.value.splice(index, 1);
};

const resetModelsListState = (
  state: typeof createModelsListState,
  config?: Parameters<typeof createInitialModelsListState>[0],
) => {
  const fresh = createInitialModelsListState(config);
  state.enabled = fresh.enabled;
  state.savedModels = fresh.savedModels;
  state.candidateModels = fresh.candidateModels;
  state.items = fresh.items;
};

const loadModelsListCandidates = async (
  mode: "create" | "edit",
  groupID: number,
) => {
  const request = { mode, groupID };
  const requestID = modelsListCandidatesTracker.next(request);
  const state = mode === "create" ? createModelsListState : editModelsListState;
  const loadingRef = mode === "create" ? createModelsListLoading : editModelsListLoading;
  loadingRef.value = true;
  try {
    const models = await adminAPI.groups.getModelsListCandidates(groupID);
    if (!modelsListCandidatesTracker.isCurrent(requestID, request)) {
      return;
    }
    setModelsListCandidates(state, models);
  } catch (error) {
    if (!modelsListCandidatesTracker.isCurrent(requestID, request)) {
      return;
    }
    console.error("Error loading group models list candidates:", error);
  } finally {
    if (modelsListCandidatesTracker.isCurrent(requestID, request)) {
      loadingRef.value = false;
    }
  }
};

// 列表组件只发出选择事件，页面更新独立草稿供保存和探测候选共同读取。
function setModelSelection(state: typeof createModelsListState, id: string, value: boolean) {
  const item = state.items.find(item => item.id === id);
  if (item) item.selected = value;
}

function buildAvailabilityProbeModelOptions(models: string[]) {
  const seen = new Set<string>();
  const options = [{ value: "", label: t("admin.groups.availabilityProbe.selectModel") }];
  for (const raw of models) {
    const model = raw.trim();
    if (!model || seen.has(model)) {
      continue;
    }
    seen.add(model);
    options.push({ value: model, label: model });
  }
  return options;
}

const isAvailabilityProbeModelAvailable = (
  modelID: string,
  options: ReturnType<typeof buildAvailabilityProbeModelOptions>,
) => {
  // 空值代表尚未选择，始终允许保留。
  return !modelID || options.some((option) => option.value === modelID);
};

const resetAvailabilityProbeFormState = (
  form: typeof createForm | typeof editForm,
  config?: GroupAvailabilityProbeConfig | null,
) => {
  form.availability_probe_enabled = config?.enabled ?? false;
  form.availability_probe_model_id = config?.model_id ?? "";
  form.availability_probe_prompt = config?.prompt ?? "hi";
  form.availability_probe_interval_minutes = config?.interval_minutes ?? 30;
  form.availability_probe_timeout_seconds = config?.timeout_seconds ?? 30;
  form.availability_probe_max_retries = config?.max_retries ?? 3;
  form.availability_probe_user_agent = config?.user_agent ?? "";
};

const buildAvailabilityProbeConfig = (
  form: typeof createForm | typeof editForm,
): GroupAvailabilityProbeConfig => {
  if (!form.availability_probe_enabled) {
    return { enabled: false };
  }

  const modelID = form.availability_probe_model_id.trim();
  const prompt = form.availability_probe_prompt.trim();
  if (!modelID) {
    throw new Error(t("admin.groups.availabilityProbe.modelRequired"));
  }
  if (!prompt) {
    throw new Error(t("admin.groups.availabilityProbe.promptRequired"));
  }

  return {
    enabled: true,
    model_id: modelID,
    prompt,
    interval_minutes: Number(form.availability_probe_interval_minutes) || 30,
    timeout_seconds: Number(form.availability_probe_timeout_seconds) || 30,
    // Number("") 为 0，这里有意保留 0 次重试的显式配置。
    max_retries: Number(form.availability_probe_max_retries),
    user_agent: form.availability_probe_user_agent.trim(),
  };
};

// 将 UI 格式的路由规则转换为 API 格式
const convertRoutingRulesToApiFormat = (
  rules: ModelRoutingRule[],
): Record<string, number[]> | null => {
  const result: Record<string, number[]> = {};
  let hasValidRules = false;

  for (const rule of rules) {
    const pattern = rule.pattern.trim();
    if (!pattern) continue;

    const providerIds = rule.providers.map((a) => a.id).filter((id) => id > 0);

    if (providerIds.length > 0) {
      result[pattern] = providerIds;
      hasValidRules = true;
    }
  }

  return hasValidRules ? result : null;
};

// 将 API 格式的路由规则转换为 UI 格式（需要加载提供商名称）
const convertApiFormatToRoutingRules = async (
  apiFormat: Record<string, number[]> | null,
): Promise<ModelRoutingRule[]> => {
  if (!apiFormat) return [];

  const rules: ModelRoutingRule[] = [];
  for (const [pattern, providerIds] of Object.entries(apiFormat)) {
    // 加载提供商信息
    const providers: SimpleProvider[] = [];
    for (const id of providerIds) {
      try {
        const provider = await adminAPI.providers.getById(id);
        providers.push({ id: provider.id, name: provider.name });
      } catch {
        // 如果提供商不存在，仍然显示 ID
        providers.push({ id, name: `#${id}` });
      }
    }
    rules.push({ pattern, providers });
  }
  return rules;
};

const editForm = reactive({
  localization: undefined as LocalizedUpdate<{ display_name: string; description: string }> | undefined,
  name: "",
  description: "",
  display_brand: "",
  scheduler_type: "basic" as GroupSchedulerType,
  advanced_scheduler_overrides: {} as GroupAdvancedSchedulerOverrides,
  protocol_fallbacks: {} as Partial<Record<ProtocolID, ProtocolID[]>>,
  responses_image_policy: "inherit" as "inherit" | "enabled" | "disabled" | "block",
  allowed_protocols: [] as ProtocolID[],
  rate_multiplier: 1.0,
  is_exclusive: false,
  // 会话隔离开关
  session_isolation_enabled: false,
  status: "active" as "active" | "inactive",

  routing_policy: defaultRoutingPolicy(),
  // 图片生成权限
  allow_image_generation: false,
  allow_batch_image_generation: false,

  // Claude Code 客户端限制（仅 anthropic 平台使用）
  claude_code_only: false,
  fallback_group_id: null as number | null,
  fallback_group_id_on_invalid_request: null as number | null,
  // 分组不可用时优先使用的指定回退分组。
  unavailable_fallback_group_id: null as number | null,
  // OpenAI Messages 模型映射（仅 openai 平台使用）
  allow_live: false,
  // OpenAI 分组级 Fast 强制策略
  openai_fast_policy: "follow_request",

  default_mapped_model: '',
  // 提供商过滤控制（OpenAI/Antigravity 平台）
  require_oauth_only: false,
  require_privacy_set: false,
  // 模型路由开关
  model_routing_enabled: false,
  // 从分组复制提供商
  copy_providers_from_group_ids: [] as number[],
  // 分组级 RPM 限制（每用户每分钟最大请求数；0 = 不限制）
  rpm_limit: 0 as number,
  max_reasoning_effort: "",
  max_reasoning_effort_over_limit: reasoningEffortOverLimitDowngrade,
  reasoning_effort_mappings: [] as ReasoningEffortMappingRow[],
  // 分组主动可用性探测配置
  availability_probe_enabled: false,
  availability_probe_model_id: "",
  availability_probe_prompt: "hi",
  availability_probe_interval_minutes: 30,
  availability_probe_timeout_seconds: 30,
  availability_probe_max_retries: 3,
  availability_probe_user_agent: "",
});

// 草稿默认值必须在目录就绪后建立；空集合是用户配置，不能作为“未初始化”的标记。
function initializeGroupProtocolDefaults(form: {
  allowed_protocols: ProtocolID[];
  protocol_fallbacks: Partial<Record<ProtocolID, ProtocolID[]>>;
}) {
  const profile = protocolCatalog.value?.groups[0];
  if (!profile) return;
  form.allowed_protocols = [...profile.defaults];
  form.protocol_fallbacks = { ...profile.default_fallbacks };
}

// 编辑回显保留服务器配置；只有目录未就绪时的主动平台切换需要延后初始化。
const editProtocolDefaultsPending = ref(false);
watch(protocolCatalog, (catalog, previous) => {
  if (catalog && !previous) initializeGroupProtocolDefaults(createForm);
}, { immediate: true });
watch(protocolCatalog, (catalog) => {
  if (catalog && editProtocolDefaultsPending.value) {
    initializeGroupProtocolDefaults(editForm);
    editProtocolDefaultsPending.value = false;
  }
});

// 根据分组类型返回不同的删除确认消息
const deleteConfirmMessage = computed(() => {
  if (!deletingGroup.value) {
    return "";
  }
  return t("admin.groups.deleteConfirm", { name: deletingGroup.value.name });
});

const loadLiveCapability = async () => {
  if (liveCapability.value) return liveCapability.value;
  if (!liveCapabilityRequest) {
    liveCapabilityRequest = adminAPI.groups
      .getLiveCapability()
      .catch(() => ({ supported: false }))
      .finally(() => {
        liveCapabilityRequest = null;
      });
  }
  liveCapability.value = await liveCapabilityRequest;
  return liveCapability.value ?? { supported: false };
};

const confirmUnsupportedLive = () => {
  if (pendingLiveForm.value === "create") createForm.allow_live = true;
  if (pendingLiveForm.value === "edit") editForm.allow_live = true;
  pendingLiveForm.value = null;
};

const cancelUnsupportedLive = () => {
  pendingLiveForm.value = null;
};

const loadGroups = async () => {
  if (abortController) {
    abortController.abort();
  }
  const currentController = new AbortController();
  abortController = currentController;
  const { signal } = currentController;
  loading.value = true;
  try {
    const response = await adminAPI.groups.list(
      pagination.page,
      pagination.page_size,
      {
        status: filters.status as any,
        search: searchQuery.value.trim() || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order,
      },
      { signal },
    );
    if (signal.aborted) return;
    groups.value = response.items;
    pagination.total = response.total;
    pagination.pages = response.pages;
    if (hasVisibleUsageColumn.value) {
      loadUsageSummary();
    } else {
      usageLoading.value = false;
    }
    if (hasVisibleCapacityColumn.value) {
      loadCapacitySummary();
    }
  } catch (error: any) {
    if (
      signal.aborted ||
      error?.name === "AbortError" ||
      error?.code === "ERR_CANCELED"
    ) {
      return;
    }
    appStore.showError(t("admin.groups.failedToLoad"));
    console.error("Error loading groups:", error);
  } finally {
    if (abortController === currentController && !signal.aborted) {
      loading.value = false;
    }
  }
};

const loadUnavailableFallbackGroups = async () => {
  try {
    unavailableFallbackGroups.value = await adminAPI.groups.getAll();
  } catch (error) {
    console.error("Error loading unavailable fallback groups:", error);
  }
};

const formatGroupBalance = (cost: number | null | undefined): string =>
  formatBalanceAmount(cost, { fractionDigits: 2 });

const normalizeDisplayBrand = (value: string): string => value.trim().slice(0, 50);

const displayBrandLabel = (value: unknown): string =>
  providerBrandDisplayName(String(value || ""));

const displayBrandBadgeClass = (value: unknown): string => {
  const base =
    "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium";
  return `${base} ${resolveProviderBrand(String(value || "")).badgeClass}`;
};

const loadUsageSummary = async () => {
  if (!hasVisibleUsageColumn.value) {
    usageLoading.value = false;
    return;
  }
  usageLoading.value = true;
  try {
    const data = await adminAPI.groups.getUsageSummary();
    const map = new Map<number, { today_cost: number; yesterday_cost: number; total_cost: number }>();
    for (const item of data) {
      map.set(item.group_id, {
        today_cost: item.today_cost,
        yesterday_cost: item.yesterday_cost,
        total_cost: item.total_cost,
      });
    }
    usageMap.value = map;
  } catch (error) {
    console.error("Error loading group usage summary:", error);
  } finally {
    usageLoading.value = false;
  }
};

const loadCapacitySummary = async () => {
  if (!hasVisibleCapacityColumn.value) {
    return;
  }
  try {
    const data = await adminAPI.groups.getCapacitySummary();
    const map = new Map<
      number,
      {
        concurrencyUsed: number;
        concurrencyMax: number;
        sessionsUsed: number;
        sessionsMax: number;
        rpmUsed: number;
        rpmMax: number;
      }
    >();
    for (const item of data) {
      map.set(item.group_id, {
        concurrencyUsed: item.concurrency_used,
        concurrencyMax: item.concurrency_max,
        sessionsUsed: item.sessions_used,
        sessionsMax: item.sessions_max,
        rpmUsed: item.rpm_used,
        rpmMax: item.rpm_max,
      });
    }
    capacityMap.value = map;
  } catch (error) {
    console.error("Error loading group capacity summary:", error);
  }
};

let searchTimeout: ReturnType<typeof setTimeout>;
const handleSearch = () => {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => {
    pagination.page = 1;
    loadGroups();
  }, 300);
};

const handlePageChange = (page: number) => {
  pagination.page = page;
  loadGroups();
};

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize;
  pagination.page = 1;
  loadGroups();
};

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key;
  sortState.sort_order = order;
  pagination.page = 1;
  loadGroups();
};

const openCreateModal = () => {
  showCreateModal.value = true;
  loadModelsListCandidates("create", 0);
};

const closeCreateModal = () => {
  showCreateModal.value = false;
  createModelRoutingRules.value.forEach((rule) => {
    providerSearchRunner.clearKey(getCreateRuleSearchKey(rule));
  });
  clearAllProviderSearchState();
  createForm.localization = undefined;
  createForm.name = "";
  createForm.description = "";
  createForm.display_brand = "";
  createForm.scheduler_type = "basic";
  createForm.advanced_scheduler_overrides = {};
  initializeGroupProtocolDefaults(createForm);
  createForm.responses_image_policy = "inherit";
  createForm.rate_multiplier = 1.0;
  createForm.is_exclusive = false;
  createForm.session_isolation_enabled = false;
  createForm.allow_image_generation = false;
  createForm.allow_batch_image_generation = false;

  createForm.routing_policy = defaultRoutingPolicy();

  createForm.claude_code_only = false;
  createForm.fallback_group_id = null;
  createForm.fallback_group_id_on_invalid_request = null;
  createForm.unavailable_fallback_group_id = null;
  createForm.allow_live = false;
  createForm.openai_fast_policy = "follow_request";

  createForm.require_oauth_only = false;
  createForm.require_privacy_set = false;
  createForm.copy_providers_from_group_ids = [];
  createForm.rpm_limit = 0;
  createForm.max_reasoning_effort = "";
  createForm.max_reasoning_effort_over_limit = reasoningEffortOverLimitDowngrade;
  createForm.reasoning_effort_mappings = [];
  createSettingsRef.value?.resetValidation();
  resetAvailabilityProbeFormState(createForm);
  resetModelsListState(createModelsListState);
  createModelRoutingRules.value = [];
};

// 整份表单统一校验，业务校验失败也要定位到对应页签中的字段。
const validateGroupForm = async (target: "create" | "edit"): Promise<boolean> => {
  const form = target === "create" ? createForm : editForm;
  const tabs = target === "create" ? createSettingsRef.value : editSettingsRef.value;
  if (tabs && !(await tabs.validate())) return false;
  if (!form.name.trim()) {
    appStore.showError(t("admin.groups.nameRequired"));
    await tabs?.revealField('[data-group-field="name"]');
    return false;
  }
  try {
    buildAvailabilityProbeConfig(form);
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error));
    await tabs?.revealField(form.availability_probe_model_id.trim()
      ? '[data-group-field="probe-prompt"]'
      : '[data-group-field="probe-model"]');
    return false;
  }
  return true;
};

const handleCreateGroup = async () => {
  if (!protocolCatalog.value || submitting.value || !(await validateGroupForm("create"))) return;
  if (submitting.value || !showCreateModal.value) return;
  submitting.value = true;
  try {
    const availabilityProbeConfig = buildAvailabilityProbeConfig(createForm);
    // 构建请求数据，包含模型路由配置
    const requestData = {
      ...createForm,
      allow_image_generation: undefined,
      allow_batch_image_generation: undefined,
      allow_live: undefined,
      allowed_protocols: effectiveGroupClientProtocols(
        createForm.allowed_protocols,
      ),
      protocol_fallbacks: sanitizeGroupProtocolFallbacks(
        createForm.protocol_fallbacks,
      ),
      responses_image_policy: createForm.responses_image_policy,
      display_brand: normalizeDisplayBrand(createForm.display_brand),
      routing_policy: createForm.routing_policy,

      model_routing: convertRoutingRulesToApiFormat(
        createModelRoutingRules.value,
      ),
      models_list_config: buildModelsListConfig(createModelsListState),
      availability_probe_config: availabilityProbeConfig,
      openai_fast_policy: normalizeGroupOpenAIFastPolicy(
        createForm.openai_fast_policy,
      ),

      max_reasoning_effort_over_limit: normalizeReasoningEffortOverLimit(
        createForm.max_reasoning_effort_over_limit,
      ),
      reasoning_effort_mappings: reasoningEffortMappingsToAPI(
        createForm.reasoning_effort_mappings,
      ),
    };
    delete (requestData as any).availability_probe_enabled;
    delete (requestData as any).availability_probe_model_id;
    delete (requestData as any).availability_probe_prompt;
    delete (requestData as any).availability_probe_interval_minutes;
    delete (requestData as any).availability_probe_timeout_seconds;
    delete (requestData as any).availability_probe_max_retries;
    delete (requestData as any).availability_probe_user_agent;

    await adminAPI.groups.create(requestData);
    appStore.showSuccess(t("admin.groups.groupCreated"));
    closeCreateModal();
    loadGroups();
    loadUnavailableFallbackGroups();
    // Only advance tour if active, on submit step, and creation succeeded
    if (onboardingStore.isCurrentStep('[data-tour="group-form-submit"]')) {
      onboardingStore.nextStep(500);
    }
    } catch (error: any) {
      appStore.showError(
        extractApiErrorMessage(error, t("admin.groups.failedToCreate")),
      );
    console.error("Error creating group:", error);
    // Don't advance tour on error
  } finally {
    submitting.value = false;
  }
};

const handleEdit = async (group: AdminGroup) => {
  editingGroup.value = group;
  editForm.localization = group.localization ? JSON.parse(JSON.stringify(group.localization)) : undefined;
  editForm.name = group.name;
  editForm.description = group.description || "";
  editForm.display_brand = group.display_brand || "";
  editForm.scheduler_type = group.scheduler_type ?? "basic";
  editForm.advanced_scheduler_overrides = cloneAdvancedSchedulerOverrides(
    group.advanced_scheduler_overrides,
  );
  editForm.rate_multiplier = group.rate_multiplier;
  editForm.is_exclusive = group.is_exclusive;
  editForm.session_isolation_enabled =
    group.session_isolation_enabled ?? false;
  editForm.status = group.status;

  editForm.routing_policy = cloneRoutingPolicy(group.routing_policy);
  editForm.allow_image_generation = group.allowed_protocols?.some(id => ['openai_images_generations','openai_images_edits','image_batches'].includes(id)) ?? false;
  editForm.allow_batch_image_generation =
    group.allowed_protocols?.includes('image_batches') ?? false;

  editForm.claude_code_only = group.claude_code_only || false;
  editForm.fallback_group_id = group.fallback_group_id;
  editForm.fallback_group_id_on_invalid_request =
    group.fallback_group_id_on_invalid_request;
  editForm.unavailable_fallback_group_id =
    group.unavailable_fallback_group_id;
  editForm.allowed_protocols = effectiveGroupClientProtocols(
    group.allowed_protocols,
  );
  editForm.protocol_fallbacks = sanitizeGroupProtocolFallbacks(
    group.protocol_fallbacks,
  );
  editProtocolDefaultsPending.value = false;
  editForm.responses_image_policy = group.responses_image_policy ?? "inherit";
  editForm.allow_live = group.allowed_protocols?.includes('openai_live') ?? false;
  editForm.openai_fast_policy = normalizeGroupOpenAIFastPolicy(
    group.openai_fast_policy ?? (group.force_openai_fast ? "force_priority" : "follow_request"),
  );

  editForm.require_oauth_only = group.require_oauth_only ?? false;
  editForm.require_privacy_set = group.require_privacy_set ?? false;
  editForm.model_routing_enabled = group.model_routing_enabled || false;
  editForm.copy_providers_from_group_ids = []; // 复制提供商字段每次编辑时重置为空
  editForm.rpm_limit = group.rpm_limit ?? 0;
  editForm.max_reasoning_effort = normalizeReasoningEffortForPlatform(
    group.max_reasoning_effort,
  );
  editForm.max_reasoning_effort_over_limit = normalizeReasoningEffortOverLimit(
    group.max_reasoning_effort_over_limit,
  );
  editForm.reasoning_effort_mappings = reasoningEffortMappingsToRows(
    group.reasoning_effort_mappings,
  );
  resetAvailabilityProbeFormState(editForm, group.availability_probe_config);
  resetModelsListState(editModelsListState, group.models_list_config);
  // 加载模型路由规则（异步加载提供商名称）
  editModelRoutingRules.value = await convertApiFormatToRoutingRules(
    group.model_routing,
  );
  loadModelsListCandidates("edit", group.id);
  showEditModal.value = true;
};

const closeEditModal = () => {
  editModelRoutingRules.value.forEach((rule) => {
    providerSearchRunner.clearKey(getEditRuleSearchKey(rule));
  });
  clearAllProviderSearchState();
  showEditModal.value = false;
  editingGroup.value = null;
  editForm.max_reasoning_effort = "";
  editForm.max_reasoning_effort_over_limit = reasoningEffortOverLimitDowngrade;
  editForm.reasoning_effort_mappings = [];
  editSettingsRef.value?.resetValidation();
  editModelRoutingRules.value = [];
  editForm.scheduler_type = "basic";
  editForm.advanced_scheduler_overrides = {};
  editForm.session_isolation_enabled = false;
  editForm.unavailable_fallback_group_id = null;
  editForm.copy_providers_from_group_ids = [];
  resetAvailabilityProbeFormState(editForm);

  editForm.routing_policy = defaultRoutingPolicy();

  editForm.allow_live = false;
  editForm.openai_fast_policy = "follow_request";

  resetModelsListState(editModelsListState);
};

const handleUpdateGroup = async () => {
  if (!editingGroup.value) return;
  if (!protocolCatalog.value || submitting.value || !(await validateGroupForm("edit"))) return;
  if (submitting.value || !showEditModal.value || !editingGroup.value) return;

  submitting.value = true;
  try {
    const availabilityProbeConfig = buildAvailabilityProbeConfig(editForm);
    // 转换 fallback_group_id: null -> 0 (后端使用 0 表示清除)
    const payload = {
      ...editForm,
      allow_image_generation: undefined,
      allow_batch_image_generation: undefined,
      allow_live: undefined,
      allowed_protocols: effectiveGroupClientProtocols(
        editForm.allowed_protocols,
      ),
      protocol_fallbacks: sanitizeGroupProtocolFallbacks(
        editForm.protocol_fallbacks,
      ),
      responses_image_policy: editForm.responses_image_policy,
      display_brand: normalizeDisplayBrand(editForm.display_brand),
      routing_policy: editForm.routing_policy,

      fallback_group_id:
        editForm.fallback_group_id === null ? 0 : editForm.fallback_group_id,
      fallback_group_id_on_invalid_request:
        editForm.fallback_group_id_on_invalid_request === null
          ? 0
          : editForm.fallback_group_id_on_invalid_request,
      unavailable_fallback_group_id:
        editForm.unavailable_fallback_group_id === null
          ? 0
          : editForm.unavailable_fallback_group_id,
      model_routing: convertRoutingRulesToApiFormat(
        editModelRoutingRules.value,
      ),
      models_list_config: buildModelsListConfig(editModelsListState),
      availability_probe_config: availabilityProbeConfig,
      openai_fast_policy: normalizeGroupOpenAIFastPolicy(
        editForm.openai_fast_policy,
      ),

      max_reasoning_effort_over_limit: normalizeReasoningEffortOverLimit(
        editForm.max_reasoning_effort_over_limit,
      ),
      reasoning_effort_mappings: reasoningEffortMappingsToAPI(
        editForm.reasoning_effort_mappings,
      ),
    };
    delete (payload as any).availability_probe_enabled;
    delete (payload as any).availability_probe_model_id;
    delete (payload as any).availability_probe_prompt;
    delete (payload as any).availability_probe_interval_minutes;
    delete (payload as any).availability_probe_timeout_seconds;
    delete (payload as any).availability_probe_max_retries;
    delete (payload as any).availability_probe_user_agent;

    await adminAPI.groups.update(editingGroup.value.id, payload);
    appStore.showSuccess(t("admin.groups.groupUpdated"));
    closeEditModal();
    loadGroups();
    loadUnavailableFallbackGroups();
    } catch (error: any) {
      appStore.showError(
        extractApiErrorMessage(error, t("admin.groups.failedToUpdate")),
      );
    console.error("Error updating group:", error);
  } finally {
    submitting.value = false;
  }
};

const handleRateMultipliers = (group: AdminGroup) => {
  rateMultipliersGroup.value = group;
  showRateMultipliersModal.value = true;
};

const handleRPMOverrides = (group: AdminGroup) => {
  rpmOverridesGroup.value = group;
  showRPMOverridesModal.value = true;
};

const handleDuplicate = async (group: AdminGroup) => {
  if (duplicatingGroupIds.has(group.id)) return;

  duplicatingGroupIds.add(group.id);
  try {
    const duplicate = await adminAPI.groups.duplicate(group.id);
    appStore.showSuccess(
      t("admin.groups.duplicateSuccess", { name: duplicate.name }),
    );
    await loadGroups();
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(error, t("admin.groups.duplicateFailed")),
    );
  } finally {
    duplicatingGroupIds.delete(group.id);
  }
};

const handleDelete = (group: AdminGroup) => {
  deletingGroup.value = group;
  showDeleteDialog.value = true;
};

const confirmDelete = async () => {
  if (!deletingGroup.value) return;

  try {
    await adminAPI.groups.delete(deletingGroup.value.id);
    appStore.showSuccess(t("admin.groups.groupDeleted"));
    showDeleteDialog.value = false;
    deletingGroup.value = null;
    loadGroups();
    loadUnavailableFallbackGroups();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail || t("admin.groups.failedToDelete"),
    );
    console.error("Error deleting group:", error);
  }
};

watch(createAvailabilityProbeModelOptions, (options) => {
  if (!createModelsListState.enabled && createModelsListState.items.length === 0) {
    return;
  }
  if (
    !isAvailabilityProbeModelAvailable(
      createForm.availability_probe_model_id,
      options,
    )
  ) {
    createForm.availability_probe_model_id = "";
  }
});

watch(editAvailabilityProbeModelOptions, (options) => {
  if (!editModelsListState.enabled && editModelsListState.items.length === 0) {
    return;
  }
  if (
    !isAvailabilityProbeModelAvailable(
      editForm.availability_probe_model_id,
      options,
    )
  ) {
    editForm.availability_probe_model_id = "";
  }
});

// 点击外部关闭提供商搜索下拉框
const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement;
  // 检查是否点击在下拉框或输入框内
  if (!target.closest(".provider-search-container")) {
    Object.keys(showProviderDropdown.value).forEach((key) => {
      showProviderDropdown.value[key] = false;
    });
  }
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(target)) {
    showColumnDropdown.value = false;
  }
};

// 打开排序弹窗
const openSortModal = async () => {
  try {
    // 获取所有分组（不分页）
    const allGroups = await adminAPI.groups.getAll();
    // 按 sort_order 排序
    sortableGroups.value = [...allGroups].sort(
      (a, b) => a.sort_order - b.sort_order,
    );
    showSortModal.value = true;
  } catch (error) {
    appStore.showError(t("admin.groups.failedToLoad"));
    console.error("Error loading groups for sorting:", error);
  }
};

// 关闭排序弹窗
const closeSortModal = () => {
  showSortModal.value = false;
  sortableGroups.value = [];
};

// 保存排序
const saveSortOrder = async () => {
  sortSubmitting.value = true;
  try {
    const updates = sortableGroups.value.map((g, index) => ({
      id: g.id,
      sort_order: index * 10,
    }));
    await adminAPI.groups.updateSortOrder(updates);
    appStore.showSuccess(t("admin.groups.sortOrderUpdated"));
    closeSortModal();
    loadGroups();
    loadUnavailableFallbackGroups();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail || t("admin.groups.failedToUpdateSortOrder"),
    );
    console.error("Error updating sort order:", error);
  } finally {
    sortSubmitting.value = false;
  }
};

onMounted(async () => {
  try { await loadProtocolCatalog(); } catch { appStore.showError(t("admin.protocols.loadError")); }
  loadGroups();
  loadUnavailableFallbackGroups();
  void loadLiveCapability();
  loadModelsListCandidates("create", 0);
  document.addEventListener("click", handleClickOutside);
});

onUnmounted(() => {
  document.removeEventListener("click", handleClickOutside);
  providerSearchRunner.clearAll();
  clearAllProviderSearchState();
});
</script>
