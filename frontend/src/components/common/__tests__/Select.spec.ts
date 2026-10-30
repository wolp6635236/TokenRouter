import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

// Select.spec.ts tests horizontal placement of a teleported selection menu in a narrow
// viewport. A mocked trigger rectangle near the right edge and a long option label
// exercise viewport clamping while assertions inspect the resulting menu dimensions.

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

// originalInnerWidth retains the test environment viewport width so teardown restores
// it after the constrained-menu case.
const originalInnerWidth = window.innerWidth

// Release the resources retained by this case before the next one, keeping browser
// state and component lifecycle observations isolated across assertions.
afterEach(() => {
  document.body.innerHTML = ''
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: originalInnerWidth,
  })
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  // A narrow viewport contains a trigger near its right edge and a long option label.
  // The teleported panel must clamp its horizontal offset and retain the expected
  // minimum and maximum widths.
  it('repositions the teleported dropdown within the viewport', async () => {
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      value: 320,
    })

    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 220,
      y: 20,
      top: 20,
      right: 300,
      bottom: 60,
      left: 220,
      width: 80,
      height: 40,
      toJSON: () => ({}),
    })

    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        options: [
          {
            value: 'example',
            label: 'very-long-unbroken-option-value-that-must-not-overflow',
          },
        ],
      },
    })

    await wrapper.get('button').trigger('click')
    await nextTick()
    await nextTick()

    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('104px')
    expect(dropdown?.style.minWidth).toBe('80px')
    expect(dropdown?.style.maxWidth).toBe('288px')

    wrapper.unmount()
  })
})

// An exact search creates a manual action beside the existing catalog option.
// Distinct render keys keep both choices mounted without Vue duplicate-key warnings.
it('keeps creation and existing options distinct for an exact query', async () => {
  const warnings = vi.spyOn(console, 'warn').mockImplementation(() => {})
  const wrapper = mount(Select, {
    props: {
      modelValue: '',
      searchable: true,
      creatable: true,
      options: [{ value: 'target', label: 'target' }],
    },
  })
  await wrapper.get('button').trigger('click')
  await nextTick()
  const search = document.body.querySelector<HTMLInputElement>('.select-search-input')!
  search.value = 'target'
  search.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  const options = [...document.body.querySelectorAll<HTMLElement>('[role="option"]')]
  expect(options).toHaveLength(2)
  expect(warnings.mock.calls.flat().join(' ')).not.toContain('Duplicate keys')
  options[1]!.click()
  await nextTick()
  expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['target'])
  wrapper.unmount()
})
