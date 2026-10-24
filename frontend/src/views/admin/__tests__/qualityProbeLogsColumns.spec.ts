import { afterEach, describe, expect, it } from 'vitest'
import {
  QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY,
  QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY,
  migrateQualityProbeLogsProviderFirst,
  placeProviderFirst,
} from '../qualityProbeLogsColumns'

// qualityProbeLogsColumns.spec.ts checks one-time persisted log-column migration. Browser storage fixtures distinguish a saved order from a first-time visitor without preferences.

describe('placeProviderFirst', () => {
  it('把提供商列移到最前', () => {
    expect(placeProviderFirst(['at', 'provider_name', 'trigger', 'model'])).toEqual([
      'provider_name',
      'at',
      'trigger',
      'model',
    ])
  })

  it('已经在最前时顺序不变', () => {
    expect(placeProviderFirst(['provider_name', 'at'])).toEqual(['provider_name', 'at'])
  })
})

describe('migrateQualityProbeLogsProviderFirst', () => {
  afterEach(() => {
    localStorage.clear()
  })

  it('已保存的列顺序只把提供商提前一次', () => {
    localStorage.setItem(
      QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY,
      JSON.stringify(['at', 'provider_name', 'model']),
    )
    migrateQualityProbeLogsProviderFirst(localStorage)
    expect(JSON.parse(localStorage.getItem(QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY) || '[]')).toEqual([
      'provider_name',
      'at',
      'model',
    ])
    localStorage.setItem(QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY, JSON.stringify(['at', 'model']))
    migrateQualityProbeLogsProviderFirst(localStorage)
    expect(JSON.parse(localStorage.getItem(QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY) || '[]')).toEqual([
      'at',
      'model',
    ])
  })

  it('没有保存过顺序时只写下迁移标记', () => {
    migrateQualityProbeLogsProviderFirst(localStorage)
    expect(localStorage.getItem(QUALITY_PROBE_LOGS_COLUMN_ORDER_KEY)).toBeNull()
    expect(localStorage.getItem(QUALITY_PROBE_LOGS_PROVIDER_FIRST_KEY)).toBe('1')
  })
})
