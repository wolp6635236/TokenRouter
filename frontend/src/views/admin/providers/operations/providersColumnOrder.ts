// providersColumnOrder.ts migrates saved provider-table column preferences after adding the quality-probe badge. Existing orders place that badge after groups so administrators can compare scope and latest probe outcome.
export const PROVIDERS_COLUMN_ORDER_KEY = 'admin-providers-column-order'
export const PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY =
  'admin-providers-quality-probe-after-groups'

// placeQualityProbeAfterGroups puts the quality-probe column after groups for provider scope comparison.
export function placeQualityProbeAfterGroups(order: string[]): string[] {
  const groupsIdx = order.indexOf('groups')
  if (groupsIdx < 0) {
    return order
  }
  const next = order.filter((key) => key !== 'quality_probe')
  next.splice(next.indexOf('groups') + 1, 0, 'quality_probe')
  return next
}

// migrateQualityProbeColumnOrder updates a saved column order once and records completion in browser storage.
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
