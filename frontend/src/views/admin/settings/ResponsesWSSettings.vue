<template>
  <SettingsCard :title="t('admin.settings.responsesWS.title')" :description="t('admin.settings.responsesWS.description')" data-testid="responses-ws-settings">
    <template #actions>
      <button type="button" class="btn btn-secondary btn-sm h-9 shrink-0" :disabled="!canRestore" @click="restore">
        <Icon name="undo" size="sm" />
        {{ t('admin.settings.responsesWS.restore') }}
      </button>
    </template>
    <SettingsNotice v-if="error" tone="error">
      <p>{{ error }}</p>
      <button type="button" class="font-medium underline underline-offset-2" @click="load">{{ t('admin.settings.responsesWS.retry') }}</button>
    </SettingsNotice>
    <template v-if="effective">
      <SettingsNotice v-if="resetAll" tone="warning">{{ t('admin.settings.responsesWS.restoring') }}</SettingsNotice>
      <SettingsNotice v-else tone="info">{{ t('admin.settings.responsesWS.applies') }}</SettingsNotice>
      <SettingsSection
        v-for="group in basicGroups"
        :key="group.key"
        :title="t(`admin.settings.responsesWS.groups.${group.key}.title`)"
        :hint="group.hint ? t(`admin.settings.responsesWS.groups.${group.key}.hint`) : undefined"
      >
        <div :class="responsesWsGridClass(group.fields.length)">
          <ResponsesWSNumberField
            v-for="field in group.fields"
            :key="field.key"
            v-bind="numberFieldProps(field)"
            @update:model-value="setNumber(field, $event)"
            @reset="setValue(field.key, null)"
          />
        </div>
      </SettingsSection>
      <section class="settings-section">
        <Disclosure summary-class="flex w-full flex-wrap items-baseline gap-x-2 gap-y-1 rounded-compact text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">
          <template #summary>
            <span class="text-sm font-semibold text-primary-900 dark:text-dark-50">{{ t('admin.settings.responsesWS.advanced') }}</span>
            <span class="text-xs text-primary-900/80 dark:text-dark-300">{{ t('admin.settings.responsesWS.advancedHint') }}</span>
          </template>
          <div class="space-y-6 pt-6">
            <SettingsSection v-for="group in advancedGroups" :key="group.key" :title="t(`admin.settings.responsesWS.groups.${group.key}.title`)">
              <SettingToggleRow
                v-if="group.toggle"
                :id="`responses-ws-${group.toggle}`"
                :model-value="booleanValue(group.toggle)"
                :label="t(`admin.settings.responsesWS.fields.${group.toggle}`)"
                :hint="t(`admin.settings.responsesWS.hints.${group.toggle}`)"
                :disabled="saving"
                @update:model-value="setValue(group.toggle, $event)"
              />
              <Collapse :open="!group.dependent || !group.toggle || booleanValue(group.toggle)">
                <component :is="group.dependent ? SettingsSubpanel : 'div'">
                  <div :class="responsesWsGridClass(group.fields.length)">
                    <ResponsesWSNumberField
                      v-for="field in group.fields"
                      :key="field.key"
                      v-bind="numberFieldProps(field)"
                      @update:model-value="setNumber(field, $event)"
                      @reset="setValue(field.key, null)"
                    />
                  </div>
                </component>
              </Collapse>
            </SettingsSection>
          </div>
        </Disclosure>
      </section>
    </template>
  </SettingsCard>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSettings, updateSettings, type ResponsesWSParameters, type ResponsesWSOverrides } from '@/api/admin/settings'
