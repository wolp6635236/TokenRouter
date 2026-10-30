import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import OpenCodeFields from '../OpenCodeFields.vue'
import { openCodeCredentials, openCodeDraft } from '../openCodeDraft'
import { useProtocolCatalogFixture } from '@/__tests__/helpers/protocolCatalog'

// OpenCodeFields.spec.ts exercises the per-mode model protocol table through the
// rendered form. The shared protocol catalog fixture supplies OpenCode's native
// protocols, and a Select stub exposes the offered options so the cases can check
// protocol filtering, mode isolation, and override ownership in the parent draft.

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

useProtocolCatalogFixture()

// SelectStub renders each option label and value so tests can read the offered protocols.
const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options', 'placeholder'],
  emits: ['update:modelValue'],
  template:
    '<div data-testid="protocol-select">{{ modelValue }}|<span v-for="o in options" :key="o.value">{{ o.value }}={{ o.label }};</span></div>',
}

// mountFields mounts the table for a draft with optional mode and enabled protocols.
function mountFields(props: Record<string, unknown>) {
  return mount(OpenCodeFields, {
    props: { mode: 'payg', ...props } as never,
    global: { stubs: { Select: SelectStub, Icon: true } },
  })
}

// offeredProtocols reads the protocol values offered by the new-override selector.
function offeredProtocols(wrapper: ReturnType<typeof mountFields>) {
  const selects = wrapper.findAllComponents(SelectStub)
  return (selects.at(-1)!.props('options') as Array<{ value: string }>).map((item) => item.value)
}

describe('OpenCodeFields', () => {
  // The administrator disabled Gemini and SystemOne on the provider. The override
  // selector must offer only the remaining native protocols, named from the catalog,
  // so a model cannot be assigned a protocol the provider refuses to call.
  it('offers only enabled native protocols with catalog names', () => {
    const wrapper = mountFields({
      modelValue: openCodeDraft(),
      protocols: ['anthropic_messages', 'openai_chat_completions'],
    })
    expect(offeredProtocols(wrapper)).toEqual(['anthropic_messages', 'openai_chat_completions'])
    expect(wrapper.text()).toContain('anthropic_messages=Anthropic Messages')
    wrapper.unmount()
  })

  // A provider without an explicit protocol selection uses the catalog defaults. Every
  // native OpenCode protocol must then be selectable for a model override.
  it('offers every native protocol when the form keeps catalog defaults', () => {
    const wrapper = mountFields({ modelValue: openCodeDraft() })
    expect(offeredProtocols(wrapper)).toEqual([
      'anthropic_messages',
      'openai_responses',
      'openai_chat_completions',
      'gemini_generate_content',
      'systemone',
    ])
    wrapper.unmount()
  })

  // A Go draft holds a catalog suggestion and a manual override for the same model,
  // plus a payg override. The Go table shows the override, and deleting it restores
  // the suggestion while the payg table and the saved catalog table stay unchanged.
  it('edits the selected mode and restores catalog suggestions after deletion', async () => {
    const draft = reactive(
      openCodeDraft({
        opencode_catalog_protocols: { go: { custom: 'anthropic_messages' } },
        opencode_model_protocols: {
          go: { custom: 'systemone' },
          payg: { custom: 'openai_responses' },
        },
      }),
    )
    const wrapper = mountFields({ modelValue: draft, mode: 'go' })
    expect(wrapper.findAllComponents(SelectStub)[0]!.props('modelValue')).toBe('systemone')

    await wrapper.get('button[aria-label="common.delete"]').trigger('click')

    expect(wrapper.findAllComponents(SelectStub)[0]!.props('modelValue')).toBe('anthropic_messages')
    expect(wrapper.text()).toContain('admin.providers.opencode.catalogSuggestion')
    expect(openCodeCredentials(draft)).toEqual({
      opencode_catalog_protocols: { payg: {}, go: { custom: 'anthropic_messages' } },
      opencode_model_protocols: { payg: { custom: 'openai_responses' }, go: {} },
    })
    wrapper.unmount()
  })

  // The administrator picks a protocol for a suggested model and adds a new model row.
  // Both choices must land in the selected mode's manual table and leave suggestions
  // untouched, so later catalog updates cannot overwrite them.
  it('stores manual choices separately from catalog suggestions', async () => {
    const draft = reactive(
      openCodeDraft({ opencode_catalog_protocols: { payg: { known: 'anthropic_messages' } } }),
    )
    const wrapper = mountFields({ modelValue: draft })

    wrapper.findAllComponents(SelectStub)[0]!.vm.$emit('update:modelValue', 'openai_responses')
    await wrapper.get('input').setValue(' fresh ')
    wrapper.findAllComponents(SelectStub).at(-1)!.vm.$emit('update:modelValue', 'systemone')
    await wrapper.vm.$nextTick()
    await wrapper.findAll('button').at(-1)!.trigger('click')

    expect(draft.protocols.payg).toEqual({ known: 'openai_responses', fresh: 'systemone' })
    expect(draft.suggestions.payg).toEqual({ known: 'anthropic_messages' })
    expect(draft.protocols.go).toEqual({})
    wrapper.unmount()
  })
})
