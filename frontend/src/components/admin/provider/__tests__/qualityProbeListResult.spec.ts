import { describe, expect, it } from 'vitest'
import { qualityProbeListResult } from '../qualityProbeListResult'

describe('qualityProbeListResult', () => {
  it('没有探测 Extra 时不展示', () => {
    expect(qualityProbeListResult(undefined)).toBeNull()
    expect(qualityProbeListResult({})).toBeNull()
    expect(qualityProbeListResult({ quality_probe: {} })).toBeNull()
  })

  it('优先用 history 第一条：跳过、降智、通过', () => {
    expect(
      qualityProbeListResult({
        quality_probe: {
          last_candy_ok: true,
          last_trace_ok: true,
          history: [{ skipped: true, at: '2026-10-04T21:00:00+08:00' }]
        }
      })
    ).toEqual({ kind: 'skipped', at: '2026-10-04T21:00:00+08:00' })
    expect(
      qualityProbeListResult({
        quality_probe: {
          history: [{ skipped: false, degraded: true, at: '2026-10-04T21:01:00+08:00' }]
        }
      })
    ).toEqual({ kind: 'degraded', at: '2026-10-04T21:01:00+08:00' })
    expect(
      qualityProbeListResult({
        quality_probe: {
          history: [{ skipped: false, degraded: false, at: '2026-10-04T21:02:00+08:00' }]
        }
      })
    ).toEqual({ kind: 'passed', at: '2026-10-04T21:02:00+08:00' })
  })

  it('没有 history 时用 last_candy_ok 和 last_trace_ok', () => {
    expect(
      qualityProbeListResult({
        quality_probe: {
          updated_at: '2026-10-04T19:00:00+08:00',
          last_candy_ok: true,
          last_trace_ok: false
        }
      })
    ).toEqual({ kind: 'passed', at: '2026-10-04T19:00:00+08:00' })
    expect(
      qualityProbeListResult({
        quality_probe: {
          last_model: 'gpt-6-astra',
          last_candy_ok: false,
          last_trace_ok: false
        }
      })
    ).toEqual({ kind: 'degraded', at: undefined })
  })
})
