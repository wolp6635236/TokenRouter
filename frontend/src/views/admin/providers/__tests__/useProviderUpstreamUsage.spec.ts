import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import type {
  Provider,
  UpstreamUsageQueryResult,
  BatchUpstreamUsageResponse,
} from '@/api/admin/providers.types'
import { useProviderUpstreamUsage } from '../useProviderUpstreamUsage'

// useProviderUpstreamUsage.spec.ts exercises OpenCode observations through the
// provider page's request and session-cache controller. API and account mocks
// keep queries local while deferred responses expose configuration changes,
// invalidation, and competing single and batch request ownership.

// Query and feedback spies expose admitted IDs, normalized failures, and cache
// effects while the mutable account fixture permits administrator changes.
const { querySingle, queryBatch, getById, showSuccess, showError, account } = vi.hoisted(() => ({
  querySingle: vi.fn(),
  queryBatch: vi.fn(),
  getById: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  account: { user: { id: 42 } },
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    providers: { queryUpstreamUsage: querySingle, queryBatchUpstreamUsage: queryBatch, getById },
  },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => account }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError, showWarning: vi.fn() }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

// goProvider constructs a persisted Go provider with a stale manual adapter
// preference. Fixed platform resolution must ignore that preference when
// validating responses and deriving the browser cache identity.
function goProvider(id = 1): Provider {
  return {
    id,
    name: `Go ${id}`,
    type: 'apikey',
    platform: 'opencode',
    credentials: { provider_mode: 'go', base_url: 'https://inference.example.test/go' },
    extra: { upstream_usage_query: { adapter: 'new_api', enabled: true } },
    updated_at: '2026-10-26T00:00:00Z',
  } as Provider
}

// goResult supplies the server's three normalized percentage windows. Usage
// above one hundred and its rate-limited status remain observable, while the
// adapter has already clamped the corresponding remainder to zero.
function goResult(id = 1): UpstreamUsageQueryResult {
  return {
    provider_id: id,
    adapter: 'opencode_go',
    provider: 'opencode',
    observed_at: '2026-10-26T01:00:00Z',
    mode: 'limits',
    unit: 'PERCENT',
    limits: [
      {
        name: '5h',
        used: 125,
        limit: 100,
        remaining: 0,
        status: 'rate-limited',
        reset_at: '2026-10-26T05:00:00Z',
      },
      { name: 'weekly', used: 30, limit: 100, remaining: 70, reset_at: '2026-11-01T00:00:00Z' },
      { name: 'monthly', used: 10, limit: 100, remaining: 90, reset_at: '2026-11-26T00:00:00Z' },
    ],
  }
}

// deferred exposes a response resolver so tests control request completion
// order. Each test resolves its promise after the competing state change and
// awaits the controller work before checking publication.
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((complete) => {
    resolve = complete
  })
  return { promise, resolve }
}

