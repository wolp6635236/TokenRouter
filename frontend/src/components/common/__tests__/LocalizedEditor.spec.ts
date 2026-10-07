import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, nextTick, ref } from 'vue'
import LocalizedEditor from '../LocalizedEditor.vue'
import type { LocalizedUpdate } from '@/i18n/content'

let wrapper: VueWrapper | undefined

// 受控父组件持有内容，表单输入和弹窗写回都经过同一个 v-model。
function editor() {
  const content = ref<LocalizedUpdate<string>>({ source: '原文', source_locale: 'zh-Hans', revision: 3, source_revision: 2, translations: { en: { value: 'English', source_revision: 2 } } })
  const parent = defineComponent({ components: { LocalizedEditor }, setup: () => ({ content }), template: '<LocalizedEditor v-model="content" label="站点名称" />' })
  wrapper = mount(parent, { attachTo: document.body, global: { plugins: [createI18n({ legacy: false, locale: 'zh-Hans', missingWarn: false, fallbackWarn: false, messages: {} })] } })
  return { wrapper, content }
}

function dialog(): HTMLElement {
  return document.body.querySelector('[role="dialog"]') as HTMLElement
}

function button(root: ParentNode, text: string): HTMLButtonElement {
  return [...root.querySelectorAll('button')].find(item => item.textContent?.includes(text) || item.getAttribute('aria-label') === text) as HTMLButtonElement
}

async function openDialog(): Promise<void> {
  await wrapper!.get('button').trigger('click')
  await nextTick()
}

async function input(element: HTMLInputElement, value: string): Promise<void> {
  element.value = value
  element.dispatchEvent(new Event('input'))
  await nextTick()
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  // 关闭动画未结束的弹窗节点会留在 body 里，下一个用例需要干净的文档。
  document.body.innerHTML = ''
})

describe('翻译编辑', () => {
  it('表单里的输入只修改原文，译文随之过期', async () => {
    const { wrapper, content } = editor()
    await wrapper.get('input').setValue('修改后')
    expect(content.value.source).toBe('修改后')
    expect(content.value.translations.en.value).toBe('English')
    expect(wrapper.get('button').text()).toContain('localization.needsUpdate')
  })

  it('弹窗里编辑译文，点完成后写回并记为已核对', async () => {
    const { content } = editor()
    await openDialog()
    const inputs = dialog().querySelectorAll('input')
    await input(inputs[inputs.length - 1], 'Updated')
    expect(content.value.translations.en.value).toBe('English')
    button(dialog(), 'localization.done').click()
    await nextTick()
    expect(content.value.translations.en.value).toBe('Updated')
    expect(content.value.reviewed_locales).toEqual(['en'])
  })

  it('取消后丢弃弹窗里的修改', async () => {
    const { content } = editor()
    await openDialog()
    button(dialog(), 'localization.removeTranslation').click()
    await nextTick()
    button(dialog(), 'common.cancel').click()
    await nextTick()
    expect(content.value.translations.en.value).toBe('English')
    expect(content.value.deleted_locales).toBeUndefined()
  })

  it('原文修改后可以确认旧译文仍然适用', async () => {
    const { wrapper, content } = editor()
    await wrapper.get('input').setValue('修改后')
    await openDialog()
    expect(dialog().textContent).toContain('localization.staleNotice')
    button(dialog(), 'localization.stillValid').click()
    await nextTick()
    button(dialog(), 'localization.done').click()
    await nextTick()
    expect(content.value.reviewed_locales).toEqual(['en'])
    expect(wrapper.get('button').text()).not.toContain('localization.needsUpdate')
  })
})
