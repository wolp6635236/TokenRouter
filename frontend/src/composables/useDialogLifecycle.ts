import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

const dialogs: symbol[] = []

/** 将滚动锁和焦点恢复绑定到弹窗的实际退出，支持嵌套及退出途中重新打开。 */
export function useDialogLifecycle(visible: () => boolean, panel: Ref<HTMLElement | null>) {
  const token = Symbol('dialog')
  let previousFocus: HTMLElement | null = null
  let held = false
  let closingPanel: HTMLElement | null = null

  watch(visible, async (open) => {
    if (!open) {
      closingPanel = panel.value
      return
    }
    if (!held) {
      previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
      dialogs.push(token)
      document.body.classList.add('modal-open')
      held = true
    }
    closingPanel = null
    await nextTick()
    if (!visible() || dialogs[dialogs.length - 1] !== token) return
    const target = panel.value?.querySelector<HTMLElement>(
      'button:not(:disabled), [href], input:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])',
    )
    const focusTarget = target ?? panel.value
    focusTarget?.focus({ preventScroll: true })
  }, { immediate: true })

  function release() {
    if (!held) return
    const wasTop = dialogs[dialogs.length - 1] === token
    dialogs.splice(dialogs.indexOf(token), 1)
    held = false
    if (dialogs.length === 0) document.body.classList.remove('modal-open')
    const active = document.activeElement
    const focusStillInside = active === document.body || !active?.isConnected ||
      panel.value?.contains(active) || closingPanel?.contains(active)
    // 新弹窗或其他控件已取得焦点时，不用旧弹窗保存的节点覆盖它。
    if (wasTop && focusStillInside && previousFocus?.isConnected && !previousFocus.closest('[inert]')) {
      previousFocus.focus({ preventScroll: true })
    }
    previousFocus = null
    closingPanel = null
  }

  function afterLeave() {
    if (!visible()) release()
  }

  onBeforeUnmount(release)
  return { afterLeave, isTop: () => dialogs[dialogs.length - 1] === token }
}
