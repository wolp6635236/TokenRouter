import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SystemOneBoxAnswer from '../SystemOneBoxAnswer.vue'

/**
 * SystemOneBoxAnswer.spec.ts checks normalized overlay geometry and unknown confidence display.
 * Image load events are explicit because jsdom does not decode data URLs; missing-image and
 * source-replacement cases preserve numeric coordinates without displaying a stale overlay.
 */

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('Box result', () => {
  // A non-square image displays a fractional rectangle after it loads. Percent geometry
  // uses its image-sized wrapper, and a null confidence remains explicitly unavailable.
  it('uses image-relative coordinates and preserves null confidence', async () => {
    const wrapper = mount(SystemOneBoxAnswer, {
      props: {
        box: { top: 0.25, bottom: 0.75, left: 0, right: 1 },
        confidence: null,
        image: { name: 'photo.png', src: 'data:image/png;base64,aW1hZ2U=' },
      },
    })
    expect(wrapper.find('[data-testid="decision-box-overlay"]').exists()).toBe(false)
    await wrapper.get('img').trigger('load')
    const overlay = wrapper.get<HTMLElement>('[data-testid="decision-box-overlay"]').element
    expect(overlay.style.top).toBe('25%')
    expect(overlay.style.left).toBe('0%')
    expect(overlay.style.width).toBe('100%')
    expect(overlay.style.height).toBe('50%')
    expect(wrapper.text()).toContain('confidenceUnavailable')
    await wrapper.setProps({ confidence: 0 })
    expect(wrapper.text()).toContain('0.00%')
    await wrapper.get('img').trigger('error')
    expect(wrapper.find('[data-testid="decision-box-overlay"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('imageUnavailable')
    expect(wrapper.text()).toContain('0.25')
    wrapper.unmount()
  })
})
