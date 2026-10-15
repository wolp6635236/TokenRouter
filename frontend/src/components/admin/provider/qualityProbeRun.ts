import type { QualityProbeReport } from '@/api/admin/providers'

export interface QualityProbeRun {
  model: string
  phase: 'running' | 'done' | 'error'
  report: QualityProbeReport | null
  error: string
}
