import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'

import UserTokenRanking from '../UserTokenRanking.vue'

const getUserBreakdown = vi.fn()

vi.mock('@/api/admin/dashboard', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/admin/dashboard')>()),
  getUserBreakdown: (...args: unknown[]) => getUserBreakdown(...args),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const item = (id: number, tokens: number) => ({
  user_id: id,
  email: `u${id}@test.com`,
  requests: 1,
  input_tokens: tokens,
  output_tokens: 0,
  cache_tokens: 0,
  total_tokens: tokens,
  actual_cost: 0.5,
})

const mountRanking = (props: Record<string, unknown> = {}) =>
  mount(UserTokenRanking, {
    props: {
      startDate: '2026-07-01',
      endDate: '2026-07-08',
      filters: {},
      ...props,
    },
    global: { plugins: [createPinia()], stubs: { Select: true } },
  })

const originalConfig = window.__APP_CONFIG__

describe('UserTokenRanking', () => {
  beforeEach(() => {
    getUserBreakdown.mockReset()
    getUserBreakdown.mockResolvedValue({ users: [item(1, 100), item(2, 50)] })
  })

  afterEach(() => {
    window.__APP_CONFIG__ = originalConfig
  })

  it('消费列用配置的余额符号显示用户实际扣费', async () => {
    window.__APP_CONFIG__ = { ...originalConfig, balance_unit_symbol: '🍥' }
    const wrapper = mountRanking()
    await flushPromises()

    const costCell = wrapper.findAll('tbody tr')[0].findAll('td').at(-1)!
    expect(costCell.text()).toBe('🍥0.5000')
    wrapper.unmount()
  })

  it('loads on mount with shared filters and emits select-user with id + email on row click', async () => {
    const wrapper = mountRanking({ filters: { group_id: 3 }, model: 'claude-fable-5' })
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 3,
      model: 'claude-fable-5',
      start_date: '2026-07-01',
      end_date: '2026-07-08',
      sort_by: 'total_tokens',
      limit: 50,
    }))

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)

    await rows[0].trigger('click')
    expect(wrapper.emitted('select-user')![0]).toEqual([1, 'u1@test.com'])
  })

  it('reloads when shared filters change', async () => {
    const wrapper = mountRanking()
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ filters: { user_id: 9 } })
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledTimes(2)
    expect(getUserBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 9 }))
  })

  it('等待数据时保留表头和列数，完成后恢复可点击的数据行', async () => {
    let finish!: (value: { users: ReturnType<typeof item>[] }) => void
    getUserBreakdown.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountRanking()
    await flushPromises()

    const headerCount = wrapper.findAll('thead th').length
    expect(wrapper.get('table').attributes('aria-busy')).toBe('true')
    expect(wrapper.get('tbody').attributes('aria-label')).toBe('common.loading')
    expect(wrapper.findAll('tbody tr')).toHaveLength(5)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(headerCount)
    expect(wrapper.text()).not.toContain('admin.dashboard.noDataAvailable')

    finish({ users: [item(1, 100)] })
    await flushPromises()
    expect(wrapper.find('[data-loading-skeleton]').exists()).toBe(false)
    expect(wrapper.get('table').attributes('aria-busy')).toBe('false')
    await wrapper.get('tbody tr').trigger('click')
    expect(wrapper.emitted('select-user')).toEqual([[1, 'u1@test.com']])
    wrapper.unmount()
  })

  it.each(['empty', 'error'])('请求以 %s 结束时退出骨架并显示原有空态', async (result) => {
    if (result === 'empty') getUserBreakdown.mockResolvedValue({ users: [] })
    else getUserBreakdown.mockRejectedValue(new Error('request failed'))
    const wrapper = mountRanking()
    await flushPromises()

    expect(wrapper.find('[data-loading-skeleton]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.dashboard.noDataAvailable')
    wrapper.unmount()
  })
})
