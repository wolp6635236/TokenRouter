import type { QualityProbeReport } from '@/api/admin/providers'

// qualityProbeRun.ts describes client state for one selected manual quality-probe model. A run changes from pending to a server report or request error while other selected models proceed independently.

export interface QualityProbeRun {
  model: string
  phase: 'running' | 'done' | 'error'
  report: QualityProbeReport | null
  error: string
}
