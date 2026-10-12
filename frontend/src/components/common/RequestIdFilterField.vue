<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import FilterField from '@/components/common/FilterField.vue'

// RequestIdFilterField 是 FilterDropdown 面板里的请求 ID 条件。
// modelValue 是已生效的 ID，输入框里的草稿按回车或失焦后才写回。
const props = defineProps<{
  modelValue?: string | null
}>()
const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
  (event: 'search'): void
}>()

const { t } = useI18n()
const draft = ref(props.modelValue ?? '')

// 父级重置条件或从链接带入 ID 时，草稿跟着更新。
watch(() => props.modelValue, (value) => {
  draft.value = value ?? ''
})

// apply 在草稿和已生效的 ID 不同时写回并触发查询。
function apply() {
  const value = draft.value.trim()
  draft.value = value
  if (value === (props.modelValue ?? '')) return
  emit('update:modelValue', value)
  emit('search')
}

function clear() {
  draft.value = ''
  apply()
}
</script>

<template>
  <FilterField :label="t('requests.id')" :value-text="modelValue || ''" @clear="clear">
    <input
      v-model="draft"
      type="text"
      class="input font-mono placeholder:font-sans"
      :placeholder="t('requests.filterPlaceholder')"
      maxlength="255"
      spellcheck="false"
      autocomplete="off"
      @keydown.enter.prevent="apply"
      @blur="apply"
    />
  </FilterField>
</template>
