import { describe, expect, it } from 'vitest'
import { displayTempUnschedErrorMessage } from '../tempUnschedErrorMessage'

// tempUnschedErrorMessage.spec.ts checks translated, unknown, and absent temporary-unscheduling reasons. The fixture supplies one quality-degraded translation so each fallback stays observable.

describe('displayTempUnschedErrorMessage', () => {
  const labels: Record<string, string> = {
    'admin.providers.tempUnschedulable.reasons.quality_degraded': '降智探测未通过',
  }
  const t = (key: string) => labels[key] ?? key
  const te = (key: string) => key in labels

  it('把 quality_degraded 显示成中文', () => {
    expect(displayTempUnschedErrorMessage('quality_degraded', t, te)).toBe('降智探测未通过')
  })

  it('未知原因保持原文', () => {
    expect(displayTempUnschedErrorMessage('overloaded', t, te)).toBe('overloaded')
  })

  it('空值显示破折号', () => {
    expect(displayTempUnschedErrorMessage('', t, te)).toBe('-')
    expect(displayTempUnschedErrorMessage(null, t, te)).toBe('-')
  })
})
