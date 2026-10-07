<template>
  <div>
    <SettingRow
      id="announcement-targeting"
      :label="t('admin.announcements.form.targetingMode')"
      :hint="mode === 'all' ? t('admin.announcements.form.targetingAllHint') : t('admin.announcements.form.targetingCustomHint')"
    >
      <SettingsSegmented
        v-model="mode"
        :options="modeOptions"
        :ariaLabel="t('admin.announcements.form.targetingMode')"
      />
    </SettingRow>

    <!-- 收起过程中冻结条件列表，过渡期间显示收起前的内容 -->
    <Collapse :open="mode === 'custom'" unmount-on-hide>
      <div class="pt-4">
        <RuleListEditor
          :items="anyOf"
          :item-key="targetingRowKey"
          :add-label="t('admin.announcements.form.addOrGroup')"
          add-placement="footer"
          :max="50"
          :error="validationError"
          variant="card"
          :item-label="(index) => t('admin.announcements.form.conditionGroupItem', { index: index + 1 })"
          test-id="announcement-groups"
          @add="addOrGroup"
          @remove="removeOrGroup"
        >
          <template #row="{ item: group, index: groupIndex }">
            <RuleListEditor
              :items="group.all_of || []"
              :item-key="targetingRowKey"
              :add-label="t('admin.announcements.form.addAndCondition')"
              add-placement="footer"
              :max="50"
              :test-id="`announcement-conditions-${groupIndex}`"
              @add="addAndCondition(groupIndex)"
              @remove="removeAndCondition(groupIndex, $event)"
            >
              <!-- 每个条件占一行：左侧选类型，右侧填套餐或余额阈值 -->
              <template #row="{ item: cond, index: condIndex }">
                <div class="flex flex-col gap-2 sm:flex-row sm:items-start">
                  <div class="w-full shrink-0 sm:w-36">
                    <Select
                      :model-value="cond.type"
                      :options="conditionTypeOptions"
                      :aria-label="t('admin.announcements.form.conditionType')"
                      @update:model-value="(v) => setConditionType(groupIndex, condIndex, v as any)"
                    />
                  </div>
                  <div
                    v-if="cond.type === 'subscription'"
                    class="flex min-w-0 flex-1 flex-wrap items-center gap-2 sm:min-h-9"
                    role="group"
                    :aria-label="t('admin.announcements.form.selectPackages')"
                  >
                    <button
                      v-for="plan in plans"
                      :key="plan.id"
                      type="button"
                      :aria-pressed="isPlanSelected(groupIndex, condIndex, plan.id)"
                      :class="['plan-chip', isPlanSelected(groupIndex, condIndex, plan.id) && 'plan-chip-active']"
                      @click="togglePlanSelection(groupIndex, condIndex, plan.id, !isPlanSelected(groupIndex, condIndex, plan.id))"
                    >
                      <Icon
                        v-if="isPlanSelected(groupIndex, condIndex, plan.id)"
                        name="check"
                        size="xs"
                        :stroke-width="2.5"
                        :animate-on-hover="false"
                      />
                      <span>{{ plan.name }}</span>
                      <span class="plan-chip-meta">{{ plan.validity_days }}{{ t('payment.days') }}</span>
                    </button>
                    <span v-if="plans.length === 0" class="text-sm text-gray-500 dark:text-dark-400">
                      {{ t('admin.announcements.form.noPlans') }}
                    </span>
                  </div>
                  <div v-else class="flex min-w-0 flex-1 gap-2">
                    <div class="w-24 shrink-0">
                      <Select
                        :model-value="cond.operator"
                        :options="balanceOperatorOptions"
                        :aria-label="t('admin.announcements.form.operator')"
                        @update:model-value="(v) => setOperator(groupIndex, condIndex, v as any)"
                      />
                    </div>
                    <input
                      :value="String(cond.value ?? '')"
                      type="number"
                      step="any"
                      class="input min-w-0 flex-1"
                      :placeholder="t('admin.announcements.form.balanceValue')"
                      :aria-label="t('admin.announcements.form.balanceValue')"
                      @input="(e) => setBalanceValue(groupIndex, condIndex, (e.target as HTMLInputElement).value)"
                    />
                  </div>
                </div>
              </template>
            </RuleListEditor>
          </template>
        </RuleListEditor>
      </div>
    </Collapse>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, toRaw, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  AnnouncementTargeting,
  AnnouncementCondition,
  AnnouncementConditionGroup,
  AnnouncementConditionType,
  AnnouncementOperator,
  SubscriptionPlan
} from '@/types'

import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import RuleListEditor from '@/components/common/RuleListEditor.vue'
import Collapse from '@/components/common/Collapse.vue'
import SettingRow from '@/components/common/settings/SettingRow.vue'
import SettingsSegmented, { type SettingsSegmentedOption } from '@/components/common/settings/SettingsSegmented.vue'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'

const { t } = useI18n()