describe('useProviderUpstreamUsage OpenCode Go', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.resetAllMocks()
    account.user = { id: 42 }
    querySingle.mockResolvedValue(goResult())
    queryBatch.mockResolvedValue({ usage: { '1': goResult() }, errors: {} })
  })

  // A Go request returns all three windows including an over-limit observation.
  // A fresh controller restores the same data and force refresh remains a manual action.
  it('accepts single Go results and restores their five-minute session cache', async () => {
    const rows = ref([goProvider()])
    const first = useProviderUpstreamUsage(rows, ref([]))
    first.hydrateUpstreamUsageCache()
    expect(querySingle).not.toHaveBeenCalled()
    await first.requestUpstreamUsage(rows.value[0])
    expect(first.upstreamUsageByProviderId.value['1']).toEqual(goResult())
    expect(first.upstreamUsageErrorByProviderId.value['1']).toBeNull()
    expect(sessionStorage.key(0)).toContain('opencode_go')
    const second = useProviderUpstreamUsage(rows, ref([]))
    second.hydrateUpstreamUsageCache()
    expect(second.upstreamUsageByProviderId.value['1']).toEqual(goResult())
    await second.requestUpstreamUsage(rows.value[0])
    expect(querySingle).toHaveBeenCalledTimes(1)
    await second.requestUpstreamUsage(rows.value[0], { force: true })
    expect(querySingle).toHaveBeenCalledTimes(2)
    const key = second.upstreamUsageCacheKey(rows.value[0])
    // Expiry is measured from storage time rather than vendor observation time;
    // reaching the five-minute lifetime removes the entry without starting a query.
    sessionStorage.setItem(key, JSON.stringify({ data: goResult(), ts: Date.now() - 300_000 }))
    second.hydrateUpstreamUsageCache()
    expect(second.upstreamUsageByProviderId.value['1']).toBeNull()
    expect(sessionStorage.length).toBe(0)
    expect(querySingle).toHaveBeenCalledTimes(2)
  })

  // The selection combines Go, disabled Go, payg, and an off-page Go provider.
  // Only enabled Go IDs are queried, with independent success and rate-limit outcomes.
  it('accepts batch Go observations and skips disabled and payg providers', async () => {
    const disabled = goProvider(2)
    disabled.extra = { upstream_usage_query: { enabled: false } }
    const payg = goProvider(3)
    payg.credentials.provider_mode = 'payg'
    const rows = ref([goProvider(), disabled, payg])
    getById.mockResolvedValue(goProvider(4))
    queryBatch.mockResolvedValue({
      usage: { '1': goResult() },
      errors: { '4': { code: 'UPSTREAM_USAGE_RATE_LIMITED', status: 429 } },
    })
    const controller = useProviderUpstreamUsage(rows, ref([1, 2, 3, 4]))
    await controller.handleBulkQueryUpstreamUsage()
    expect(queryBatch).toHaveBeenCalledWith([1, 4])
    expect(controller.upstreamUsageByProviderId.value['1']).toEqual(goResult())
    expect(controller.upstreamUsageErrorByProviderId.value['4']?.status).toBe(429)
    expect(controller.upstreamUsageLoadingByProviderId.value['4']).toBe(false)
    expect(sessionStorage.length).toBe(1)
    expect(showError).toHaveBeenCalledWith('admin.providers.upstreamUsage.partialSuccess')
    await controller.requestUpstreamUsage(disabled)
    await controller.requestUpstreamUsage(payg)
    expect(querySingle).not.toHaveBeenCalled()
  })

  // Wrong provider IDs and relay adapters must fail both public request paths.
  // The same corrupt observation is rejected when injected into the matching cache key.
  it.each([{ provider_id: 9 }, { adapter: 'sub2api' }])(
    'rejects mismatched result identity %j for single, batch, and cache',
    async (override) => {
      const rows = ref([goProvider()])
      const invalid = { ...goResult(), ...override }
      querySingle.mockResolvedValue(invalid)
      queryBatch.mockResolvedValue({ usage: { '1': invalid }, errors: {} })
      const controller = useProviderUpstreamUsage(rows, ref([1]))
      await controller.requestUpstreamUsage(rows.value[0])
      expect(controller.upstreamUsageErrorByProviderId.value['1']?.code).toBe(
        'UPSTREAM_USAGE_INVALID_RESPONSE',
      )
      await controller.handleBulkQueryUpstreamUsage()
      expect(controller.upstreamUsageErrorByProviderId.value['1']?.code).toBe(
        'UPSTREAM_USAGE_INVALID_RESPONSE',
      )
      expect(sessionStorage.length).toBe(0)
      sessionStorage.setItem(
        controller.upstreamUsageCacheKey(rows.value[0]),
        JSON.stringify({ data: invalid, ts: Date.now() }),
      )
      controller.hydrateUpstreamUsageCache()
      expect(controller.upstreamUsageByProviderId.value['1']).toBeNull()
      expect(sessionStorage.length).toBe(0)
    },
  )

  // Fresh browser snapshots belong to one provider revision and administrator.
  // Changing either identity or the management address makes hydration miss without an automatic query.
  it.each(['revision', 'endpoint', 'admin'])(
    'does not restore a cached Go result after %s changes',
    async (change) => {
      const rows = ref([goProvider()])
      const controller = useProviderUpstreamUsage(rows, ref([]))
      controller.hydrateUpstreamUsageCache()
      await controller.requestUpstreamUsage(rows.value[0])
      const oldKey = controller.upstreamUsageCacheKey(rows.value[0])
      if (change === 'revision') rows.value[0].updated_at = '2026-10-26T02:00:00Z'
      if (change === 'endpoint')
        rows.value[0].extra = {
          upstream_usage_query: { enabled: true, base_url: 'https://management.example.test' },
        }
      if (change === 'admin') account.user = { id: 43 }
      expect(controller.upstreamUsageCacheKey(rows.value[0])).not.toBe(oldKey)
      controller.hydrateUpstreamUsageCache()
      expect(controller.upstreamUsageByProviderId.value['1']).toBeNull()
      expect(querySingle).toHaveBeenCalledTimes(1)
    },
  )

  // A single Go query fails with the service's rate-limit code. The controller
  // exposes a retryable row error, clears loading, and stores no successful observation.
  it('keeps single-query rate limits out of the session cache', async () => {
    querySingle.mockRejectedValue({ reason: 'UPSTREAM_USAGE_RATE_LIMITED', status: 429 })
    const rows = ref([goProvider()])
    const controller = useProviderUpstreamUsage(rows, ref([]))
    await controller.requestUpstreamUsage(rows.value[0])
    expect(controller.upstreamUsageErrorByProviderId.value['1']).toMatchObject({
      code: 'UPSTREAM_USAGE_RATE_LIMITED',
      status: 429,
    })
    expect(controller.upstreamUsageLoadingByProviderId.value['1']).toBe(false)
    expect(sessionStorage.length).toBe(0)
  })

  // A pending request belongs to the configuration captured at admission.
  // Replacing its provider revision, mode, endpoint, or administrator prevents late publication.
  it.each(['revision', 'mode', 'endpoint', 'admin'])(
    'discards a single response after an identity change: %s',
    async (change) => {
      const pending = deferred<UpstreamUsageQueryResult>()
      querySingle.mockReturnValue(pending.promise)
      const rows = ref([goProvider()])
      const controller = useProviderUpstreamUsage(rows, ref([]))
      const request = controller.requestUpstreamUsage(rows.value[0])
      const replacement = goProvider()
      if (change === 'revision') replacement.updated_at = '2026-10-26T02:00:00Z'
      if (change === 'mode') replacement.credentials.provider_mode = 'payg'
      if (change === 'endpoint')
        replacement.extra = {
          upstream_usage_query: { enabled: true, base_url: 'https://management.example.test' },
        }
      if (change === 'admin') account.user = { id: 43 }
      rows.value = [replacement]
      pending.resolve(goResult())
      await request
      expect(controller.upstreamUsageByProviderId.value['1']).toBeNull()
      expect(controller.upstreamUsageLoadingByProviderId.value['1']).toBe(false)
      expect(sessionStorage.length).toBe(0)
    },
  )

  // A previously cached Go observation exists before the provider becomes payg
  // or disabled. Hydration retires its display and storage instead of keeping stale windows.
  it.each(['payg', 'disabled'])('invalidates cached Go usage after %s changes', async (change) => {
    const rows = ref([goProvider()])
    const controller = useProviderUpstreamUsage(rows, ref([]))
    controller.hydrateUpstreamUsageCache()
    await controller.requestUpstreamUsage(rows.value[0])
    if (change === 'payg') rows.value[0].credentials.provider_mode = 'payg'
    else rows.value[0].extra = { upstream_usage_query: { enabled: false } }
    controller.hydrateUpstreamUsageCache()
    expect(controller.upstreamUsageByProviderId.value['1']).toBeNull()
    expect(sessionStorage.length).toBe(0)
    await controller.requestUpstreamUsage(rows.value[0])
    expect(querySingle).toHaveBeenCalledTimes(1)
  })

  // A page lists payg providers that were never queried in this session. Hydration must
  // skip invalidation for them, leaving session storage unscanned on each list refresh.
  it('skips invalidation for ineligible providers without in-memory state', () => {
    const payg = goProvider()
    payg.credentials.provider_mode = 'payg'
    const controller = useProviderUpstreamUsage(ref([payg]), ref([]))
    sessionStorage.setItem('unrelated', 'kept')
    const keySpy = vi.spyOn(Storage.prototype, 'key')
    controller.hydrateUpstreamUsageCache()
    expect(keySpy).not.toHaveBeenCalled()
    expect(controller.upstreamUsageByProviderId.value['1']).toBeUndefined()
    keySpy.mockRestore()
    sessionStorage.removeItem('unrelated')
  })

  // A Go batch remains pending when its provider is edited or invalidated.
  // Completing that batch cannot reintroduce its old result or leave the row loading.
  it.each(['mode', 'invalidate'])('discards batch responses after %s changes', async (change) => {
    const pending = deferred<BatchUpstreamUsageResponse>()
    queryBatch.mockReturnValue(pending.promise)
    const rows = ref([goProvider()])
    const controller = useProviderUpstreamUsage(rows, ref([1]))
    const request = controller.handleBulkQueryUpstreamUsage()
    await flushPromises()
    expect(queryBatch).toHaveBeenCalledTimes(1)
    if (change === 'mode') rows.value[0].credentials.provider_mode = 'payg'
    else controller.invalidateUpstreamUsageCache(1)
    pending.resolve({ usage: { '1': goResult() }, errors: {} })
    await request
    expect(controller.upstreamUsageByProviderId.value['1']).toBeNull()
    expect(controller.upstreamUsageLoadingByProviderId.value['1']).toBe(false)
    expect(sessionStorage.length).toBe(0)
  })

  // A single request supersedes an already-running batch for the same provider.
  // The older batch must retain the newer observation in both memory and session storage.
  it('shares request ownership between batch and single Go queries', async () => {
    const pending = deferred<BatchUpstreamUsageResponse>()
    queryBatch.mockReturnValue(pending.promise)
    const rows = ref([goProvider()])
    const controller = useProviderUpstreamUsage(rows, ref([1]))
    const batch = controller.handleBulkQueryUpstreamUsage()
    await flushPromises()
    const newer = { ...goResult(), observed_at: '2026-10-26T02:00:00Z' }
    querySingle.mockResolvedValue(newer)
    await controller.requestUpstreamUsage(rows.value[0], { force: true })
    pending.resolve({ usage: { '1': goResult() }, errors: {} })
    await batch
    expect(controller.upstreamUsageByProviderId.value['1']).toEqual(newer)
    expect(
      JSON.parse(sessionStorage.getItem(controller.upstreamUsageCacheKey(rows.value[0]))!).data,
    ).toEqual(newer)
  })
})
