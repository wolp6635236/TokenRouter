import messages from '../../../backend/internal/pkg/locale/error_messages.json'
import { getLocale } from './index'
import { normalizeLocale, defaultLocale } from './catalog'

// 客户端错误和翻译编辑错误按当前界面语言生成提示。
export function localizedErrorMessage(reason: unknown, status: number, original = ''): string {
  const code = typeof reason === 'string' ? reason.toUpperCase() : ''
  const language = normalizeLocale(getLocale()) || defaultLocale
  const dictionary = (messages as Record<string, Record<string, string>>)[language] || messages.en
  return dictionary[code] || original || dictionary[`HTTP_${status}`] || dictionary.REQUEST_FAILED
}

// 服务端负责业务错误的语言，管理端翻译编辑错误使用客户端词条。
export function responseErrorMessage(path: string, reason: unknown, status: number, original = ''): string {
  const code = typeof reason === 'string' ? reason.toUpperCase() : ''
  const contentError = /^(LOCALIZATION_|SOURCE_LOCALE_|INVALID_LOCALE|INVALID_TRANSLATION_|DUPLICATE_LOCALE|UNKNOWN_LOCALIZED_|LOCALIZED_|SITE_NAME_REQUIRED|LEGAL_|NAVIGATION_LABEL_|INVALID_LINK_URL|DUPLICATE_CONTENT_ID|LOCALIZED_PAGE_|ENDPOINT_NAME_REQUIRED|PLAN_NAME_REQUIRED|PLAN_TEXT_TOO_LONG|GROUP_DISPLAY_|GROUP_DESCRIPTION_|MODEL_DISPLAY_|INVALID_BLOCK_MESSAGE|INVALID_CUSTOM_MESSAGE|PAYMENT_METHOD_NAME_REQUIRED)/.test(code)
  if (original.trim() && (!path.includes('/admin/') || !contentError)) return original
  return localizedErrorMessage(reason, status, original)
}
