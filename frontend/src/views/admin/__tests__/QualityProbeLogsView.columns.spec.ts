import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QualityProbeLogsView from '../QualityProbeLogsView.vue'
import { QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY } from '@/components/admin/provider/qualityProbeLogsColumns'

const listQualityProbeLogs = vi.fn()

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      listQualityProbeLogs: (...args: unknown[]) => listQualityProbeLogs(...args)
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn() })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const mountView = () =>
  mount(QualityProbeLogsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: {
          props: ['columns'],
          template: '<div data-test="columns">{{ columns.map(column => column.key).join(",") }}</div>'
        },
        Pagination: true,
        BaseDialog: true,
        QualityProbeSamples: true,
        Icon: true
      }
    }
  })

describe('QualityProbeLogsView columns', () => {
  beforeEach(() => {
    localStorage.clear()
    listQualityProbeLogs.mockReset()
    listQualityProbeLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  })

  it('提供商列在最前，列显示可以藏起时间并记住选择', async () => {
    const wrapper = mountView()
    await flushPromises()

    const columnText = () => wrapper.get('[data-test="columns"]').text()
    expect(columnText().startsWith('provider_name,')).toBe(true)
    expect(columnText()).toContain('actions')

    await wrapper.get('button[aria-label="admin.providers.viewColumns"]').trigger('click')
    const timeToggle = wrapper.findAll('button').find(button => button.text().includes('admin.providers.qualityProbeLogs.time'))
    expect(timeToggle).toBeDefined()
    expect(wrapper.text()).not.toContain('admin.providers.qualityProbeLogs.provider')
    await timeToggle!.trigger('click')

    expect(columnText()).not.toContain('at')
    expect(columnText().startsWith('provider_name,')).toBe(true)
    expect(JSON.parse(localStorage.getItem(QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY) || '[]')).toEqual(['at'])
    wrapper.unmount()
  })
})
