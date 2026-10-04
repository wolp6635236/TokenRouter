import { afterEach, describe, expect, it } from 'vitest'
import {
  PROVIDERS_COLUMN_ORDER_KEY,
  PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY,
  migrateQualityProbeColumnOrder,
  placeQualityProbeAfterGroups
} from '../providersColumnOrder'

describe('placeQualityProbeAfterGroups', () => {
  it('把 quality_probe 插到 groups 后面', () => {
    expect(placeQualityProbeAfterGroups([
      'name',
      'groups',
      'usage',
      'notes',
      'quality_probe'
    ])).toEqual([
      'name',
      'groups',
      'quality_probe',
      'usage',
      'notes'
    ])
  })

  it('已在分组后面时保持原样', () => {
    const order = ['groups', 'quality_probe', 'usage']
    expect(placeQualityProbeAfterGroups(order)).toEqual(order)
  })

  it('没有 groups 时不动', () => {
    const order = ['name', 'quality_probe', 'usage']
    expect(placeQualityProbeAfterGroups(order)).toEqual(order)
  })
})

describe('migrateQualityProbeColumnOrder', () => {
  afterEach(() => {
    localStorage.clear()
  })

  it('空存储只打迁移标记，让默认列顺序生效', () => {
    migrateQualityProbeColumnOrder(localStorage)
    expect(localStorage.getItem(PROVIDERS_COLUMN_ORDER_KEY)).toBeNull()
    expect(localStorage.getItem(PROVIDERS_QUALITY_PROBE_COLUMN_MIGRATION_KEY)).toBe('1')
  })

  it('已保存的顺序只改一次', () => {
    localStorage.setItem(PROVIDERS_COLUMN_ORDER_KEY, JSON.stringify([
      'groups',
      'usage',
      'quality_probe'
    ]))
    migrateQualityProbeColumnOrder(localStorage)
    expect(JSON.parse(localStorage.getItem(PROVIDERS_COLUMN_ORDER_KEY) || '[]')).toEqual([
      'groups',
      'quality_probe',
      'usage'
    ])
    localStorage.setItem(PROVIDERS_COLUMN_ORDER_KEY, JSON.stringify(['usage', 'groups', 'quality_probe']))
    migrateQualityProbeColumnOrder(localStorage)
    expect(JSON.parse(localStorage.getItem(PROVIDERS_COLUMN_ORDER_KEY) || '[]')).toEqual([
      'usage',
      'groups',
      'quality_probe'
    ])
  })
})
