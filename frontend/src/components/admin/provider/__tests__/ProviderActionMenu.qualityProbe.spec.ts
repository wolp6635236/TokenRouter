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

  it('悬停降智探测时列出可用模型，点击后带上模型发起探测', async () => {
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
    const terra = Array.from(document.body.querySelectorAll('button')).find(button =>
      button.textContent?.includes('GPT-5.6 Terra'),
    )
    expect(terra).toBeDefined()
    terra!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('quality-probe')?.[0]).toEqual([provider, 'gpt-5.6-terra'])
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
