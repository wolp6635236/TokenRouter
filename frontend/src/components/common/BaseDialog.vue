<template>
  <Teleport to="body">
    <MotionTransition name="modal" @after-leave="handleAfterLeave">
      <div
        v-if="show"
        :inert="!show || undefined"
        class="modal-overlay h-[100dvh] w-[100dvw] min-w-0 overflow-hidden"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- 动态视口单位避开移动端浏览器工具栏，vh/vw 规则由公共样式作为旧浏览器兜底。 -->
        <div
          ref="dialogRef"
          tabindex="-1"
          :class="['modal-content min-h-0 min-w-0 max-h-[95dvh] sm:max-h-[90dvh]', widthClasses]"
          @click.stop
        >
          <!-- 头部 -->
          <div class="modal-header min-w-0 max-w-full gap-3">
            <div class="flex min-w-0 flex-1 items-center gap-3">
              <slot name="header-icon"></slot>
              <div class="min-w-0">
                <h3 :id="dialogId" class="modal-title min-w-0 break-words">
                  {{ title }}
                </h3>
                <p v-if="subtitle" class="mt-0.5 truncate text-xs text-gray-500 dark:text-dark-400">
                  {{ subtitle }}
                </p>
              </div>
            </div>
            <slot name="header-actions"></slot>
            <button
              @click="emit('close')"
              class="-mr-2 rounded-control p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-black/10 focus-visible:ring-offset-2 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-dark-300 dark:focus-visible:ring-primary-500/30 dark:focus-visible:ring-offset-dark-900"
              aria-label="Close modal"
            >
              <Icon name="x" size="sm" />
            </button>
          </div>

          <!-- 内容区 -->
          <div
            ref="modalBodyRef"
            class="modal-body min-h-0 min-w-0 max-w-full"
            :class="{ 'modal-body-contained': !bodyScroll, 'modal-body-flush': flush }"
          >
            <slot></slot>
          </div>

          <!-- 底部 -->
          <div v-if="$slots.footer" class="modal-footer min-w-0 max-w-full">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </MotionTransition>
  </Teleport>
</template>

<script lang="ts">
let dialogIdCounter = 0
</script>

<script setup lang="ts">
import MotionTransition from '@/components/common/MotionTransition.vue'
import { computed, watch, onMounted, onUnmounted, ref, nextTick } from 'vue'
import { useDialogLifecycle } from '@/composables/useDialogLifecycle'
import Icon from '@/components/icons/Icon.vue'
import { Z_INDEX } from '@/constants/overlay'

// 生成唯一ID以避免多个对话框时ID冲突
// eslint-disable-next-line no-useless-assignment -- 后续对话框实例需要读取更新后的标题计数器。
const dialogId = `modal-title-${++dialogIdCounter}`

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  /** 标题下方的一行说明，超出时截断。 */
  subtitle?: string
  width?: DialogWidth
  bodyScroll?: boolean
  /** 去掉内容区内边距，供需要贴边分栏的工作区弹窗使用。 */
  flush?: boolean
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
  (e: 'after-leave'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  bodyScroll: true,
  flush: false,
  closeOnEscape: true,
  closeOnClickOutside: false,
  zIndex: Z_INDEX.MODAL
})

const emit = defineEmits<Emits>()

// 自定义层级会覆盖 CSS 中默认的 z-50。
const zIndexStyle = computed(() => {
  return props.zIndex !== Z_INDEX.MODAL ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // 移动端弹窗宽度限制在视口内，并保留遮罩边距。
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-[calc(100vw-1rem)] sm:max-w-md',
    normal: 'max-w-[calc(100vw-1rem)] sm:max-w-lg',
    wide: 'max-w-[calc(100vw-1rem)] sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'max-w-[calc(100vw-1rem)] sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'max-w-[calc(100vw-1rem)] sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (props.show && isTop() && props.closeOnEscape && event.key === 'Escape') {
    emit('close')
  }
}

const { afterLeave, isTop } = useDialogLifecycle(() => props.show, dialogRef)

function handleAfterLeave() {
  afterLeave()
  if (!props.show) emit('after-leave')
}

// 重新打开默认内容区时回顶，分页表单继续自行管理内部滚动。
watch(() => props.show, async (open) => {
  if (!open) return
  await nextTick()
  if (props.show && modalBodyRef.value) modalBodyRef.value.scrollTop = 0
}, { immediate: true })

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
/* 分页表单自行管理滚动，外壳只分配标题和按钮之间的剩余高度。 */
.modal-body-contained {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 贴边分栏的外壳内边距为 0，由内容自行留白。 */
.modal-body-flush {
  padding: 0;
}
</style>
