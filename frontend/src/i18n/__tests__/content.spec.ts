import { afterEach, describe, expect, it, vi } from 'vitest'
import { computed, ref } from 'vue'
import { newContentID, resolveContent, type LocalizedUpdate } from '../content'
import { markStillValid, removeTranslation, updateSource, updateTranslation } from '../contentEdit'

afterEach(() => vi.restoreAllMocks())

describe('内容标识', () => {
  it('浏览器 UUID 符合菜单接口的长度和字符限制', () => {
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue('00112233-4455-6677-8899-aabbccddeeff')
    expect(newContentID()).toBe('00112233445566778899aabbccddeeff')
  })

  it('普通 HTTP 环境在同一毫秒内也能生成不同的短标识', () => {
    vi.spyOn(globalThis, 'crypto', 'get').mockReturnValue(undefined as unknown as Crypto)
    vi.spyOn(Date, 'now').mockReturnValue(1791200000000)
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    const ids = Array.from({ length: 1000 }, () => newContentID())
    expect(new Set(ids).size).toBe(ids.length)
    for (const id of ids) expect(id).toMatch(/^[a-zA-Z0-9_-]{1,32}$/)
  })
})

describe('多语言草稿预览', () => {
  it('随原文、译文和语言切换更新，并跳过过期或删除的译文', () => {
    const draft = ref<LocalizedUpdate<string>>({
      source_locale: 'en', source: 'Credits', revision: 2, source_revision: 1,
      translations: { 'zh-Hans': { value: '积分', source_revision: 1 } },
    })
    const language = ref('zh-Hans')
    const preview = computed(() => resolveContent(draft.value, language.value).value)
    expect(preview.value).toBe('积分')
    draft.value = updateTranslation(draft.value, 'zh-Hans', '点数')
    expect(preview.value).toBe('点数')
    language.value = 'en'
    expect(preview.value).toBe('Credits')
    draft.value = updateSource(draft.value, 'Tokens')
    expect(preview.value).toBe('Tokens')
    language.value = 'zh-Hans'
    expect(preview.value).toBe('Tokens')
    draft.value = markStillValid(draft.value, 'zh-Hans')
    expect(preview.value).toBe('点数')
    draft.value = removeTranslation(draft.value, 'zh-Hans')
    expect(preview.value).toBe('Tokens')
  })
})
