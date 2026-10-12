import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import RequestIdFilterField from '../RequestIdFilterField.vue'

vi.mock('vue-i18n', async (original) => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

function mountField(modelValue = '') {
  return mount(RequestIdFilterField, {
    props: { modelValue },
    global: { stubs: { FilterField: { template: '<div><slot /></div>' } } },
  })
}

describe('RequestIdFilterField', () => {
  it('回车后写回去掉空白的 ID 并触发查询', async () => {
    const wrapper = mountField()
    await wrapper.get('input').setValue('  req-1  ')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('input').trigger('keydown.enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['req-1']])
    expect(wrapper.emitted('search')).toHaveLength(1)
  })

  it('内容没变时失焦不重复查询', async () => {
    const wrapper = mountField('req-1')
    await wrapper.get('input').trigger('blur')
    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('父级清空 ID 后输入框同步清空', async () => {
    const wrapper = mountField('req-1')
    await wrapper.setProps({ modelValue: '' })
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
  })
})
