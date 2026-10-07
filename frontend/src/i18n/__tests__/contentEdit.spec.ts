import { describe, expect, it } from 'vitest'
import type { LocalizedUpdate } from '../content'
import {
  addTranslation,
  adoptSourceLocale,
  markStillValid,
  promoteToOriginal,
  removeTranslation,
  setSourceLocale,
  translationStatus,
  translationSummary,
  updateSource,
  updateTranslation,
  withDefaultSource,
} from '../contentEdit'

// 已保存过一次的内容：原文为中文，英文译文对应当前原文版本。
function saved(): LocalizedUpdate<string> {
  return { source: '原文', source_locale: 'zh-Hans', revision: 3, source_revision: 2, translations: { en: { value: 'English', source_revision: 2 } } }
}

describe('译文编辑', () => {
  it('编辑译文时记为已核对', () => {
    const content = updateTranslation(saved(), 'en', 'Updated')
    expect(content.translations.en.value).toBe('Updated')
    expect(content.reviewed_locales).toEqual(['en'])
    expect(translationStatus(content, 'en')).toBe('translated')
  })

  it('修改原文后译文过期，确认仍然适用后恢复展示', () => {
    const changed = updateSource(saved(), '新原文')
    expect(changed.reviewed_locales).toEqual([])
    expect(translationStatus(changed, 'en')).toBe('stale')
    expect(translationSummary(changed)).toEqual({ locales: ['en'], stale: true, sourceLocaleMissing: false })
    const confirmed = markStillValid(changed, 'en')
    expect(translationStatus(confirmed, 'en')).toBe('translated')
    expect(confirmed.translations.en.value).toBe('English')
  })

  it('编辑函数返回新对象，传入的内容保持原值', () => {
    const content = saved()
    updateTranslation(content, 'en', 'Updated')
    updateSource(content, '新原文')
    expect(content).toEqual(saved())
  })

  it('原文语言未知时，修改原文填入当前语言', () => {
    const legacy: LocalizedUpdate<string> = { source: 'TokenRouter', source_locale: null, revision: 1, source_revision: 1, translations: {} }
    expect(updateSource(legacy, 'Token Router', 'en').source_locale).toBe('en')
    expect(withDefaultSource(legacy, 'en').source_locale).toBeNull()
    expect(withDefaultSource({ ...legacy, revision: 0 }, 'en').source_locale).toBe('en')
  })

  it('当前语言已有译文时，原文语言保持未知', () => {
    const legacy: LocalizedUpdate<string> = { source: '站点', source_locale: null, revision: 1, source_revision: 1, translations: { en: { value: 'Site', source_revision: 1 } } }
    expect(updateSource(legacy, '新站点', 'en').source_locale).toBeNull()
  })

  it('添加译文用原文预填，删除译文记录到 deleted_locales', () => {
    const removed = removeTranslation(saved(), 'en')
    expect(removed.translations).toEqual({})
    expect(removed.deleted_locales).toEqual(['en'])
    expect(translationStatus(removed, 'en')).toBe('missing')
    const added = addTranslation(removed, 'en')
    expect(added.translations.en.value).toBe('原文')
    expect(added.deleted_locales).toEqual([])
    expect(translationStatus(added, 'en')).toBe('translated')
  })

  it('改原文语言遇到同语言译文时报告冲突', () => {
    const result = setSourceLocale(saved(), 'en')
    expect(result.conflict).toBe(true)
    expect(result.content.source_locale).toBe('zh-Hans')
  })

  it('历史内容定下原文语言时，删除同语言的旧译文', () => {
    const legacy: LocalizedUpdate<string> = { source: 'TokenRouter', source_locale: null, revision: 1, source_revision: 1, translations: { en: { value: 'TokenRouter', source_revision: 1 }, 'zh-Hans': { value: '中转站', source_revision: 1 } } }
    const content = adoptSourceLocale(legacy, 'en')
    expect(content.source_locale).toBe('en')
    expect(content.translations.en).toBeUndefined()
    expect(content.deleted_locales).toEqual(['en'])
    expect(translationStatus(content, 'zh-Hans')).toBe('translated')
  })

  it('已知原文语言改成其他语言时，译文需要重新核对', () => {
    const result = setSourceLocale(saved(), 'ja')
    expect(result.conflict).toBe(false)
    expect(translationStatus(result.content, 'en')).toBe('stale')
  })

  it('译文设为原文后，旧原文转成译文', () => {
    const content = promoteToOriginal(saved(), 'en')
    expect(content.source).toBe('English')
    expect(content.source_locale).toBe('en')
    expect(content.translations['zh-Hans'].value).toBe('原文')
    expect(content.deleted_locales).toEqual(['en'])
    expect(translationStatus(content, 'zh-Hans')).toBe('stale')
  })

  it('同一草稿切回原文语言后，恢复的译文可以提交', () => {
    const english = promoteToOriginal(saved(), 'en')
    const restored = promoteToOriginal(english, 'zh-Hans')
    expect(restored.source).toBe('原文')
    expect(restored.translations.en.value).toBe('English')
    expect(restored.deleted_locales).not.toContain('en')
    expect(restored.deleted_locales).toEqual(['zh-Hans'])
  })
})
