import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProviderTodayStatsCell from '../ProviderTodayStatsCell.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const originalConfig = window.__APP_CONFIG__

afterEach(() => {
  window.__APP_CONFIG__ = originalConfig
})

describe('ProviderTodayStatsCell', () => {
  it('提供商计费用美元，用户扣费用配置的余额符号', () => {
    window.__APP_CONFIG__ = {
      ...originalConfig,
      balance_unit_symbol: '🍥'
    }

    const wrapper = mount(ProviderTodayStatsCell, {
      props: {
        stats: { requests: 2224, tokens: 287860000, cost: 190.15, user_cost: 4754.97 }
      },
      global: { plugins: [createPinia()] }
    })

    expect(wrapper.text()).toContain('usage.providerBilled:$190.15')
    expect(wrapper.text()).toContain('usage.userBilled:🍥4,754.97')
  })
})
