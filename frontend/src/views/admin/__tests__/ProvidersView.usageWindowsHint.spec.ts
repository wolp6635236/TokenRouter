import { useProtocolCatalogFixture } from '@/__tests__/helpers/protocolCatalog'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ProvidersView from '../ProvidersView.vue'

const {
  listProviders,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroupsIncludingInactive
} = vi.hoisted(() => ({
  listProviders: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroupsIncludingInactive: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    providers: {
      list: listProviders,
      listWithEtag,
      getBatchTodayStats,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAllIncludingInactive: getAllGroupsIncludingInactive
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
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

// 渲染每列的表头插槽，方便断言用量窗口表头提示。
const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <div data-test="data-table">
      <template v-for="column in columns" :key="column.key">
        <div v-if="column.key === 'usage'" data-test="usage-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
      </template>
    </div>
  `
}

// HelpTooltip 替身直接渲染传入内容。
const HelpTooltipStub = {
  props: ['content', 'widthClass'],
  template: '<span data-test="usage-windows-hint">{{ content }}</span>'
}

function mountView() {
  return mount(ProvidersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: HelpTooltipStub,
        Pagination: true,
        ConfirmDialog: true,
        ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        ProviderTableFilters: {
          props: ['groups'],
          template: '<div data-test="provider-filters" :data-group-count="groups.length"></div>'
        },
        ProviderBulkActionsBar: true,
        ProviderActionMenu: true,
        ImportDataModal: true,
        ReAuthProviderModal: true,
        ProviderTestModal: true,
        QualityProbeResultModal: true,
        ProviderStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateProviderModal: true,
        EditProviderModal: true,
        BulkEditProviderModal: true,
        PlatformTypeBadge: true,
        ProviderCapacityCell: true,
        ProviderStatusIndicator: true,
        ProviderTodayStatsCell: true,
        ProviderGroupsCell: true,
        ProviderUsageCell: true,
        Icon: true
      }
    }
  })
}

describe('admin ProvidersView usage windows hint', () => {
  beforeEach(() => {
    localStorage.clear()

    listProviders.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getAllProxies.mockReset()
    getAllGroupsIncludingInactive.mockReset()

    listProviders.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
    listWithEtag.mockResolvedValue({
      notModified: true,
      etag: null,
      data: null
    })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroupsIncludingInactive.mockResolvedValue([])
  })

  it('keeps groups available when loading proxies fails', async () => {
    getAllProxies.mockRejectedValue(new Error('proxy service unavailable'))
    getAllGroupsIncludingInactive.mockResolvedValue([{ id: 7, name: 'production' }])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="provider-filters"]').attributes('data-group-count')).toBe('1')
  })

  it('renders an explanatory tooltip next to the usage windows column header', async () => {
    const wrapper = mountView()
    await flushPromises()

    const header = wrapper.find('[data-test="usage-header"]')
    expect(header.exists()).toBe(true)
    // 列标题应与帮助图标一起展示。
    expect(header.text()).toContain('admin.providers.columns.usageWindows')

    const hint = wrapper.find('[data-test="usage-windows-hint"]')
    expect(hint.exists()).toBe(true)
    expect(hint.text()).toBe('admin.providers.usageWindowsHint')
    expect(getAllGroupsIncludingInactive).toHaveBeenCalledTimes(1)
  })

  it('keeps Ollama Cloud in the single usage column and ignores legacy column preferences', async () => {
    localStorage.setItem('provider-hidden-columns', JSON.stringify(['ollama_cloud_usage']))
    const wrapper = mountView()
    await flushPromises()

    const columns = wrapper.getComponent(DataTableStub).props('columns') as Array<{ key: string }>
    expect(columns.filter(column => column.key === 'usage')).toHaveLength(1)
    expect(columns.some(column => column.key === 'ollama_cloud_usage')).toBe(false)
  })

  it('does not expose the removed upstream billing rate column', async () => {
    const wrapper = mountView()
    await flushPromises()

    const columns = wrapper.getComponent(DataTableStub).props('columns') as Array<{ key: string; sortable: boolean }>
    expect(columns.some(column => column.key === 'upstream_billing_rate')).toBe(false)
  })
})

useProtocolCatalogFixture()
