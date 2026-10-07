import type { LocalizedUpdate } from './content'

// 译文在编辑器里的状态：有效译文对用户展示，过期译文要等管理员确认或修改。
export type TranslationStatus = 'translated' | 'stale' | 'missing'

// 编辑函数返回新的草稿对象，调用方持有的内容保持原值。
function copy<T>(content: LocalizedUpdate<T>): LocalizedUpdate<T> {
  return JSON.parse(JSON.stringify(content)) as LocalizedUpdate<T>
}

function withLocale(list: string[] | undefined, code: string): string[] {
  return [...new Set([...(list || []), code])]
}

function withoutLocale(list: string[] | undefined, code: string): string[] {
  return (list || []).filter(item => item !== code)
}

// 原文或原文语言变化后，服务端会推进原文版本，未重新核对的译文暂停展示。
function invalidateTranslations<T>(content: LocalizedUpdate<T>): void {
  content.reviewed_locales = []
  for (const translation of Object.values(content.translations)) translation.source_revision = -1
}

// labelSourceLocale 给原文补上语言。原文语言原本未知时，原文内容没有变化，之前有效的译文继续有效。
function labelSourceLocale<T>(previous: LocalizedUpdate<T>, next: LocalizedUpdate<T>, code: string): void {
  const valid = previous.source_locale ? [] : Object.keys(next.translations).filter(item => translationStatus(previous, item) === 'translated')
  next.source_locale = code
  invalidateTranslations(next)
  next.reviewed_locales = valid
}

// withDefaultSource 给尚未保存过的新内容填入原文语言。
export function withDefaultSource<T>(content: LocalizedUpdate<T>, fallbackLocale?: string): LocalizedUpdate<T> {
  if (content.revision !== 0 || content.source_locale || !fallbackLocale || fallbackLocale in content.translations) return content
  return { ...content, source_locale: fallbackLocale }
}

// updateSource 写入原文。原文语言未知时使用 fallbackLocale，服务端要求修改原文时提供语言。
// fallbackLocale 已经有译文时语言保持未知，由管理员在翻译弹窗里选择。
export function updateSource<T>(content: LocalizedUpdate<T>, value: T, fallbackLocale?: string): LocalizedUpdate<T> {
  const next = copy(content)
  next.source = value
  if (!next.source_locale && fallbackLocale && !(fallbackLocale in next.translations)) next.source_locale = fallbackLocale
  invalidateTranslations(next)
  return next
}

// updateTranslation 写入译文。管理员对照当前原文编辑译文，所以同时记为已核对。
export function updateTranslation<T>(content: LocalizedUpdate<T>, code: string, value: T): LocalizedUpdate<T> {
  const next = copy(content)
  next.translations[code] = { value, source_revision: -1 }
  next.reviewed_locales = withLocale(next.reviewed_locales, code)
  return next
}

// markStillValid 确认原文修改后旧译文仍然可用。
export function markStillValid<T>(content: LocalizedUpdate<T>, code: string): LocalizedUpdate<T> {
  const next = copy(content)
  next.reviewed_locales = withLocale(next.reviewed_locales, code)
  return next
}

// addTranslation 用原文预填新译文，管理员在此基础上修改。
export function addTranslation<T>(content: LocalizedUpdate<T>, code: string): LocalizedUpdate<T> {
  const next = copy(content)
  next.deleted_locales = withoutLocale(next.deleted_locales, code)
  next.translations[code] = { value: copy(content).source, source_revision: -1 }
  next.reviewed_locales = withLocale(next.reviewed_locales, code)
  return next
}

// removeTranslation 删除译文，并通过 deleted_locales 通知服务端。
export function removeTranslation<T>(content: LocalizedUpdate<T>, code: string): LocalizedUpdate<T> {
  const next = copy(content)
  delete next.translations[code]
  next.deleted_locales = withLocale(next.deleted_locales, code)
  next.reviewed_locales = withoutLocale(next.reviewed_locales, code)
  return next
}

// setSourceLocale 修改原文语言。该语言已有译文时返回 conflict，内容保持原值。
export function setSourceLocale<T>(content: LocalizedUpdate<T>, code: string): { content: LocalizedUpdate<T>; conflict: boolean } {
  if (code in content.translations) return { content, conflict: true }
  const next = copy(content)
  labelSourceLocale(content, next, code)
  return { content: next, conflict: false }
}

// adoptSourceLocale 把原文语言定为 code，并删除同语言的旧译文，用于原文语言未知的历史内容。
export function adoptSourceLocale<T>(content: LocalizedUpdate<T>, code: string): LocalizedUpdate<T> {
  const next = code in content.translations ? removeTranslation(content, code) : copy(content)
  labelSourceLocale(content, next, code)
  return next
}

// promoteToOriginal 把译文设为原文。原文语言已知时，旧原文转成该语言的译文。
export function promoteToOriginal<T>(content: LocalizedUpdate<T>, code: string): LocalizedUpdate<T> {
  const translation = content.translations[code]
  if (!translation) return content
  const next = copy(content)
  if (next.source_locale) {
    next.translations[next.source_locale] = { value: next.source, source_revision: -1 }
    next.deleted_locales = withoutLocale(next.deleted_locales, next.source_locale)
  }
  next.source = copy(content).translations[code].value
  next.source_locale = code
  delete next.translations[code]
  next.deleted_locales = withLocale(next.deleted_locales, code)
  invalidateTranslations(next)
  return next
}

// translationStatus 按服务端的展示条件判断译文状态，本次草稿里核对过的译文算作有效。
export function translationStatus<T>(content: LocalizedUpdate<T>, code: string): TranslationStatus {
  const translation = content.translations[code]
  if (!translation) return 'missing'
  if (content.reviewed_locales?.includes(code)) return 'translated'
  return translation.source_revision === content.source_revision ? 'translated' : 'stale'
}

// translationSummary 汇总已有译文的语言、是否有译文暂停展示，以及是否需要选择原文语言。
// 编辑过原文的草稿带有 reviewed_locales，这时原文语言仍然未知，服务端会拒绝保存。
export function translationSummary<T>(content: LocalizedUpdate<T>): { locales: string[]; stale: boolean; sourceLocaleMissing: boolean } {
  const locales = Object.keys(content.translations)
  return {
    locales,
    stale: locales.some(code => translationStatus(content, code) === 'stale'),
    sourceLocaleMissing: !content.source_locale && content.reviewed_locales !== undefined,
  }
}