import { useSettingsSaveTarget } from '@/composables/useSettingsSaveRegistry'
import { useDirtyTracker } from '@/composables/useDirtyTracker'
import { extractApiErrorMessage } from '@/utils/apiError'
import Collapse from '@/components/common/Collapse.vue'
import Disclosure from '@/components/common/Disclosure.vue'
import Icon from '@/components/icons/Icon.vue'
import SettingsCard from '@/components/common/settings/SettingsCard.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import SettingsNotice from '@/components/common/settings/SettingsNotice.vue'
import SettingsSubpanel from '@/components/common/settings/SettingsSubpanel.vue'
import SettingToggleRow from '@/components/common/settings/SettingToggleRow.vue'
import ResponsesWSNumberField from './ResponsesWSNumberField.vue'
import { responsesWsGridClass, responsesWsGroups, responsesWsKeys, type ResponsesWSBooleanKey, type ResponsesWSNumberField as NumberField } from './responsesWsFields'

const { t } = useI18n()
const basicGroups = responsesWsGroups.filter(group => !group.advanced)
const advancedGroups = responsesWsGroups.filter(group => group.advanced)
const effective = ref<ResponsesWSParameters>()
const saved = ref<ResponsesWSOverrides>({})
const patch = ref<ResponsesWSOverrides>({})
const resetAll = ref(false)
const saving = ref(false)
const error = ref('')
const proposedOverrides = computed(() => {
  if (resetAll.value) return {}
  const next = { ...saved.value, ...patch.value }
  return Object.fromEntries(Object.entries(next).filter(([, value]) => value != null).sort(([left], [right]) => left.localeCompare(right)))
})
const { dirty, markClean } = useDirtyTracker({ responsesWS: () => proposedOverrides.value })
// 有覆盖值时才需要“全部恢复默认”。
const canRestore = computed(() => Boolean(effective.value) && !saving.value && Object.keys(proposedOverrides.value).length > 0)

// 仅发送用户修改的字段，值为 null 表示移除该项覆盖。
function setValue(key: keyof ResponsesWSParameters, value: number | boolean | null) {
  if (resetAll.value) {
    patch.value = Object.fromEntries(responsesWsKeys.map(item => [item, null]))
    resetAll.value = false
  }
  patch.value = { ...patch.value, [key]: value }
}
// 清空输入框等同于移除该项覆盖。
function setNumber(field: NumberField, text: string) {
  setValue(field.key, text === '' ? null : Number(text) * field.scale)
}
// overrideValue 返回字段当前的覆盖值，没有覆盖时返回 null。
function overrideValue(key: keyof ResponsesWSParameters) {
  if (resetAll.value) return null
  const value = Object.prototype.hasOwnProperty.call(patch.value, key) ? patch.value[key] : saved.value[key]
  return value ?? null
}
// numberFieldProps 生成数字字段的值、占位文字和覆盖状态。
function numberFieldProps(field: NumberField) {
  const value = overrideValue(field.key)
  return {
    field,
    modelValue: value == null ? '' as const : Number(value) / field.scale,
    placeholder: String(Number(effective.value?.[field.key]) / field.scale),
    overridden: value != null,
    disabled: saving.value,
  }
}
// booleanValue 返回开关显示的值：有覆盖值时用覆盖值，否则用当前有效值。
function booleanValue(key: ResponsesWSBooleanKey) {
  return Boolean(overrideValue(key) ?? effective.value?.[key])
}
function restore() { resetAll.value = true; patch.value = {} }
async function load() {
  try {
    const value = await getSettings()
    effective.value = value.responses_ws_effective
    saved.value = value.responses_ws ?? {}
    patch.value = {}
    resetAll.value = false
    error.value = ''
    markClean('responsesWS')
  } catch { error.value = t('admin.settings.responsesWS.loadError') }
}
async function save() {
  saving.value = true
  error.value = ''
  try {
    await updateSettings({ responses_ws: resetAll.value ? null : patch.value })
    await load()
    return !error.value
  } catch (cause) {
    error.value = extractApiErrorMessage(cause, t('admin.settings.responsesWS.saveError'), {
      SETTINGS_APPLY_FAILED: t('admin.settings.responsesWS.applyError')
    })
    return false
  } finally { saving.value = false }
}
useSettingsSaveTarget('responses-ws', { dirty, save, discard: load })
onMounted(load)
</script>
