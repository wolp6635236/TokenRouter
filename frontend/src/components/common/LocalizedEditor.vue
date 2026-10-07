<template>
  <div>
    <div class="mb-1.5 flex items-center justify-between gap-2">
      <label :for="id" :class="labelClass">{{ label }}</label>
      <LocalizedTranslateButton
        :model-value="modelValue"
        :title="dialogTitle || label"
        :fallback-locale="fallbackLocale"
        @update:model-value="emit('update:modelValue', $event)"
      >
        <template #default="scope">
          <slot v-bind="scope">
            <input v-if="rows === 1" :id="scope.id" dir="auto" :value="String(scope.value ?? '')" class="input" :class="inputClass" :placeholder="placeholder" @input="scope.update(($event.target as HTMLInputElement).value as T)" />
            <textarea v-else :id="scope.id" dir="auto" :value="String(scope.value ?? '')" :rows="rows" class="input resize-y" :class="inputClass" :placeholder="placeholder" @input="scope.update(($event.target as HTMLTextAreaElement).value as T)" />
          </slot>
        </template>
      </LocalizedTranslateButton>
    </div>
    <slot :value="modelValue.source" :update="updateValue" :locale="modelValue.source_locale" :id="id">
      <input v-if="rows === 1" :id="id" dir="auto" :value="String(modelValue.source ?? '')" class="input" :class="inputClass" :placeholder="placeholder" @input="updateValue(($event.target as HTMLInputElement).value as T)" />
      <textarea v-else :id="id" dir="auto" :value="String(modelValue.source ?? '')" :rows="rows" class="input resize-y" :class="inputClass" :placeholder="placeholder" @input="updateValue(($event.target as HTMLTextAreaElement).value as T)" />
    </slot>
  </div>
</template>

<script lang="ts">
let editorSequence = 0
</script>

<script setup lang="ts" generic="T">
import { computed } from 'vue'
import LocalizedTranslateButton from '@/components/common/LocalizedTranslateButton.vue'
import { getLocale } from '@/i18n'
import type { LocalizedUpdate } from '@/i18n/content'
import { updateSource, withDefaultSource } from '@/i18n/contentEdit'

// 表单里只编辑原文，译文在标签行右侧入口打开的翻译弹窗里编辑。
const props = withDefaults(defineProps<{
  modelValue: LocalizedUpdate<T>
  label: string
  /** 标签样式，紧凑的行内表单可以换成小号文字。 */
  labelClass?: string
  /** 翻译弹窗标题里的字段名，默认取 label。 */
  dialogTitle?: string
  rows?: number
  placeholder?: string
  /** 加在默认输入控件上的类，例如链接用等宽字体。 */
  inputClass?: string
  defaultSourceLocale?: string
}>(), { rows: 1, labelClass: 'input-label mb-0' })
const emit = defineEmits<{ 'update:modelValue': [value: LocalizedUpdate<T>] }>()
defineSlots<{ default(props: { value: T; update: (value: T) => void; locale: string | null; id: string }): unknown }>()

// eslint-disable-next-line no-useless-assignment -- 多个译文编辑器共享字段编号。
const id = `localized-field-${++editorSequence}`
// 原文语言未知时，管理员修改原文视为用当前界面语言书写。
const fallbackLocale = computed(() => props.defaultSourceLocale || getLocale())

function updateValue(value: T): void {
  const base = props.defaultSourceLocale ? withDefaultSource(props.modelValue, props.defaultSourceLocale) : props.modelValue
  emit('update:modelValue', updateSource(base, value, fallbackLocale.value))
}
</script>
