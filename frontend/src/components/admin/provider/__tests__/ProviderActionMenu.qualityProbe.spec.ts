import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ProviderActionMenu from '../ProviderActionMenu.vue'
import type { Provider } from '@/types'

const getAvailableModels = vi.fn()

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    providers: {
      getAvailableModels: (...args: unknown[]) => getAvailableModels(...args),
    },
  },
}))

function makeProvider(overrides: Partial<Provider> = {}): Provider {
  return {
    id: 12,
    name: 'usfast',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 3,
    priority: 50,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

describe('ProviderActionMenu quality probe models', () => {
  beforeEach(() => {
    getAvailableModels.mockReset()
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-6-astra', display_name: 'GPT-6 Astra' },
      { id: 'gpt-5.6-terra', display_name: 'GPT-5.6 Terra' },
    ])
  })

  it('悬停降智探测时默认勾选全部模型，取消一个后只探测剩下的', async () => {
    const provider = makeProvider()
    const wrapper = mount(ProviderActionMenu, {
      props: { show: true, provider, position: { top: 80, left: 80 } },
      attachTo: document.body,
    })
    const probeButton = Array.from(document.body.querySelectorAll('button')).find(button =>
      button.textContent?.includes('admin.providers.qualityProbe'),
    )
    expect(probeButton).toBeDefined()
    await probeButton!.parentElement!.dispatchEvent(new Event('mouseenter'))
    await flushPromises()
    expect(getAvailableModels).toHaveBeenCalledWith(12)

    const boxes = Array.from(document.body.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'))
    expect(boxes).toHaveLength(3)
    expect(boxes.every(box => box.checked)).toBe(true)

    const terra = Array.from(document.body.querySelectorAll('label')).find(label =>
      label.textContent?.includes('GPT-5.6 Terra'),
    )
    expect(terra).toBeDefined()
    terra!.querySelector('input')!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('quality-probe')).toBeUndefined()

    const start = Array.from(document.body.querySelectorAll('button')).find(button =>
      button.textContent?.includes('admin.providers.qualityProbeStart'),
    )
    expect(start).toBeDefined()
    start!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('quality-probe')?.[0]).toEqual([provider, ['gpt-6-astra']])
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
