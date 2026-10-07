<template>
  <button
    type="button"
    class="inline-flex shrink-0 items-center gap-1 rounded-compact text-xs transition-colors duration-fast focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50"
    :class="summary.stale || summary.sourceLocaleMissing
      ? 'text-amber-600 hover:text-amber-700 dark:text-amber-400 dark:hover:text-amber-300'
      : 'text-gray-500 hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-500'"
    :aria-label="t('localization.dialogTitle', { field: title })"
    @click="open = true"
  >
    <Icon name="globe" size="xs" />
    <template v-if="summary.sourceLocaleMissing">
      <span>{{ t('localization.chooseOriginalLanguage') }}</span>
      <Icon name="exclamationTriangle" size="xs" :animate-on-hover="false" />
    </template>
    <template v-else>
      <span>{{ text }}</span>
      <Icon v-if="summary.stale" name="exclamationTriangle" size="xs" :animate-on-hover="false" />
      <Icon v-else-if="summary.locales.length" name="check" size="xs" :animate-on-hover="false" />
      <span v-if="summary.stale">{{ t('localization.needsUpdate') }}</span>
    </template>
  </button>
  <LocalizedTranslationDialog
    :show="open"
    :title="title"
    :model-value="modelValue"
    :fallback-locale="fallbackLocale"
    :reference-fields="referenceFields"
    @close="open = false"
    @save="save"
  >
    <template #default="scope">
      <slot v-bind="scope" />
    </template>
  </LocalizedTranslationDialog>
</template>

<script setup lang="ts" generic="T">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LocalizedTranslationDialog from '@/components/common/LocalizedTranslationDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { availableLocales } from '@/i18n/catalog'
import type { LocalizedUpdate } from '@/i18n/content'
import { translationSummary } from '@/i18n/contentEdit'

const props = defineProps<{
  modelValue: LocalizedUpdate<T>
  title: string
  fallbackLocale?: string
  /** 多字段内容在原文参照里逐个显示的字段。 */
  referenceFields?: { key: string; label: string }[]
}>()
const emit = defineEmits<{ 'update:modelValue': [value: LocalizedUpdate<T>] }>()
defineSlots<{ default(props: { value: T; update: (value: T) => void; locale: string | null; id: string }): unknown }>()

const { t } = useI18n()
const open = ref(false)
const summary = computed(() => translationSummary(props.modelValue))

// 两种以内的语言直接写名称，更多语言时显示数量。
const text = computed(() => {
  const { locales } = summary.value
  if (!locales.length) return t('localization.addTranslation')
  if (locales.length > 2) return t('localization.languageCount', { n: locales.length })
  return locales.map(code => availableLocales.find(item => item.code === code)?.name || code).join(' · ')
})

function save(value: LocalizedUpdate<T>): void {
  emit('update:modelValue', value)
  open.value = false
}
</script>
