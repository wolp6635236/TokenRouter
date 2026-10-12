<template>
  <div class="flex items-center">
    <span
      :class="[
        'inline-flex items-center gap-1 rounded-compact px-2 py-0.5 text-xs font-medium',
        statusClass
      ]"
    >
      <!-- Four-square grid icon -->
      <Icon name="grid" size="xs" class="h-3 w-3" />
      <!-- 数字加 self-baseline，徽章的基线取自数字，移动卡片的标签和它对齐。 -->
      <span class="self-baseline font-mono">{{ current }}</span>
      <span class="self-baseline text-gray-400 dark:text-gray-500">/</span>
      <span class="self-baseline font-mono">{{ max }}</span>
    </span>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { computed } from 'vue'

const props = defineProps<{
  current: number
  max: number
}>()

// Status color based on usage
const statusClass = computed(() => {
  const { current, max } = props

  // Full: red
  if (current >= max && max > 0) {
    return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  }
  // In use: yellow
  if (current > 0) {
    return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  }
  // Idle: gray
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
})
</script>
