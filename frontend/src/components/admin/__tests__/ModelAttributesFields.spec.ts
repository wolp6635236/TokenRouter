import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelAttributesFields from '../ModelAttributesFields.vue'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))

describe('模型属性编辑', () => {
  it('显式 false 与继承分别保存，空模态不会变成继承', async () => {
    const wrapper = mount(ModelAttributesFields, { props: { modelValue: { reasoning: true } }, global: { stubs: { Select: true } } })
    wrapper.findAllComponents(Select).find(component => component.attributes('aria-label') === 'admin.modelAttributes.fields.reasoning')!.vm.$emit('update:modelValue', 'false')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({ reasoning: false })
    wrapper.findAllComponents(Select).find(component => component.attributes('aria-label') === 'admin.modelAttributes.fields.reasoning')!.vm.$emit('update:modelValue', 'inherit')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({})
    await wrapper.find('fieldset input[type="checkbox"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({ reasoning: true, input_modalities: [] })
    wrapper.unmount()
  })
})
