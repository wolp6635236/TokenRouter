<template>
  <!-- 不 teleport:与四个迁移前组件的渲染位置一致,嵌套在 BaseDialog 内时层级由 zIndex 决胜。 -->
  <MotionTransition name="modal" appear @after-leave="handleAfterLeave">
    <div v-if="show" :inert="!show || undefined" role="dialog" aria-modal="true" class="fixed inset-0 z-modal overflow-y-auto" :style="zIndexStyle" @click.self="handleOverlay">
      <div class="flex min-h-full items-center justify-center p-4">
        <div class="fixed inset-0 bg-[var(--overlay-bg)]" @click="handleOverlay"></div>

        <div ref="dialogRef" tabindex="-1" class="auth-dialog-content relative w-full max-w-md transform rounded-surface bg-white p-6 shadow-xl dark:bg-dark-900 sm:rounded-dialog">
          <slot />
        </div>
      </div>
    </div>
  </MotionTransition>
</template>

<script setup lang="ts">
import MotionTransition from '@/components/common/MotionTransition.vue'
// 安全凭证流程(TOTP 设置/禁用/登录验证/提权)的居中卡片弹窗壳。
// 与 BaseDialog 的管理台对话框是两个有意的风格族:居中图标头、无右上角 X、整卡 p-6。
import { computed, ref } from 'vue'
import { useDialogLifecycle } from '@/composables/useDialogLifecycle'
import { Z_INDEX } from '@/constants/overlay'

const props = withDefaults(defineProps<{
  /** 父级用 v-if 控制时保持默认 true;自管理显隐(如 step-up 控制器)传响应式值。 */
  show?: boolean
  /** 默认 50;叠加在其他弹窗之上(如 step-up)传 60。 */
  zIndex?: number
  /** 遮罩点击是否关闭;登录态验证等不可打断的流程传 false。 */
  closeOnOverlay?: boolean
}>(), {
  show: true,
  zIndex: Z_INDEX.MODAL,
  closeOnOverlay: true
})

const emit = defineEmits<{ (e: 'close'): void; (e: 'after-leave'): void }>()
const dialogRef = ref<HTMLElement | null>(null)
const { afterLeave } = useDialogLifecycle(() => props.show, dialogRef)
function handleAfterLeave() {
  afterLeave()
  if (!props.show) emit('after-leave')
}

const zIndexStyle = computed(() => (props.zIndex !== Z_INDEX.MODAL ? { zIndex: props.zIndex } : undefined))
const handleOverlay = () => {
  if (props.closeOnOverlay) emit('close')
}
</script>
