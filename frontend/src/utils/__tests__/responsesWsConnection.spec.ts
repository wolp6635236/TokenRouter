import { describe, expect, it } from 'vitest'
import { readResponsesWSConnectionMode, clearLegacyResponsesWSSettings } from '@/utils/responsesWsConnection'

describe('Responses WS connection settings', () => {
  it('defaults to pooled connections and reads the shared field', () => {
    expect(readResponsesWSConnectionMode(undefined)).toBe('pooled')
    expect(readResponsesWSConnectionMode({ responses_ws_connection_mode: 'per_session' })).toBe('per_session')
    expect(readResponsesWSConnectionMode({ openai_ws_enabled: false })).toBe('pooled')
  })
  it('removes retired fields without changing unrelated settings', () => {
    const extra = { responses_ws_connection_mode: 'per_session', openai_ws_enabled: false, openai_oauth_responses_websockets_v2_mode: 'off', custom: 3 }
    clearLegacyResponsesWSSettings(extra)
    expect(extra).toEqual({ responses_ws_connection_mode: 'per_session', custom: 3 })
  })
})
