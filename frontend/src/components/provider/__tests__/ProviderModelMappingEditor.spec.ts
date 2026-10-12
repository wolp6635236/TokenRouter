import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'

import ProviderModelMappingEditor from '../ProviderModelMappingEditor.vue'
import type { ModelMappingRow } from '@/utils/modelMappingRules'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

const mountEditor = (rows: ModelMappingRow[], props: Record<string, unknown> = {}) => {
  wrapper = mount(ProviderModelMappingEditor, {
    props: { modelValue: rows, testId: 'mapping', ...props },
    global: { stubs: { Icon: true, 'transition-group': true } },
  })
  return wrapper
}

describe('ProviderModelMappingEditor', () => {
  it('默认显示提供商映射说明、空态和请求模型占位', () => {
    mountEditor([])
    expect(wrapper!.text()).toContain('admin.providers.mapRequestModels')
    expect(wrapper!.text()).toContain('admin.providers.modelMappingEmpty')
    expect(wrapper!.get('[data-testid="mapping-add"]').text()).toContain('admin.providers.addMapping')

    mountEditor([{ from: '', to: '' }])
    expect(wrapper!.get('[data-testid="mapping-source-0"]').attributes('placeholder'))
      .toBe('admin.providers.requestModel')
  })

  it('开启通配符提示后实时显示错误', () => {
    mountEditor([{ from: 'a*b', to: 'c*' }], { wildcardValidation: true })

    const alerts = wrapper!.findAll('[role="alert"]').map((alert) => alert.text())
    expect(alerts).toEqual(['admin.providers.wildcardOnlyAtEnd', 'admin.providers.targetNoWildcard'])
  })

  it('未开启通配符提示时不显示错误', () => {
    mountEditor([{ from: 'a*b', to: 'c*' }])
    expect(wrapper!.find('[role="alert"]').exists()).toBe(false)
  })

  it('透传增删事件', async () => {
    const rows = [{ from: 'a', to: 'b' }]
    mountEditor(rows)

    await wrapper!.get('[data-testid="mapping-add"]').trigger('click')
    await wrapper!.get('[data-testid="mapping-remove-0"]').trigger('click')

    expect(wrapper!.emitted('add')).toHaveLength(1)
    expect(wrapper!.emitted('remove')?.[0]?.[1]).toBe(0)
  })
})
