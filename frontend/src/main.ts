import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { initI18n, getLocale } from './i18n'
import { useAppStore } from '@/stores/app'
import { initTheme } from '@/composables/useTheme'
import { updateFavicon } from '@/utils/branding'
import { isIOSDevice } from '@/utils/device'
// 在全局样式前加载自托管字体，确保 Tailwind 字体栈首次渲染即可命中。
import '@fontsource-variable/plus-jakarta-sans'
import '@fontsource-variable/plus-jakarta-sans/wght-italic.css'
import '@fontsource-variable/geist-mono'
import './style.css'

function initIOSViewportZoomFix() {
  // iOS Safari 在输入框字号小于 16px 时聚焦会自动放大页面，且失焦后不会恢复。
  // 限制 maximum-scale 可阻止该行为；iOS 10+ 用户仍可双指手动缩放，不影响可访问性。
  // iOS 设备注入视口修正，Android Chrome 使用浏览器的手动缩放。
  if (!isIOSDevice()) return

  const viewport = document.querySelector('meta[name="viewport"]')
  if (!viewport) return

  const content = viewport.getAttribute('content') || ''
  if (/maximum-scale/i.test(content)) return
  viewport.setAttribute('content', [content.trim(), 'maximum-scale=1.0'].filter(Boolean).join(', '))
}

async function bootstrap() {
  // 挂载前应用主题，首屏按所选明暗模式渲染。
  initTheme()
  initIOSViewportZoomFix()

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  // Initialize settings from injected config BEFORE mounting (prevents flash)
  // This must happen after pinia is installed but before router and i18n
  const appStore = useAppStore()
  appStore.initFromInjectedConfig()

  // Set document title immediately after config is loaded
  if (appStore.siteName && appStore.siteName !== 'TokenRouter') {
    document.title = `${appStore.siteName} - AI API Gateway`
  }
  updateFavicon(appStore.siteLogo)

  await initI18n()
  if (appStore.cachedPublicSettings?.locale !== getLocale()) await appStore.fetchPublicSettings(true)

  app.use(router)
  app.use(i18n)

  // 路由初始导航完成后挂载应用。
  await router.isReady()
  app.mount('#app')
}

bootstrap()
