<template>
  <div :class="layout">
    <div v-for="(field, index) in fields" :key="field.key" :class="field.class">
      <div class="mb-1.5 flex items-center justify-between gap-2">
        <label :for="`${id}-${field.key}`" class="input-label mb-0">
          {{ field.label }}
          <span v-if="field.required" class="text-red-500">*</span>
        </label>
        <LocalizedTranslateButton
          v-if="index === 0"
          :model-value="content"
          :title="dialogTitle || field.label"
          :fallback-locale="fallbackLocale"
          :reference-fields="fields"
          @update:model-value="emit('update:modelValue', $event)"
        >
          <template #default="{ value, update, id: scopeId }">
            <div :class="layout">
              <div v-for="item in fields" :key="item.key" :class="item.class">
                <label :for="`${scopeId}-${item.key}`" class="input-label">{{ item.label }}</label>
                <textarea v-if="item.multiline" :id="`${scopeId}-${item.key}`" dir="auto" :value="value[item.key]" class="input resize-y" :class="item.inputClass" :rows="item.rows || 3" :placeholder="item.placeholder" @input="update({ ...value, [item.key]: ($event.target as HTMLTextAreaElement).value })"></textarea>
                <input v-else :id="`${scopeId}-${item.key}`" dir="auto" :value="value[item.key]" :type="item.url ? 'url' : 'text'" class="input" :class="item.inputClass" :placeholder="item.placeholder" @input="update({ ...value, [item.key]: ($event.target as HTMLInputElement).value })" />
              </div>
            </div>
          </template>
        </LocalizedTranslateButton>
      </div>
      <textarea v-if="field.multiline" :id="`${id}-${field.key}`" dir="auto" :value="content.source[field.key]" class="input resize-y" :class="field.inputClass" :rows="field.rows || 3" :placeholder="field.placeholder" :required="field.required" @input="updateField(field.key, ($event.target as HTMLTextAreaElement).value)"></textarea>
      <input v-else :id="`${id}-${field.key}`" dir="auto" :value="content.source[field.key]" :type="field.url ? 'url' : 'text'" class="input" :class="field.inputClass" :placeholder="field.placeholder" :required="field.required" @input="updateField(field.key, ($event.target as HTMLInputElement).value)" />
    </div>
  </div>
</template>

<script lang="ts">
let fieldsSequence = 0

// LocalizedField 描述一个文案字段。class 用于栅格跨列，inputClass 加在输入控件上。
export interface LocalizedField<T> {
  key: keyof T & string
  label: string
  placeholder?: string
  multiline?: boolean
  rows?: number
  url?: boolean
  required?: boolean
  class?: string
  inputClass?: string
}
</script>

<script setup lang="ts" generic="T extends Record<string, string>">
import { computed } from 'vue'
import LocalizedTranslateButton from '@/components/common/LocalizedTranslateButton.vue'
import { getLocale } from '@/i18n'
import { originalContent, type LocalizedUpdate } from '@/i18n/content'
import { updateSource } from '@/i18n/contentEdit'

// 一组字段共用一份内容和版本，表单里逐个编辑原文，翻译入口在第一个字段的标签行。
const props = withDefaults(defineProps<{
  modelValue?: LocalizedUpdate<T>
  source: T
  sourceLocale?: string | null
  fields: LocalizedField<T>[]
  /** 字段容器的布局类，表单和翻译弹窗里的每种语言共用。 */
  layout?: string
  /** 翻译弹窗标题里的名称，默认取第一个字段的标签。 */
  dialogTitle?: string
}>(), { layout: 'space-y-4' })
const emit = defineEmits<{ 'update:modelValue': [value: LocalizedUpdate<T>] }>()

// eslint-disable-next-line no-useless-assignment -- 不同字段编辑实例需要不同的输入 ID。
const id = `localized-fields-${++fieldsSequence}`
const content = computed(() => props.modelValue || originalContent(props.source, props.sourceLocale))
const fallbackLocale = computed(() => props.sourceLocale || getLocale())

function updateField(key: keyof T & string, value: string): void {
  emit('update:modelValue', updateSource(content.value, { ...content.value.source, [key]: value }, fallbackLocale.value))
}
</script>
