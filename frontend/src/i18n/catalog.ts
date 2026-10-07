import manifest from '../../../backend/internal/pkg/locale/manifest.json'

export type LocaleCode = typeof manifest.locales[number]['code']

// 语言标识和兼容映射与服务端共用同一份目录。
export const defaultLocale = manifest.default as LocaleCode
export const availableLocales = manifest.locales.map(item => ({
  ...item,
  code: item.code as LocaleCode,
  flag: item.code === 'en' ? '🇺🇸' : item.code === 'zh-Hans' ? '🇨🇳' : '🌐',
}))

// 未支持的语言返回空值，调用方决定使用站点默认语言还是内容原文。
export function normalizeLocale(value: string | null | undefined): LocaleCode | undefined {
  const raw = value?.trim().replace(/_/g, '-').toLowerCase()
  if (!raw || raw.length > 35 || !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(raw)) return undefined
  return availableLocales.find(item =>
    item.code.toLowerCase() === raw || raw.startsWith(`${item.code.toLowerCase()}-`) ||
    item.aliases.some(alias => alias.toLowerCase() === raw),
  )?.code
}

// 外部控件各自声明支持范围，应用语言不使用供应商的代码。
type VendorLocales = { airwallex: 'zh' | 'en'; tencent: 'zh-cn' | 'en'; aliyun: 'cn' | 'en'; stripe: 'zh' | 'en'; turnstile: 'zh-cn' | 'en' }

export function vendorLocale<V extends keyof VendorLocales>(vendor: V, code: string): VendorLocales[V] {
  const chinese = normalizeLocale(code) === 'zh-Hans'
  const values: VendorLocales = {
    airwallex: chinese ? 'zh' : 'en',
    tencent: chinese ? 'zh-cn' : 'en',
    aliyun: chinese ? 'cn' : 'en',
    stripe: chinese ? 'zh' : 'en',
    turnstile: chinese ? 'zh-cn' : 'en',
  }
  return values[vendor]
}
