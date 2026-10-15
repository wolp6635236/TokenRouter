export const QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY = 'admin-quality-probe-logs-column-order'
export const QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY = 'admin-quality-probe-logs-provider-first'
export const QUALITY_PROBE_LOGS_HIDDEN_COLUMNS_KEY = 'admin-quality-probe-logs-hidden-columns'

export function placeProviderFirst(order: string[]): string[] {
  return ['provider_name', ...order.filter(key => key !== 'provider_name')]
}

// 浏览器里保存过列顺序时，提供商列原先跟在时间后面。只迁移一次。
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
          storage.setItem(QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY, JSON.stringify(placeProviderFirst(order)))
        }
      }
    }
  } catch {
    // 顺序记录无法解析时保持原样，标记写上后不再重试。
  }
  storage.setItem(QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY, '1')
}
