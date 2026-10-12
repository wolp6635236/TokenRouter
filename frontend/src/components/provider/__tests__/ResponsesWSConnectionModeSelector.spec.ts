import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import ResponsesWSConnectionModeSelector from '../ResponsesWSConnectionModeSelector.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

// SelectStub 用原生 select 代替下拉组件，便于直接选择选项。
const SelectStub = defineComponent({
  props: { modelValue: String, options: Array },
  emits: ['update:modelValue'],
  setup(props, { emit, attrs }) {
    return () => h('select', {
      ...attrs,
      value: props.modelValue,
      onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLSelectElement).value),
    }, (props.options as { value: string; label: string }[]).map(option => h('option', { value: option.value }, option.label)))
  },
})

function mountSelector(modelValue: 'pooled' | 'per_session') {
  return mount(ResponsesWSConnectionModeSelector, {
    props: { modelValue },
    global: { stubs: { Select: SelectStub } },
  })
}

describe('ResponsesWSConnectionModeSelector', () => {
  it('shows the section title, scope and the description of the current mode', () => {
    const text = mountSelector('per_session').text()

    expect(text).toContain('admin.providers.openai.wsConnectionTitle')
    expect(text).toContain('admin.providers.openai.wsConnectionScope')
    expect(text).toContain('admin.providers.openai.wsConnectionPerSessionHint')
  })

  it('emits the selected connection mode', async () => {
    const wrapper = mountSelector('pooled')

    await wrapper.get('[data-testid="responses-ws-connection-select"]').setValue('per_session')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['per_session'])
  })
})