const props = defineProps<{
  modelValue: AnnouncementTargeting
  plans: SubscriptionPlan[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: AnnouncementTargeting): void
}>()

const anyOf = computed(() => props.modelValue?.any_of ?? [])

type Mode = 'all' | 'custom'
// 条件组为空表示所有用户。切换模式时改写 targeting。
const mode = computed<Mode>({
  get: () => (anyOf.value.length === 0 ? 'all' : 'custom'),
  set: setMode
})

const modeOptions = computed<SettingsSegmentedOption<Mode>[]>(() => [
  { value: 'all', label: t('admin.announcements.form.targetingAll'), icon: 'users' },
  { value: 'custom', label: t('admin.announcements.form.targetingCustom'), icon: 'filter' }
])

const conditionTypeOptions = computed(() => [
  { value: 'subscription', label: t('admin.announcements.form.conditionSubscription') },
  { value: 'balance', label: t('admin.announcements.form.conditionBalance') }
])

const balanceOperatorOptions = computed(() => [
  { value: 'gt', label: t('admin.announcements.operators.gt') },
  { value: 'gte', label: t('admin.announcements.operators.gte') },
  { value: 'lt', label: t('admin.announcements.operators.lt') },
  { value: 'lte', label: t('admin.announcements.operators.lte') },
  { value: 'eq', label: t('admin.announcements.operators.eq') }
])

function setMode(next: Mode) {
  if (next === 'all') {
    emit('update:modelValue', { any_of: [] })
    return
  }
  if (anyOf.value.length === 0) {
    emit('update:modelValue', { any_of: [{ all_of: [defaultSubscriptionCondition()] }] })
  }
}

function defaultSubscriptionCondition(): AnnouncementCondition {
  return {
    type: 'subscription' as AnnouncementConditionType,
    operator: 'in' as AnnouncementOperator,
    plan_ids: []
  }
}

function defaultBalanceCondition(): AnnouncementCondition {
  return {
    type: 'balance' as AnnouncementConditionType,
    operator: 'gte' as AnnouncementOperator,
    value: 0
  }
}

type TargetingDraft = {
  any_of: AnnouncementConditionGroup[]
}

// 展示 key 保存在 WeakMap 中，不写入公告的提交数据。
const inheritedRowKeys = new WeakMap<object, string>()
const newRowKey = createStableObjectKeyResolver<object>('announcement-targeting')

function targetingRowKey(row: object): string {
  const raw = toRaw(row)
  return inheritedRowKeys.get(raw) ?? newRowKey(raw)
}

// 克隆后的组和条件继承原行身份，编辑字段时保留输入焦点。
function cloneTargeting(): TargetingDraft {
  const current = props.modelValue ?? { any_of: [] }
  const draft: TargetingDraft = JSON.parse(JSON.stringify(current))
  draft.any_of ??= []
  draft.any_of.forEach((group, groupIndex) => {
    const previous = current.any_of?.[groupIndex]
    if (!previous) return
    inheritedRowKeys.set(group, targetingRowKey(previous))
    group.all_of?.forEach((condition, condIndex) => {
      const previousCondition = previous.all_of?.[condIndex]
      if (previousCondition) inheritedRowKeys.set(condition, targetingRowKey(previousCondition))
    })
  })
  return draft
}

function updateTargeting(mutator: (draft: TargetingDraft) => void) {
  const draft = cloneTargeting()
  mutator(draft)
  emit('update:modelValue', draft)
}

function addOrGroup() {
  updateTargeting((draft) => {
    if (draft.any_of.length >= 50) return
    draft.any_of.push({ all_of: [defaultSubscriptionCondition()] })
  })
}

function removeOrGroup(groupIndex: number) {
  updateTargeting((draft) => {
    draft.any_of.splice(groupIndex, 1)
  })
}

function addAndCondition(groupIndex: number) {
  updateTargeting((draft) => {
    const group = draft.any_of[groupIndex]
    if (!group.all_of) group.all_of = []
    if (group.all_of.length >= 50) return
    group.all_of.push(defaultSubscriptionCondition())
  })
}

function removeAndCondition(groupIndex: number, condIndex: number) {
  updateTargeting((draft) => {
    const group = draft.any_of[groupIndex]
    if (!group?.all_of) return
    group.all_of.splice(condIndex, 1)
  })
}

function setConditionType(groupIndex: number, condIndex: number, nextType: AnnouncementConditionType) {
  updateTargeting((draft) => {
    const group = draft.any_of[groupIndex]
    if (!group?.all_of) return

    if (nextType === 'subscription') {
      group.all_of[condIndex] = defaultSubscriptionCondition()
    } else {
      group.all_of[condIndex] = defaultBalanceCondition()
    }
  })
}

function setOperator(groupIndex: number, condIndex: number, op: AnnouncementOperator) {
  updateTargeting((draft) => {
    const group = draft.any_of[groupIndex]
    if (!group?.all_of) return

    const cond = group.all_of[condIndex]
    if (!cond) return

    cond.operator = op
  })
}

