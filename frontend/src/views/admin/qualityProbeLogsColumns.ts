// qualityProbeLogsColumns.ts migrates persisted quality-probe log column preferences. The provider column moves ahead of time once so existing administrators retain their selected optional columns.
export const QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY = 'admin-quality-probe-logs-column-order'
export const QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY = 'admin-quality-probe-logs-provider-first'
export const QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY = 'admin-quality-probe-logs-hidden-columns'

export function placeProviderFirst(order: string[]): string[] {
  return ['provider_name', ...order.filter((key) => key !== 'provider_name')]
}

// migrateQualityProbeLogsProviderFirst moves the provider column ahead of time in one stored order migration.
export function migrateQualityProbeLogsProviderFirst(storage: Storage): void {
  if (storage.getItem(QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY) === '1') {
    return
  }
  try {
    const raw = storage.getItem(QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY)
    if (raw) {
      const parsed: unknown = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        const order = parsed.filter((value): value is string => typeof value === 'string')
        if (order.length > 0) {
          storage.setItem(
            QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY,
            JSON.stringify(placeProviderFirst(order)),
          )
        }
      }
    }
  } catch {
    // Invalid stored JSON is left unchanged and marked complete to avoid repeating a failed migration.
  }
  storage.setItem(QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY, '1')
}
