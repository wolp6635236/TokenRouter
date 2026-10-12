<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

// modelValue 是已生效的请求 ID，输入框里的草稿按回车或点清除后才写回。
const props = defineProps<{
  modelValue?: string | null
}>()
const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
  (event: 'search'): void
}>()

const { t } = useI18n()
const draft = ref(props.modelValue ?? '')

// 父级重置或从链接带入 ID 时，草稿跟着更新。
watch(() => props.modelValue, (value) => {
  draft.value = value ?? ''
})

// apply 写回去掉首尾空白的 ID 并触发查询。
function apply() {
  const value = draft.value.trim()
  draft.value = value
  emit('update:modelValue', value)
  emit('search')
}

// clear 清空草稿，已有生效的 ID 时重新查询。
function clear() {
  draft.value = ''
  if (!props.modelValue) return
  emit('update:modelValue', '')
  emit('search')
}

defineExpose({ apply })
</script>

<template>
  <div class="input-icon-wrap min-w-0">
    <Icon name="search" size="md" class="input-icon text-gray-400 dark:text-dark-400" />
    <input
      v-model="draft"
      type="text"
      class="input input-has-icon font-mono placeholder:font-sans"
      :class="{ 'input-has-icon-right': draft }"
      :aria-label="t('requests.id')"
      :placeholder="t('requests.searchPlaceholder')"
      maxlength="255"
      spellcheck="false"
      autocomplete="off"
      @keydown.enter.prevent="apply"
    />
    <button
      v-if="draft"
      type="button"
      class="input-icon-right input-icon-action flex items-center text-gray-400 transition-colors hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200"
      :aria-label="t('requests.clearSearch')"
      :title="t('requests.clearSearch')"
      @click="clear"
    >
      <Icon name="x" size="sm" />
    </button>
  </div>
</template>