function setBalanceValue(groupIndex: number, condIndex: number, raw: string) {
  const n = raw === '' ? 0 : Number(raw)
  updateTargeting((draft) => {
    const group = draft.any_of[groupIndex]
    if (!group?.all_of) return

    const cond = group.all_of[condIndex]
    if (!cond) return

    cond.value = Number.isFinite(n) ? n : 0
  })
}

// We keep plan_ids selection in a parallel reactive map and mirror it back to targeting.plan_ids.
const subscriptionSelections = reactive<Record<number, Record<number, number[]>>>({})

function ensureSelectionPath(groupIndex: number, condIndex: number) {
  if (!subscriptionSelections[groupIndex]) subscriptionSelections[groupIndex] = {}
  if (!subscriptionSelections[groupIndex][condIndex]) subscriptionSelections[groupIndex][condIndex] = []
}

function isPlanSelected(groupIndex: number, condIndex: number, planID: number): boolean {
  return subscriptionSelections[groupIndex]?.[condIndex]?.includes(planID) ?? false
}

function togglePlanSelection(groupIndex: number, condIndex: number, planID: number, checked: boolean) {
  ensureSelectionPath(groupIndex, condIndex)
  const current = subscriptionSelections[groupIndex][condIndex] ?? []
  subscriptionSelections[groupIndex][condIndex] = checked
    ? [...current, planID]
    : current.filter((id) => id !== planID)
}

// Sync from modelValue to subscriptionSelections (one-way: model -> local state)
watch(
  () => props.modelValue,
  (v) => {
    const groups = v?.any_of ?? []
    for (let gi = 0; gi < groups.length; gi++) {
      const allOf = groups[gi]?.all_of ?? []
      for (let ci = 0; ci < allOf.length; ci++) {
        const c = allOf[ci]
        if (c?.type === 'subscription') {
          ensureSelectionPath(gi, ci)
          const newIds = (c.plan_ids ?? []).slice()
          const currentIds = subscriptionSelections[gi]?.[ci] ?? []
          if (JSON.stringify(newIds.sort()) !== JSON.stringify(currentIds.sort())) {
            subscriptionSelections[gi][ci] = newIds
          }
        }
      }
    }
  },
  { immediate: true }
)

// Sync from subscriptionSelections to modelValue (one-way: local state -> model)
// Use a debounced approach to avoid infinite loops
let syncTimeout: ReturnType<typeof setTimeout> | null = null
watch(
  () => subscriptionSelections,
  () => {
    // Debounce the sync to avoid rapid fire updates
    if (syncTimeout) clearTimeout(syncTimeout)

    syncTimeout = setTimeout(() => {
      // Build the new targeting state
      const newTargeting = cloneTargeting()

      const groups = newTargeting.any_of ?? []
      for (let gi = 0; gi < groups.length; gi++) {
        const allOf = groups[gi]?.all_of ?? []
        for (let ci = 0; ci < allOf.length; ci++) {
          const c = allOf[ci]
          if (c?.type === 'subscription') {
            ensureSelectionPath(gi, ci)
            c.operator = 'in' as AnnouncementOperator
            c.plan_ids = (subscriptionSelections[gi]?.[ci] ?? []).slice()
          }
        }
      }

      // Only emit if there's an actual change (deep comparison)
      if (JSON.stringify(props.modelValue) !== JSON.stringify(newTargeting)) {
        emit('update:modelValue', newTargeting)
      }
    }, 0)
  },
  { deep: true }
)

const validationError = computed(() => {
  if (mode.value !== 'custom') return ''

  const groups = anyOf.value
  if (groups.length === 0) return t('admin.announcements.form.addOrGroup')

  if (groups.length > 50) return 'any_of > 50'

  for (const g of groups) {
    const allOf = g?.all_of ?? []
    if (allOf.length === 0) return t('admin.announcements.form.addAndCondition')
    if (allOf.length > 50) return 'all_of > 50'

    for (const c of allOf) {
      if (c.type === 'subscription') {
        if (!c.plan_ids || c.plan_ids.length === 0) return t('admin.announcements.form.packagesRequired')
      }
    }
  }

  return ''
})
</script>

<style scoped>
/* 套餐用可多选的胶囊，选中时带品牌色描边和淡底。 */
.plan-chip {
  @apply inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm;
  @apply border-primary-900/10 bg-white text-gray-700 hover:border-black/20;
  @apply dark:border-dark-600 dark:bg-dark-950 dark:text-dark-100 dark:hover:border-dark-500;
  @apply transition-colors duration-fast;
}

.plan-chip-meta {
  @apply text-xs text-gray-400 dark:text-dark-400;
}

.plan-chip.plan-chip-active {
  @apply border-primary-500 bg-primary-500/8 text-primary-700;
  @apply dark:border-primary-500 dark:bg-primary-500/15 dark:text-primary-400;
}

.plan-chip-active .plan-chip-meta {
  @apply text-primary-600/70 dark:text-primary-400/70;
}
</style>
