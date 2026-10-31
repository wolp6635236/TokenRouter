import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SystemOneTestForm from '../SystemOneTestForm.vue'
import type { SystemOneImageDraft } from '../systemOneImages'
import type { SystemOneTestPayload, SystemOneTestPreview } from '../systemOneTest'

/**
 * SystemOneTestForm.spec.ts checks image placement, Box target selection, and failed file reads.
 * The image decoder is replaced with named local fixtures while the real form builds requests,
 * validates references, and emits the separate preview snapshot used by completed results.
 */

/**
 * readImage controls asynchronous file completion so tests can exercise successful, failed,
 * and stale reads without relying on jsdom's absent image decoder.
 */
const { readImage } = vi.hoisted(() => ({ readImage: vi.fn() }))
vi.mock('../systemOneImages', async () => ({
  ...(await vi.importActual('../systemOneImages')),
  readSystemOneImage: readImage,
}))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key }),
}))

/**
 * imageFixture supplies one immutable-looking draft for form request and preview assertions.
 */
function imageFixture(): SystemOneImageDraft {
  return {
    reference: '<photo>',
    name: 'photo.png',
    src: 'data:image/png;base64,aW1hZ2U=',
    width: 400,
    height: 300,
    asset: { type: 'image', format: 'png', data_base64: 'aW1hZ2U=' },
  }
}

/**
 * mountForm retains real text fields and replaces only the popup selector.
 * Native select events exercise the same controlled value updates without needing popup
 * geometry; attachment permits reference insertion to restore text-field focus.
 */
function mountForm(onChange = vi.fn()) {
  return mount(SystemOneTestForm, {
    props: { disabled: false, onChange },
    attachTo: document.body,
    global: {
      stubs: {
        Select: {
          props: ['modelValue', 'options', 'disabled'],
          emits: ['update:modelValue'],
          template:
            '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option value=""></option><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>',
        },
      },
    },
  })
}

/**
 * upload supplies a file selection and waits for the form's asynchronous read and validation.
 */
async function upload(wrapper: ReturnType<typeof mountForm>) {
  const input = wrapper.get('[data-testid="decision-image-upload"]')
  Object.defineProperty(input.element, 'files', {
    value: [new File(['image'], 'photo.png')],
    configurable: true,
  })
  await input.trigger('change')
  await flushPromises()
}

/**
 * submitted reads the latest payload and preview emitted by the form's validation watcher.
 */
function submitted(wrapper: ReturnType<typeof mountForm>) {
  return wrapper.emitted('change')!.at(-1)! as [SystemOneTestPayload | null, SystemOneTestPreview]
}

