export const PROVIDERS_COLUMN_ORDER_KEY = 'admin-providers-column-order'
export const PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY = 'admin-providers-quality-probe-after-groups'

// 把降智列插到分组列后面，方便对照账号所属分组看探测结果。
export function placeQualityProbeAfterGroups(order: string[]): string[] {
  const groupsIdx = order.indexOf('groups')
  if (groupsIdx < 0) {
    return order
  }
  const next = order.filter(key => key !== 'quality_probe')
  next.splice(next.indexOf('groups') + 1, 0, 'quality_probe')
  return next
}

// 浏览器里已经保存过列顺序时，降智列原先追加在数据列末尾；只迁移一次。
export function migrateQualityProbeColumnOrder(storage: Storage): void {
  if (storage.getItem(PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY) === '1') {
    return
  }
  let order: string[] = []
  try {
    const raw = storage.getItem(PROVIDERS_COLUMN_ORDER_KEY)
    if (raw) {
      const parsed: unknown = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        order = parsed.filter((value): value is string => typeof value === 'string')
      }
    }
  } catch {
    storage.setItem(PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY, '1')
    return
  }
  if (order.length > 0) {
    storage.setItem(PROVIDERS_COLUMN_ORDER_KEY, JSON.stringify(placeQualityProbeAfterGroups(order)))
  }
  storage.setItem(PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY, '1')
}
