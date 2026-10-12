<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { getActivePinia } from 'pinia'
import { useAuthStore } from '@/stores'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import { COPY_FEEDBACK_MS } from '@/constants/ui'
import Icon from '@/components/icons/Icon.vue'
import IdBadge from '@/components/common/IdBadge.vue'

// link 为 false 时只显示和复制，用在已经位于该请求详情的地方。
// full 为 true 时完整显示 ID 并按需折行，默认按表格列宽截断。
const props = withDefaults(defineProps<{
  value?: string | null
  link?: boolean
  full?: boolean
}>(), {
  link: true,
  full: false,
})

const authStore = getActivePinia() ? useAuthStore() : null
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copiedValue = ref('')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

// 管理员可以打开诊断详情，其他身份显示可复制的 ID。
const canOpenDetails = computed(() => props.link && authStore?.isAdmin === true)
const destination = computed(() => ({
  path: '/admin/requests',
  query: { request_id: props.value || undefined },
}))

// 表格行复用组件时，只有复制的仍是当前 ID 才显示对勾。
const showCopied = computed(() => Boolean(copiedValue.value) && copiedValue.value === props.value)

async function copy() {
  const value = props.value
  if (!value || !await copyToClipboard(value, t('requests.copied'))) return
  copiedValue.value = value
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedValue.value = ''
  }, COPY_FEEDBACK_MS)
}

onBeforeUnmount(() => clearTimeout(copiedTimer))
</script>

<template>
  <span v-if="value" class="inline-flex min-w-0 items-center gap-1.5 align-middle" :class="full ? 'max-w-full' : 'max-w-56'">
    <IdBadge />
    <RouterLink
      v-if="canOpenDetails"
      :to="destination"
      :title="value"
      :class="full ? 'break-all' : 'truncate'"
      class="min-w-0 font-mono text-xs text-gray-600 underline-offset-2 transition-colors hover:text-primary-600 hover:underline dark:text-dark-200 dark:hover:text-primary-400"
      @click.stop
    >{{ value }}</RouterLink>
    <span v-else class="min-w-0 font-mono text-xs text-gray-600 dark:text-dark-200" :class="full ? 'break-all' : 'truncate'" :title="value">{{ value }}</span>
    <button
      type="button"
      class="shrink-0 rounded-compact p-0.5 transition-colors hover:bg-gray-200 dark:hover:bg-dark-700"
      :class="showCopied ? 'text-emerald-500' : 'text-gray-400 hover:text-gray-600 dark:text-dark-400 dark:hover:text-dark-200'"
      :aria-label="t('requests.copy')"
      :title="showCopied ? t('common.copied') : t('requests.copy')"
      @click.stop="copy"
    >
      <Icon :name="showCopied ? 'check' : 'copy'" size="sm" class="h-3.5 w-3.5" />
    </button>
  </span>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>
