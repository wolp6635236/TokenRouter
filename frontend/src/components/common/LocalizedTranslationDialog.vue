<template>
  <BaseDialog
    :show="show"
    :title="t('localization.dialogTitle', { field: title })"
    :subtitle="t('localization.dialogHint')"
    width="wide"
    :body-scroll="false"
    flush
    :z-index="Z_INDEX.MODAL_NESTED"
    @close="emit('close')"
  >
    <div v-if="draft" class="flex min-h-0 flex-1 flex-col overflow-y-auto md:h-[32rem] md:flex-initial md:flex-row md:overflow-hidden">
      <!-- 左栏列出原文和每种语言的状态，点选后在右栏编辑。 -->
      <aside
        :aria-label="t('localization.languages')"
        class="flex shrink-0 flex-col gap-4 border-b border-gray-200 bg-gray-50/70 p-3 dark:border-dark-600 dark:bg-dark-950 sm:p-4 md:w-60 md:overflow-y-auto md:border-b-0 md:border-r"
      >
        <div class="space-y-1">
          <p class="px-3 text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('localization.original') }}</p>
          <button
            type="button"
            class="flex w-full items-center gap-2 rounded-control px-3 py-2 text-left text-sm transition-colors duration-fast"
            :class="itemClass('source')"
            :aria-current="selected === 'source' || undefined"
            @click="selected = 'source'"
          >
            <Icon :name="draft.source_locale ? 'document' : 'exclamationTriangle'" size="sm" :class="draft.source_locale ? '' : 'text-amber-600 dark:text-amber-400'" :animate-on-hover="false" />
            <span class="min-w-0 flex-1 truncate">{{ draft.source_locale ? languageName(draft.source_locale) : t('localization.originalLanguageUnset') }}</span>
          </button>
        </div>
        <div class="space-y-1">
          <p class="px-3 text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('localization.translations') }}</p>
          <button
            v-for="item in targets"
            :key="item.code"
            type="button"
            class="flex w-full items-center gap-2 rounded-control px-3 py-2 text-left text-sm transition-colors duration-fast"
            :class="itemClass(item.code)"
            :aria-current="selected === item.code || undefined"
            @click="selected = item.code"
          >
            <Icon :name="STATUS_ICONS[item.status]" size="sm" :class="STATUS_CLASSES[item.status]" :animate-on-hover="false" />
            <span class="min-w-0 flex-1 truncate">{{ item.name }}</span>
            <span class="shrink-0 text-xs" :class="STATUS_CLASSES[item.status]">{{ t(`localization.status.${item.status}`) }}</span>
          </button>
        </div>
      </aside>

      <section class="flex min-w-0 flex-col gap-4 px-4 py-5 sm:px-6 md:min-h-0 md:flex-1 md:overflow-y-auto">
        <!-- 原文：选择原文语言并编辑原文。 -->
        <template v-if="selected === 'source'">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h4 class="text-sm font-semibold text-primary-900 dark:text-dark-50">{{ t('localization.original') }}</h4>
            <Select
              class="w-44"
              :model-value="draft.source_locale || ''"
              :options="languageOptions"
              :placeholder="t('localization.originalLanguage')"
              :aria-label="t('localization.originalLanguage')"
              @update:model-value="changeSourceLocale(String($event))"
            />
          </div>
          <SettingsNotice v-if="conflictLocale" tone="warning">
            <p>{{ t('localization.languageConflict', { language: languageName(conflictLocale) }) }}</p>
            <div class="flex flex-wrap gap-x-4 gap-y-1">
              <button type="button" class="font-medium underline underline-offset-2" @click="keepSource(conflictLocale)">{{ t('localization.keepOriginal') }}</button>
              <button type="button" class="font-medium underline underline-offset-2" @click="promote(conflictLocale)">{{ t('localization.useTranslationAsOriginal', { language: languageName(conflictLocale) }) }}</button>
            </div>
          </SettingsNotice>
          <SettingsNotice v-else-if="!draft.source_locale" tone="warning">{{ t('localization.unknownOriginal') }}</SettingsNotice>
          <p v-else class="text-xs text-gray-500 dark:text-dark-400">{{ t('localization.originalHint') }}</p>
          <slot :value="draft.source" :update="updateDraftSource" :locale="draft.source_locale" :id="`${uid}-source`" />
        </template>

        <!-- 译文：上方是原文参照，下方编辑当前语言。 -->
        <template v-else-if="current">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex min-w-0 items-center gap-2">
              <h4 class="text-sm font-semibold text-primary-900 dark:text-dark-50">{{ current.name }}</h4>
              <span class="inline-flex items-center gap-1 text-xs" :class="STATUS_CLASSES[current.status]">
                <Icon :name="STATUS_ICONS[current.status]" size="xs" :animate-on-hover="false" />
                {{ t(`localization.status.${current.status}`) }}
              </span>
            </div>
            <div v-if="current.status !== 'missing'" class="flex items-center gap-1">
              <button type="button" class="btn btn-ghost btn-sm gap-1" @click="promote(current.code)">
                <Icon name="arrowUp" size="sm" />
                {{ t('localization.useAsOriginal') }}
              </button>
              <button type="button" class="btn btn-ghost btn-sm gap-1 hover:text-red-600 dark:hover:text-red-400" @click="apply(removeTranslation(draft, current.code))">
                <Icon name="trash" size="sm" />
                {{ t('localization.removeTranslation') }}
              </button>
            </div>
          </div>

          <div class="rounded-control border border-primary-900/10 bg-gray-50/70 px-4 py-3 dark:border-dark-600 dark:bg-dark-800/60">
            <p class="mb-2 text-xs font-medium text-gray-500 dark:text-dark-400">
              {{ t('localization.reference', { language: draft.source_locale ? languageName(draft.source_locale) : t('localization.originalLanguageUnset') }) }}
            </p>
            <dl class="max-h-40 space-y-2 overflow-y-auto text-sm">
              <div v-for="entry in referenceEntries" :key="entry.key">
                <dt v-if="entry.label" class="text-xs text-gray-500 dark:text-dark-400">{{ entry.label }}</dt>
                <dd dir="auto" class="whitespace-pre-wrap break-words" :class="entry.value ? 'text-primary-900 dark:text-dark-100' : 'text-gray-400 dark:text-dark-500'">{{ entry.value || t('localization.emptyValue') }}</dd>
              </div>
            </dl>
          </div>

          <div v-if="current.status === 'missing'" class="flex flex-col items-center gap-3 rounded-control border border-dashed border-primary-900/15 px-4 py-8 text-center dark:border-dark-600">
            <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('localization.missingHint', { language: current.name }) }}</p>
            <button type="button" class="btn btn-primary btn-sm gap-1" @click="add(current.code)">
              <Icon name="plus" size="sm" />
              {{ t('localization.startTranslation') }}
            </button>
          </div>
          <template v-else>
            <SettingsNotice v-if="current.status === 'stale'" tone="warning">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span>{{ t('localization.staleNotice') }}</span>
                <button type="button" class="shrink-0 font-medium underline underline-offset-2" @click="apply(markStillValid(draft, current.code))">
                  {{ t('localization.stillValid') }}
                </button>
              </div>
            </SettingsNotice>
            <div ref="editorRef">
              <slot
                :value="draft.translations[current.code].value"
                :update="(value: T) => apply(updateTranslation(draft!, current!.code, value))"
                :locale="current.code"
                :id="`${uid}-${current.code}`"
              />
            </div>
          </template>
        </template>
      </section>
    </div>

    <footer class="flex shrink-0 items-center justify-end gap-2 rounded-b-surface border-t border-gray-200 bg-gray-50/70 px-4 py-3 dark:border-dark-600 dark:bg-dark-950 sm:rounded-b-dialog sm:px-6">
      <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button type="button" class="btn btn-primary" @click="save">{{ t('localization.done') }}</button>
    </footer>
  </BaseDialog>
