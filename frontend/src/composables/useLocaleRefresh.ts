import { onScopeDispose } from 'vue'

// useLocaleRefresh 重新读取语言相关的展示数据，表单草稿由页面继续持有。
export function useLocaleRefresh(refresh: () => Promise<unknown> | void): void {
  if (typeof window === 'undefined') return
  let active = true
  const listener = () => {
    if (!active) return
    Promise.resolve().then(refresh).catch(error => {
      console.warn('语言切换后刷新展示数据失败', error)
    })
  }
  window.addEventListener('locale-changed', listener)
  onScopeDispose(() => {
    active = false
    window.removeEventListener('locale-changed', listener)
  })
}
