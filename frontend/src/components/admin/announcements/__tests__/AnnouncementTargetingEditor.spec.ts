import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AnnouncementTargetingEditor from '../AnnouncementTargetingEditor.vue'
import type { AnnouncementTargeting } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

// 使用真实列表外壳，省略选择框浮层和动画以检查数据及行身份。
function mountEditor(value: AnnouncementTargeting) {
  const wrapper = mount(AnnouncementTargetingEditor, {
    attachTo: document.body,
    props: {
      modelValue: value,
      plans: [],
      'onUpdate:modelValue': updated => { void wrapper.setProps({ modelValue: updated }) },
    },
    global: { stubs: { Select: true, Icon: true, 'transition-group': true } },
  })
  return wrapper
}

const balanceGroup = (value: number) => ({ all_of: [{ type: 'balance' as const, operator: 'gte' as const, value }] })

describe('AnnouncementTargetingEditor', () => {
  it('克隆更新保留焦点和行身份，提交数据不包含展示 key', async () => {
    const original = { any_of: [balanceGroup(1), balanceGroup(2)] }
    const wrapper = mountEditor(original)
    const input = wrapper.findAll('input[type="number"]')[1]
    const element = input.element as HTMLInputElement
    element.focus()
    await input.setValue('25')
    await flushPromises()
    expect(document.activeElement).toBe(element)
    expect(original.any_of[1].all_of[0].value).toBe(2)
    expect(wrapper.props('modelValue')).toEqual({ any_of: [balanceGroup(1), balanceGroup(25)] })
    await wrapper.get('[data-testid="announcement-groups-remove-0"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('input[type="number"]').element).toBe(element)
    expect(wrapper.findAll('[data-testid="announcement-groups-row"]')).toHaveLength(1)
    wrapper.unmount()
  })

  it('删除 AND 条件后保留剩余输入，并沿用空组校验', async () => {
    const wrapper = mountEditor({ any_of: [{ all_of: [...balanceGroup(1).all_of, ...balanceGroup(2).all_of] }] })
    const second = wrapper.findAll('input[type="number"]')[1].element
    await wrapper.get('[data-testid="announcement-conditions-0-remove-0"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('input[type="number"]').element).toBe(second)
    await wrapper.get('[data-testid="announcement-conditions-0-remove-0"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.announcements.form.addAndCondition')
    wrapper.unmount()
  })

  it('OR 和 AND 条目达到 50 时禁用各自的添加按钮', () => {
    const wrapper = mountEditor({ any_of: Array.from({ length: 50 }, (_, index) => index === 0 ? { all_of: Array.from({ length: 50 }, () => balanceGroup(1).all_of[0]) } : balanceGroup(1)) })
    expect(wrapper.get('[data-testid="announcement-groups-add"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="announcement-conditions-0-add"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
