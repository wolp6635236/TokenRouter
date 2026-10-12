<template>
  <div
    class="flex min-h-9 flex-wrap items-center gap-2 rounded-control border border-primary-900/10 bg-white p-2 transition duration-fast focus-within:ring-2 focus-within:ring-black/10 dark:border-dark-600 dark:bg-dark-950 dark:focus-within:border-dark-400 dark:focus-within:ring-white/6"
  >
    <span
      v-for="tag in tags"
      :key="tag"
      :data-testid="tagTestid"
      class="inline-flex items-center gap-1 rounded-compact bg-gray-100 px-2 py-1 font-mono text-xs text-gray-700 dark:bg-dark-700 dark:text-dark-100"
    >
      <span>{{ tag }}</span>
      <button
        type="button"
        class="rounded-full text-gray-500 hover:bg-gray-200 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
        :aria-label="removeLabel?.(tag)"
        @click="emit('remove', tag)"
      >
        <Icon name="x" size="xs" class="h-3.5 w-3.5" :stroke-width="2" />
      </button>
    </span>
    <input
      :id="id"
      v-model="draft"
      type="text"
      class="min-w-0 flex-1 basis-48 bg-transparent px-1 font-mono text-sm text-gray-900 outline-none placeholder:text-primary-900/45 dark:text-dark-50 dark:placeholder:text-dark-400"
      :placeholder="placeholder"
      :data-testid="inputTestid"
      @input="emit('input', $event)"
      @keydown="emit('keydown', $event)"
      @blur="emit('blur', $event)"
      @paste="emit('paste', $event)"
    />
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'

// 标签输入框只负责展示和转发事件，分隔符、去重和规范化由调用方处理。
defineProps<{
  tags: string[]
  id?: string
  placeholder?: string
  /** 返回删除按钮的无障碍名称。 */
  removeLabel?: (tag: string) => string
  tagTestid?: string
  inputTestid?: string
}>()
const draft = defineModel<string>('draft', { required: true })
const emit = defineEmits<{
  remove: [tag: string]
  input: [event: Event]
  keydown: [event: KeyboardEvent]
  blur: [event: FocusEvent]
  paste: [event: ClipboardEvent]
}>()
</script>
