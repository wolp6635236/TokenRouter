<template>
  <div>
    <div class="mb-1.5 flex items-center justify-between gap-2">
      <label :for="inputId" class="text-sm font-medium text-primary-900 dark:text-dark-50">{{ label }}</label>
      <!-- 设置了覆盖值的字段显示恢复按钮，点击后这一项改用部署默认值。 -->
      <button
        v-if="overridden"
        type="button"
        class="inline-flex rounded-compact p-0.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-50 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-gray-300"
        :title="t('admin.settings.responsesWS.resetField')"
        :aria-label="`${label}: ${t('admin.settings.responsesWS.resetField')}`"
        :disabled="disabled"
        :data-testid="`responses-ws-reset-${field.key}`"
        @click="emit('reset')"
      >
        <Icon name="undo" size="sm" />
      </button>
    </div>
    <div class="relative">
      <input
        :id="inputId"
        class="input tabular-nums"
        :class="field.unit && 'pr-12'"
        type="number"
        :inputmode="field.step === '1' ? 'numeric' : 'decimal'"
        :min="field.min"
        :max="field.max"
        :step="field.step"
        :disabled="disabled"
        :value="modelValue"
        :placeholder="placeholder"
        :aria-describedby="field.hint ? `${inputId}-hint` : undefined"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />
      <span
        v-if="field.unit"
        class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-gray-400 dark:text-dark-400"
      >{{ t(`admin.settings.responsesWS.units.${field.unit}`) }}</span>
    </div>
    <p v-if="field.hint" :id="`${inputId}-hint`" class="input-hint">{{ t(`admin.settings.responsesWS.hints.${field.key}`) }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { ResponsesWSNumberField } from './responsesWsFields'

// 参数网格里的一个数字字段：标题在上，输入框带单位，空输入框的占位文字是当前有效值。
const props = defineProps<{
  field: ResponsesWSNumberField
  modelValue: number | ''
  placeholder: string
  overridden: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
  (event: 'reset'): void
}>()

const { t } = useI18n()
const inputId = computed(() => `responses-ws-input-${props.field.key}`)
const label = computed(() => t(`admin.settings.responsesWS.fields.${props.field.key}`))
</script>
