import { readStorageWithLegacyKey } from '@/utils/storage'
import { createI18n } from 'vue-i18n'

import { availableLocales, defaultLocale, normalizeLocale, type LocaleCode } from './catalog'
export { availableLocales } from './catalog'
export type { LocaleCode } from './catalog'

type LocaleMessages = Record<string, any>

const LOCALE_KEY = 'tokenrouter_locale'
const DEFAULT_LOCALE = defaultLocale

const catalogModules = import.meta.glob<{ default: LocaleMessages }>('./locales/*/index.ts')
const localeLoaders = Object.fromEntries(availableLocales.map(item => [item.code, catalogModules[`./locales/${item.catalog}/index.ts`]]))


function getLocaleStorage(): Storage | null {
  // 测试环境或受限浏览器环境可能没有可用的 localStorage，语言初始化需要安全降级。
  try {
    return typeof window !== 'undefined' ? window.localStorage : null
  } catch {
    return null
  }
}

function getDefaultLocale(): LocaleCode {
  const saved = normalizeLocale(readStorageWithLegacyKey(getLocaleStorage(), LOCALE_KEY, 'sub2api_locale'))
  if (saved) return saved
  const cookie = typeof document === 'undefined' ? undefined : document.cookie.split('; ').find(item => item.startsWith(`${LOCALE_KEY}=`))?.split('=')[1]
  const selected = normalizeLocale(cookie)
  if (selected) return selected
  if (typeof navigator !== 'undefined') {
    for (const language of navigator.languages || [navigator.language]) {
      const supported = normalizeLocale(language)
      if (supported) return supported
    }
  }
  return normalizeLocale(typeof window === 'undefined' ? undefined : window.__APP_CONFIG__?.default_locale) || DEFAULT_LOCALE

}

export const i18n = createI18n({
  legacy: false,
  locale: getDefaultLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages: {},
  // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
  // 这些内容是内部定义的，不存在 XSS 风险
  warnHtmlMessage: false
})

const loadedLocales = new Set<LocaleCode>()

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  if (loadedLocales.has(locale)) {
    return
  }

  const loader = localeLoaders[locale]
  if (!loader) throw new Error(`Missing language catalog: ${locale}`)
  const module = await loader()
  i18n.global.setLocaleMessage(locale, module.default)
  loadedLocales.add(locale)
}

export async function initI18n(): Promise<void> {
  const current = getLocale()
  await Promise.all([loadLocaleMessages(DEFAULT_LOCALE), loadLocaleMessages(current)])
  applyDocumentLocale(current)
}

let switchGeneration = 0
let accountSave: Promise<void> = Promise.resolve()

// 文档语言与首屏 Cookie 在一次切换中更新。
function applyDocumentLocale(locale: LocaleCode): void {
  document.documentElement.lang = locale
  document.documentElement.dir = availableLocales.find(item => item.code === locale)?.direction || 'ltr'
  document.cookie = `${LOCALE_KEY}=${locale}; Path=/; Max-Age=31536000; SameSite=Lax${location.protocol === 'https:' ? '; Secure' : ''}`
}

export async function setLocale(value: string, persistAccount = true): Promise<void> {
  const locale = normalizeLocale(value)
  if (!locale) {
    return
  }

  const generation = ++switchGeneration
  await Promise.all([loadLocaleMessages(DEFAULT_LOCALE), loadLocaleMessages(locale)])
  if (generation !== switchGeneration) return
  i18n.global.locale.value = locale
  getLocaleStorage()?.setItem(LOCALE_KEY, locale)
  applyDocumentLocale(locale)

  // 同步更新浏览器页签标题，使其跟随语言切换
  const { resolveRouteDocumentTitle } = await import('@/router/title')
  const { default: router } = await import('@/router')
  const { useAppStore } = await import('@/stores/app')
  const { useAuthStore } = await import('@/stores/auth')
  const { useAdminSettingsStore } = await import('@/stores/adminSettings')
  if (generation !== switchGeneration) return
  const route = router.currentRoute.value
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems)
  window.dispatchEvent(new CustomEvent('locale-changed', { detail: locale }))
  if (persistAccount && authStore.isAuthenticated) {
    const { apiClient } = await import('@/api/client')
    const userID = authStore.user?.id
    accountSave = accountSave.catch(() => {}).then(async () => {
      if (generation !== switchGeneration || authStore.user?.id !== userID) return
      await apiClient.put('/user', { preferred_locale: locale })
      if (generation === switchGeneration && authStore.user?.id === userID && authStore.user) authStore.user.preferred_locale = locale
    })
    await accountSave
  }
}

export function getLocale(): LocaleCode {
  const current = i18n.global.locale.value
  return normalizeLocale(current) || DEFAULT_LOCALE
}


export default i18n
