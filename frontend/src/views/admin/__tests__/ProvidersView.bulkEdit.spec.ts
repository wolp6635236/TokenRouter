import { useProtocolCatalogFixture } from '@/__tests__/helpers/protocolCatalog'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import ProvidersView from '../ProvidersView.vue'

const {
  listProviders,
  listWithEtag,
  getBatchTodayStats,
  getById,
  getUsage,
  setPrivacy,
  getAllProxies,
  getAllGroupsIncludingInactive,
  showError,
  showSuccess,
  showInfo,
  showWarning
} = vi.hoisted(() => ({
  listProviders: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getById: vi.fn(),
  getUsage: vi.fn(),
  setPrivacy: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroupsIncludingInactive: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    providers: {
      list: listProviders,
      listWithEtag,
      getBatchTodayStats,
      getById,
      getUsage,
      setPrivacy,
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
    showError,
    showSuccess,
    showInfo,
    showWarning
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

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div data-test="data-table">
      <span v-for="column in columns" :key="column.key" data-test="column-key">{{ column.key }}</span>
      <div v-for="row in data" :key="row.id">
        <div data-test="select-row"><slot name="cell-select" :row="row" /></div>
        <slot name="cell-created_at" :value="row.created_at" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `
}

const ProviderBulkActionsBarStub = {
  props: ['selectedIds', 'usageLoading'],
  emits: ['query-usage'],
  template: `
    <div>
      <button data-test="query-usage" :disabled="usageLoading" @click="$emit('query-usage')">query usage</button>
    </div>
  `
}

// 「批量编辑筛选结果」位于工具菜单中，菜单通过 Teleport 挂载到 body。
// 前面用例未卸载的页面可能在 body 里留下菜单，取最后挂载的那一个。
const openFilteredBulkEditFromMenu = async (wrapper: VueWrapper) => {
  const toolsButton = wrapper.findAll('button').find((button) => button.attributes('title') === 'admin.providers.moreActions')
  expect(toolsButton).toBeDefined()
  await toolsButton!.trigger('click')
  await flushPromises()
  const menuItems = document.body.querySelectorAll<HTMLButtonElement>('[data-test="bulk-edit-filtered"]')
  expect(menuItems.length).toBeGreaterThan(0)
  menuItems[menuItems.length - 1].click()
}

const BulkEditProviderModalStub = {
  props: ['show', 'target'],
  template: '<div data-test="bulk-edit-modal" :data-show="String(show)" :data-target-mode="target?.mode ?? \'\'" :data-preview-count="String(target?.previewCount ?? \'\')" :data-platforms="(target?.selectedPlatforms ?? []).join(\',\')" :data-types="(target?.selectedTypes ?? []).join(\',\')"></div>'
}

const ProviderActionMenuStub = {
  props: ['show', 'provider'],
  emits: ['set-privacy'],
  template: '<button v-if="show" data-test="set-privacy" @click="$emit(\'set-privacy\', provider)">set privacy</button>'
}

describe('admin ProvidersView bulk edit scope', () => {
  beforeEach(() => {
    localStorage.clear()

    listProviders.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getById.mockReset()
    getUsage.mockReset()
    setPrivacy.mockReset()
    getAllProxies.mockReset()
    getAllGroupsIncludingInactive.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()

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
    getById.mockRejectedValue(new Error('unexpected getById call'))
    getUsage.mockResolvedValue({ updated_at: null, five_hour: null, seven_day: null, seven_day_sonnet: null })
    setPrivacy.mockRejectedValue(new Error('unexpected setPrivacy call'))
    getAllProxies.mockResolvedValue([])
    getAllGroupsIncludingInactive.mockResolvedValue([])
  })

  it('opens bulk edit in filtered-results mode from the bulk actions dropdown', async () => {
    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          ProviderTableFilters: { template: '<div></div>' },
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()
    await openFilteredBulkEditFromMenu(wrapper)
    await flushPromises()

    expect(wrapper.get('[data-test="bulk-edit-modal"]').attributes('data-show')).toBe('true')
    expect(wrapper.get('[data-test="bulk-edit-modal"]').attributes('data-target-mode')).toBe('filtered')
  })

  it('loads all filtered pages for bulk edit metadata using lite requests', async () => {
    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          ProviderTableFilters: { template: '<div></div>' },
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()
    listProviders.mockClear()
    listProviders
      .mockResolvedValueOnce({
        items: [
          { id: 1, platform: 'openai', type: 'apikey' },
          { id: 2, platform: 'anthropic', type: 'oauth' }
        ],
        total: 3,
        page: 1,
        page_size: 500,
        pages: 2
      })
      .mockResolvedValueOnce({
        items: [
          { id: 3, platform: 'openai', type: 'service_account' }
        ],
        total: 3,
        page: 2,
        page_size: 500,
        pages: 2
      })

    await openFilteredBulkEditFromMenu(wrapper)
    await flushPromises()

    expect(listProviders).toHaveBeenNthCalledWith(1, 1, 500, expect.objectContaining({ lite: '1' }))
    expect(listProviders).toHaveBeenNthCalledWith(2, 2, 500, expect.objectContaining({ lite: '1' }))
    const modal = wrapper.get('[data-test="bulk-edit-modal"]')
    expect(modal.attributes('data-preview-count')).toBe('3')
    expect(modal.attributes('data-platforms')).toBe('openai,anthropic')
    expect(modal.attributes('data-types')).toBe('apikey,oauth,service_account')
  })

  it('renders the created_at column by default', async () => {
    listProviders.mockResolvedValue({
      items: [
        {
          id: 1,
          name: 'test-provider',
          platform: 'anthropic',
          type: 'oauth',
          status: 'active',
          schedulable: true,
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          ProviderTableFilters: { template: '<div></div>' },
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()

    const columnKeys = wrapper.findAll('[data-test="column-key"]').map(node => node.text())
    expect(columnKeys).toContain('created_at')
    const columns = wrapper.getComponent(DataTableStub).props('columns') as Array<{ key: string; label: string; sortable: boolean }>
    expect(columns.find(column => column.key === 'created_at')).toMatchObject({
      label: 'admin.providers.columns.createdAt',
      sortable: true
    })
  })

  it('shows privacy result based on the returned provider privacy mode', async () => {
    listProviders.mockResolvedValue({
      items: [
        {
          id: 1,
          name: 'openai-oauth',
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          schedulable: true,
          extra: {},
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    setPrivacy
      .mockResolvedValueOnce({
        id: 1,
        name: 'openai-oauth',
        platform: 'openai',
        type: 'oauth',
        status: 'active',
        schedulable: true,
        extra: { privacy_mode: 'training_off' },
        created_at: '2026-03-07T10:00:00Z',
        updated_at: '2026-03-07T10:00:00Z'
      })
      .mockResolvedValueOnce({
        id: 1,
        name: 'openai-oauth',
        platform: 'openai',
        type: 'oauth',
        status: 'active',
        schedulable: true,
        extra: { privacy_mode: 'training_set_cf_blocked' },
        created_at: '2026-03-07T10:00:00Z',
        updated_at: '2026-03-07T10:00:00Z'
      })

    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          ProviderTableFilters: { template: '<div></div>' },
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
          ProviderActionMenu: ProviderActionMenuStub,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()
    const moreButton = wrapper.findAll('button').find(button => button.text().includes('common.more'))
    expect(moreButton).toBeTruthy()
    await moreButton!.trigger('click')
    await wrapper.get('[data-test="set-privacy"]').trigger('click')
    await flushPromises()

    expect(setPrivacy).toHaveBeenCalledWith(1)
    expect(showSuccess).toHaveBeenCalledWith('admin.providers.privacyTrainingOff')
    expect(showError).not.toHaveBeenCalled()

    await moreButton!.trigger('click')
    await wrapper.get('[data-test="set-privacy"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.providers.privacyCfBlocked')
  })

  it('bulk queries usage for selected supported providers', async () => {
    listProviders.mockResolvedValue({
      items: [
        {
          id: 1,
          name: 'openai-oauth',
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          schedulable: true,
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        },
        {
          id: 2,
          name: 'plain-key',
          platform: 'openai',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        },
        {
          id: 3,
          name: 'claude-oauth',
          platform: 'anthropic',
          type: 'oauth',
          status: 'active',
          schedulable: true,
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        },
        {
          id: 4,
          name: 'qoder-cosy',
          platform: 'qoder',
          type: 'cosy',
          status: 'active',
          schedulable: true,
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        }
      ],
      total: 4,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          ProviderTableFilters: { template: '<div></div>' },
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(4)
    for (const checkbox of checkboxes) {
      await checkbox.setValue(true)
    }

    await wrapper.get('[data-test="query-usage"]').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(getUsage).toHaveBeenCalledTimes(3)
    expect(getUsage).toHaveBeenCalledWith(1, 'active', true)
    expect(getUsage).toHaveBeenCalledWith(3, 'active', true)
    expect(getUsage).toHaveBeenCalledWith(4, 'active', true)
    expect(getUsage).not.toHaveBeenCalledWith(2, 'active', true)
    expect(showSuccess).toHaveBeenCalledWith('admin.providers.bulkActions.queryUsageSuccess')
  })

  it('bulk queries usage for selected providers outside the current page', async () => {
    listProviders.mockResolvedValue({
      items: [
        {
          id: 1,
          name: 'openai-oauth',
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          schedulable: true,
          created_at: '2026-03-07T10:00:00Z',
          updated_at: '2026-03-07T10:00:00Z'
        }
      ],
      total: 2,
      page: 1,
      page_size: 1,
      pages: 2
    })
    getById.mockResolvedValue({
      id: 99,
      name: 'page-two-claude',
      platform: 'anthropic',
      type: 'oauth',
      status: 'active',
      schedulable: true,
      created_at: '2026-03-07T10:00:00Z',
      updated_at: '2026-03-07T10:00:00Z'
    })

    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          ProviderTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          ProviderTableFilters: { template: '<div></div>' },
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()

    const checkbox = wrapper.get('input[type="checkbox"]')
    await checkbox.setValue(true)
    const vm = wrapper.vm as unknown as { setSelectedIds: (ids: number[]) => void }
    vm.setSelectedIds([1, 99])
    await flushPromises()

    await wrapper.get('[data-test="query-usage"]').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(getById).toHaveBeenCalledWith(99)
    expect(getUsage).toHaveBeenCalledTimes(2)
    expect(getUsage).toHaveBeenCalledWith(1, 'active', true)
    expect(getUsage).toHaveBeenCalledWith(99, 'active', true)
  })

  it('falls back to name sorting for the removed upstream billing sort key', async () => {
    localStorage.setItem('provider-table-sort', JSON.stringify({ key: 'upstream_billing_rate', order: 'asc' }))

    const wrapper = mount(ProvidersView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="table" /></div>' },
          DataTable: DataTableStub,
          ProviderBulkActionsBar: ProviderBulkActionsBarStub,
          ProviderTableActions: true,
          ProviderTableFilters: true,
          ProviderActionMenu: true,
          Pagination: true,
          ConfirmDialog: true,
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
          BulkEditProviderModal: BulkEditProviderModalStub,
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

    await flushPromises()

    expect(listProviders.mock.calls[0]?.[2]).toEqual(expect.objectContaining({
      sort_by: 'name',
      sort_order: 'asc'
    }))
    expect(wrapper.find('[data-test="probe-upstream-billing"]').exists()).toBe(false)
  })
})

useProtocolCatalogFixture()
