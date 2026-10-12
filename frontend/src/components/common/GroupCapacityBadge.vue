<template>
  <!-- 徽章里的数字加 self-baseline，徽章的基线取自数字，移动卡片的标签和它对齐。 -->
  <div :class="layoutClass">
    <!-- 并发槽位 -->
    <div class="flex items-center gap-1">
      <span
        :class="[
          'inline-flex items-center gap-1 rounded-compact px-1.5 py-0.5 text-xs font-medium',
          capacityClass(concurrencyUsed, concurrencyMax)
        ]"
      >
        <Icon name="grid" size="md" class="h-2.5 w-2.5" />
        <span class="self-baseline font-mono">{{ concurrencyUsed }}</span>
        <span class="self-baseline text-gray-400 dark:text-gray-500">/</span>
        <span class="self-baseline font-mono">{{ concurrencyMax }}</span>
      </span>
    </div>

    <!-- 会话数 -->
    <div v-if="sessionsMax > 0" class="flex items-center gap-1">
      <span
        :class="[
          'inline-flex items-center gap-1 rounded-compact px-1.5 py-0.5 text-xs font-medium',
          capacityClass(sessionsUsed, sessionsMax)
        ]"
      >
        <Icon name="users" size="md" class="h-2.5 w-2.5" />
        <span class="self-baseline font-mono">{{ sessionsUsed }}</span>
        <span class="self-baseline text-gray-400 dark:text-gray-500">/</span>
        <span class="self-baseline font-mono">{{ sessionsMax }}</span>
      </span>
    </div>

    <!-- RPM -->
    <div v-if="rpmMax > 0" class="flex items-center gap-1">
      <span
        :class="[
          'inline-flex items-center gap-1 rounded-compact px-1.5 py-0.5 text-xs font-medium',
          capacityClass(rpmUsed, rpmMax)
        ]"
      >
        <Icon name="clock" size="md" class="h-2.5 w-2.5" />
        <span class="self-baseline font-mono">{{ rpmUsed }}</span>
        <span class="self-baseline text-gray-400 dark:text-gray-500">/</span>
        <span class="self-baseline font-mono">{{ rpmMax }}</span>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { computed } from 'vue'

interface Props {
  layout?: 'vertical' | 'horizontal'
  concurrencyUsed: number
  concurrencyMax: number
  sessionsUsed: number
  sessionsMax: number
  rpmUsed: number
  rpmMax: number
}

const props = withDefaults(defineProps<Props>(), {
  layout: 'vertical',
  concurrencyUsed: 0,
  concurrencyMax: 0,
  sessionsUsed: 0,
  sessionsMax: 0,
  rpmUsed: 0,
  rpmMax: 0
})

// 模型广场需要横向展示，分组管理表格默认保持竖向展示。
const layoutClass = computed(() =>
  props.layout === 'horizontal'
    ? 'flex flex-row flex-wrap items-center gap-1'
    : 'flex flex-col gap-1'
)

function capacityClass(used: number, max: number): string {
  if (max > 0 && used >= max) {
    return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  }
  if (used > 0) {
    return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  }
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
}
</script>
