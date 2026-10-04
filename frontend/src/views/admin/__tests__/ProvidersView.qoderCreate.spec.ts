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
    showInfo: vi.fn(),
    showWarning: vi.fn()
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

const ProviderTableActionsStub = {
  emits: ['create', 'refresh'],
  template: `
    <div>
      <slot name="after" />
      <button data-test="default-create" type="button" @click="$emit('create')">create</button>
      <slot name="afterCreate" />
    </div>
  `
}

const CreateProviderModalStub = {
  props: ['show', 'initialPlatform'],
  template: `
    <div
      data-test="create-provider-modal"
      :data-show="String(show)"
      :data-initial-platform="initialPlatform || ''"
    />
  `
}

function mountView() {
  return mount(ProvidersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: { template: '<div />' },
        HelpTooltip: true,
        Pagination: true,
        ConfirmDialog: true,
        ProviderTableActions: ProviderTableActionsStub,
        ProviderTableFilters: { template: '<div />' },
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
        TLSFingerprintRoutersModal: true,
        CreateProviderModal: CreateProviderModalStub,
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

describe('admin ProvidersView Qoder create entry', () => {
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

  it('uses the standard create entry and does not render the Qoder shortcut', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-test="create-qoder-provider"]').exists()).toBe(false)

    await wrapper.get('[data-test="default-create"]').trigger('click')
    await flushPromises()

    const modal = wrapper.get('[data-test="create-provider-modal"]')
    expect(modal.attributes('data-show')).toBe('true')
    expect(modal.attributes('data-initial-platform')).toBe('')
  })
})

useProtocolCatalogFixture()
