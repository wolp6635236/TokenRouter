export type QualityProbeListKind = 'passed' | 'degraded' | 'skipped'

export interface QualityProbeListResult {
  kind: QualityProbeListKind
  at?: string
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asBool(value: unknown): boolean {
  return value === true
}

function asText(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function latestHistory(state: Record<string, unknown>): Record<string, unknown> | null {
  const history = state.history
  if (!Array.isArray(history) || history.length === 0) {
    return null
  }
  return asRecord(history[0])
}

// qualityProbeListResult 从提供商 Extra 读最近一轮探测，供列表徽章使用。
export function qualityProbeListResult(extra: unknown): QualityProbeListResult | null {
  const root = asRecord(extra)
  if (!root) {
    return null
  }
  const state = asRecord(root.quality_probe)
  if (!state) {
    return null
  }
  const latest = latestHistory(state)
  if (latest) {
    if (asBool(latest.skipped)) {
      return { kind: 'skipped', at: asText(latest.at) || asText(state.updated_at) || undefined }
    }
    if (asBool(latest.degraded)) {
      return { kind: 'degraded', at: asText(latest.at) || asText(state.updated_at) || undefined }
    }
    return { kind: 'passed', at: asText(latest.at) || asText(state.updated_at) || undefined }
  }
  const updatedAt = asText(state.updated_at)
  if (!updatedAt && !asText(state.last_model)) {
    return null
  }
  if (!asBool(state.last_candy_ok) && !asBool(state.last_trace_ok)) {
    return { kind: 'degraded', at: updatedAt || undefined }
  }
  return { kind: 'passed', at: updatedAt || undefined }
}
