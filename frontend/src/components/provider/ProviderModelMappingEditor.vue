<template>
  <ModelMappingEditor
    :model-value="modelValue"
    :title="title"
    :hint="hint ?? t('admin.providers.mapRequestModels')"
    :add-label="t('admin.providers.addMapping')"
    :empty-text="t('admin.providers.modelMappingEmpty')"
    :source-label="sourcePlaceholder ?? t('admin.providers.requestModel')"
    :target-label="targetPlaceholder ?? t('admin.providers.actualModel')"
    :source-placeholder="sourcePlaceholder ?? t('admin.providers.requestModel')"
    :target-placeholder="targetPlaceholder ?? t('admin.providers.actualModel')"
    :field-errors="fieldErrors"
    :test-id="testId"
    @update:model-value="emit('update:modelValue', $event)"
    @add="emit('add', $event)"
    @remove="(row, index) => emit('remove', row, index)"
  >
    <template v-if="$slots['header-actions']" #header-actions>
      <slot name="header-actions" />
    </template>
  </ModelMappingEditor>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelMappingEditor from '@/components/common/ModelMappingEditor.vue'
import {
  WILDCARD_ONLY_RULES,
  validateModelMappingRows,
  type ModelMappingRow,
} from '@/utils/modelMappingRules'

const props = defineProps<{
  modelValue: ModelMappingRow[]
  title?: string
  hint?: string
  sourcePlaceholder?: string
  targetPlaceholder?: string
  wildcardValidation?: boolean
  testId?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [rows: ModelMappingRow[]]
  add: [row: ModelMappingRow]
  remove: [row: ModelMappingRow, index: number]
}>()

const { t } = useI18n()

// 通配符提示只做实时展示，提交时仍由 buildModelMappingObject 跳过无效规则。
const fieldErrors = computed(() => {
  if (!props.wildcardValidation) return undefined
  return validateModelMappingRows(props.modelValue, WILDCARD_ONLY_RULES).map((issue) => ({
    from: issue.from ? t('admin.providers.wildcardOnlyAtEnd') : undefined,
    to: issue.to ? t('admin.providers.targetNoWildcard') : undefined,
  }))
})
</script>
