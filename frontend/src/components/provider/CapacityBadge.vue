<script setup lang="ts">
defineProps<{
  colorClass: string
  tooltip?: string
  current: string | number
  max: string | number
  suffix?: string
}>()
</script>

<template>
  <span
    :class="[
      'inline-flex items-center gap-1 rounded-compact px-1.5 py-px text-xs font-medium leading-tight',
      colorClass
    ]"
    :title="tooltip"
  >
    <slot />
    <!-- 文字加 self-baseline，徽章的基线取自数字，移动卡片的标签和它对齐。 -->
    <span class="self-baseline font-mono">{{ current }}</span>
    <span class="self-baseline text-gray-400 dark:text-gray-500">/</span>
    <span class="self-baseline font-mono">{{ max }}</span>
    <span v-if="suffix" class="self-baseline text-xs opacity-60">{{ suffix }}</span>
  </span>
</template>