</template>

<script setup lang="ts" generic="T">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import SettingsNotice from '@/components/common/settings/SettingsNotice.vue'
import Icon from '@/components/icons/Icon.vue'
import { Z_INDEX } from '@/constants/overlay'
import { availableLocales } from '@/i18n/catalog'
import type { LocalizedUpdate } from '@/i18n/content'
import {
  addTranslation,
  adoptSourceLocale,
  markStillValid,
  promoteToOriginal,
  removeTranslation,
  setSourceLocale,
  translationStatus,
  updateSource,
  updateTranslation,
  withDefaultSource,
  type TranslationStatus,
} from '@/i18n/contentEdit'

let dialogSequence = 0

const props = defineProps<{
  show: boolean
  title: string
  modelValue: LocalizedUpdate<T>
  /** 新内容的原文语言，以及原文语言未知时修改原文所用的语言。 */
  fallbackLocale?: string
  /** 多字段内容在原文参照里逐个显示的字段；单个字段省略。 */
  referenceFields?: { key: string; label: string }[]
}>()
const emit = defineEmits<{ close: []; save: [value: LocalizedUpdate<T>] }>()
defineSlots<{ default(props: { value: T; update: (value: T) => void; locale: string | null; id: string }): unknown }>()

const { t } = useI18n()
// eslint-disable-next-line no-useless-assignment -- 后续翻译弹窗继续使用模块计数器。
const uid = `localized-${++dialogSequence}`
const draft = ref<LocalizedUpdate<T>>()
const selected = ref('source')
const conflictLocale = ref('')
const editorRef = ref<HTMLElement>()
const languageOptions = availableLocales.map(item => ({ value: item.code, label: item.name }))