describe('SystemOne images form', () => {
  beforeEach(() => readImage.mockReset().mockResolvedValue(imageFixture()))
  afterEach(() => {
    document.body.innerHTML = ''
  })

  // An uploaded file remains unused until selected as a Box target. The resulting payload
  // carries only upstream fields, and removing the file invalidates that target immediately.
  it('requires a Box target and keeps preview metadata separate', async () => {
    const wrapper = mountForm()
    await upload(wrapper)
    expect(submitted(wrapper)[0]).toBeNull()
    await wrapper.get('[data-testid="decision-type-0"]').setValue('box')
    expect(wrapper.find('[data-testid="decision-options-0"]').exists()).toBe(false)
    await wrapper.get('[data-testid="decision-box-image-0"]').setValue('<photo>')
    const [payload, preview] = submitted(wrapper)
    expect(payload?.questions.available).toMatchObject({
      type: 'box',
      instructions: expect.stringContaining('Image: <photo>'),
    })
    expect(payload?.questions.available).not.toHaveProperty('criteria')
    expect(payload?.assets?.['<photo>'].data_base64).toBe('aW1hZ2U=')
    expect(preview.boxImages.available).toBe('<photo>')
    expect(preview.images['<photo>'].name).toBe('photo.png')
    await wrapper.get('[data-testid="decision-image-remove"]').trigger('click')
    expect(submitted(wrapper)[0]).toBeNull()
    expect(preview.images['<photo>'].name).toBe('photo.png')
    wrapper.unmount()
  })

  // Reference insertion targets the most recently focused text selection. Removing the image
  // then blocks stale placeholders until the administrator repairs the remaining text.
  it('inserts at the text selection and rejects deleted references', async () => {
    const wrapper = mountForm()
    await upload(wrapper)
    const state = wrapper.get<HTMLTextAreaElement>('[data-testid="systemone-state"] textarea')
    await state.setValue('before after')
    state.element.focus()
    state.element.setSelectionRange(7, 7)
    await wrapper.get('[data-testid="decision-image-insert"]').trigger('click')
    expect(submitted(wrapper)[0]?.state).toBe('before <photo>after')
    await wrapper.get('[data-testid="decision-image-remove"]').trigger('click')
    expect(submitted(wrapper)[0]).toBeNull()
    expect(wrapper.text()).toContain('removedImageReference')
    await state.setValue('repaired')
    expect(submitted(wrapper)[0]?.state).toBe('repaired')
    wrapper.unmount()
  })

  // Switching a focused state field to JSON removes it as an insertion destination.
  // The action stays disabled until another eligible text field is selected, then inserts there.
  it('clears the text-state insertion destination when switching to JSON', async () => {
    const wrapper = mountForm()
    await upload(wrapper)
    const state = wrapper.get<HTMLTextAreaElement>('[data-testid="systemone-state"] textarea')
    await state.setValue('{"photo":""}')
    state.element.focus()
    await wrapper
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'JSON')!
      .trigger('click')
    const insert = wrapper.get('[data-testid="decision-image-insert"]')
    expect(insert.attributes('disabled')).toBeDefined()
    const instructions = wrapper.get<HTMLTextAreaElement>(
      '[data-testid="decision-instructions-0"] textarea',
    )
    await instructions.setValue('Check ')
    instructions.element.focus()
    instructions.element.setSelectionRange(6, 6)
    await wrapper.vm.$nextTick()
    expect(insert.attributes('disabled')).toBeUndefined()
    await insert.trigger('click')
    expect(submitted(wrapper)[0]?.state).toEqual({ photo: '' })
    expect(submitted(wrapper)[0]?.questions.available.instructions).toBe('Check <photo>')
    wrapper.unmount()
  })

  // Structured state references are checked in values rather than keys. Failed reads retain
  // the previously accepted image and the same JSON state for correction or another attempt.
  it('validates JSON references and retains the draft after read failure', async () => {
    const wrapper = mountForm()
    await upload(wrapper)
    await wrapper
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'JSON')!
      .trigger('click')
    const state = wrapper.get('[data-testid="systemone-state"] textarea')
    await state.setValue('{"<photo>":"text"}')
    expect(submitted(wrapper)[0]).toBeNull()
    await state.setValue('{"image":"<photo>"}')
    expect(submitted(wrapper)[0]?.state).toEqual({ image: '<photo>' })
    readImage.mockRejectedValueOnce(new Error('invalidImageFormat'))
    await upload(wrapper)
    expect(wrapper.text()).toContain('invalidImageFormat')
    expect(submitted(wrapper)[0]?.state).toEqual({ image: '<photo>' })
    expect(wrapper.findAll('[data-testid="decision-image"]')).toHaveLength(1)
    wrapper.unmount()
  })

  // A file read finishes after the form closes. The generation guard prevents a late image
  // from publishing another change event or restoring a payload belonging to the old dialog.
  it('discards a file read that completes after unmount', async () => {
    let finish!: (image: SystemOneImageDraft) => void
    readImage.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    const onChange = vi.fn()
    const wrapper = mountForm(onChange)
    await upload(wrapper)
    const count = onChange.mock.calls.length
    wrapper.unmount()
    finish(imageFixture())
    await flushPromises()
    expect(onChange).toHaveBeenCalledTimes(count)
  })
})