const STATUS_CLASSES: Record<TranslationStatus, string> = {
  translated: 'text-emerald-600 dark:text-emerald-400',
  stale: 'text-amber-600 dark:text-amber-400',
  missing: 'text-gray-400 dark:text-dark-500',
}
const STATUS_ICONS = { translated: 'checkCircle', stale: 'exclamationTriangle', missing: 'circle' } as const

// 原文语言以外的每种语言都列出来，未翻译的语言也可以直接选中。
const targets = computed(() => {
  const content = draft.value
  if (!content) return []
  return availableLocales
    .filter(item => item.code !== content.source_locale)
    .map(item => ({ code: item.code, name: item.name, status: translationStatus(content, item.code) }))
})
const current = computed(() => targets.value.find(item => item.code === selected.value))

// 原文参照按字段展开，单个字段直接显示文本。
const referenceEntries = computed(() => {
  const source = draft.value?.source as unknown
  if (props.referenceFields && source && typeof source === 'object') {
    const values = source as Record<string, unknown>
    return props.referenceFields.map(field => ({ key: field.key, label: field.label, value: String(values[field.key] ?? '') }))
  }
  return [{ key: 'value', label: '', value: String(source ?? '') }]
})

// 每次打开都从外层表单的当前值复制草稿，取消时直接丢弃。
watch(() => props.show, open => {
  if (!open) return
  const content = withDefaultSource(JSON.parse(JSON.stringify(props.modelValue)) as LocalizedUpdate<T>, props.fallbackLocale)
  draft.value = content
  conflictLocale.value = ''
  selected.value = initialSelection(content)
}, { immediate: true })

// 打开时停在最需要处理的位置：原文语言未知、过期译文、已有译文，最后是第一种未翻译的语言。
function initialSelection(content: LocalizedUpdate<T>): string {
  if (!content.source_locale) return 'source'
  const codes = availableLocales.map(item => item.code).filter(code => code !== content.source_locale)
  return codes.find(code => translationStatus(content, code) === 'stale')
    || codes.find(code => translationStatus(content, code) === 'translated')
    || codes[0]
    || 'source'
}

function languageName(code: string): string {
  return availableLocales.find(item => item.code === code)?.name || code
}
function itemClass(key: string): string {
  return selected.value === key
    ? 'bg-primary-100 text-primary-700 dark:bg-primary-500/8 dark:text-primary-500'
    : 'text-gray-700 hover:bg-gray-100 dark:text-dark-100 dark:hover:bg-dark-800'
}
function apply(next: LocalizedUpdate<T>): void {
  draft.value = next
}
function updateDraftSource(value: T): void {
  apply(updateSource(draft.value!, value, props.fallbackLocale))
}
// 选中的语言已有译文时，先让管理员决定保留哪一份作为原文。
function changeSourceLocale(code: string): void {
  if (!code) return
  const result = setSourceLocale(draft.value!, code)
  conflictLocale.value = result.conflict ? code : ''
  apply(result.content)
}
function keepSource(code: string): void {
  apply(adoptSourceLocale(draft.value!, code))
  conflictLocale.value = ''
}
function promote(code: string): void {
  apply(promoteToOriginal(draft.value!, code))
  conflictLocale.value = ''
  selected.value = 'source'
}
// 开始翻译时用原文预填，并聚焦第一个输入控件，管理员在原文基础上改写。
async function add(code: string): Promise<void> {
  apply(addTranslation(draft.value!, code))
  await nextTick()
  editorRef.value?.querySelector<HTMLElement>('input:not([type="file"]), textarea')?.focus()
}
function save(): void {
  if (draft.value) emit('save', draft.value)
}
</script>
