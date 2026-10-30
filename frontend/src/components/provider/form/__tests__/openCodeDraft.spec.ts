import { describe, expect, it } from 'vitest'
import { openCodeCredentials, openCodeDraft } from '../openCodeDraft'
import { supportsUpstreamUsageQuery } from '@/utils/upstreamUsage'
import type { Provider } from '@/api/admin/providers.types'

// openCodeDraft.spec.ts checks mode isolation and credential serialization for
// OpenCode provider forms. These tests exercise nested draft ownership and query
// eligibility, which must remain consistent when an administrator switches
// between payg and Go or cancels an edited configuration.

describe('OpenCode provider drafts', () => {
  // A hydrated draft is edited in both modes before serialization. The source
  // record stays intact, and each model retains its mode-specific wire encoding.
  it('copies nested mode maps for both protocol tables', () => {
    const source = {
      provider_mode: 'go',
      opencode_model_protocols: {
        payg: { 'minimax-m3': 'openai_chat_completions' },
        go: { 'minimax-m3': 'anthropic_messages' },
      },
      opencode_catalog_protocols: { go: { known: 'anthropic_messages' } },
    }
    const draft = openCodeDraft(source)
    draft.protocols.go!.custom = 'systemone'
    draft.suggestions.go!.suggested = 'anthropic_messages'
    expect(source.opencode_model_protocols.go).not.toHaveProperty('custom')
    expect(source.opencode_catalog_protocols.go).not.toHaveProperty('suggested')
    expect(openCodeCredentials(draft)).toEqual({
      opencode_catalog_protocols: {
        payg: {},
        go: { known: 'anthropic_messages', suggested: 'anthropic_messages' },
      },
      opencode_model_protocols: {
        payg: source.opencode_model_protocols.payg,
        go: { ...source.opencode_model_protocols.go, custom: 'systemone' },
      },
    })
  })

  // A draft table receives an entry for a mode OpenCode does not accept. Serialization
  // writes only payg and go, so the backend never sees an unknown mode key.
  it('serializes only the modes OpenCode accepts', () => {
    const draft = openCodeDraft()
    draft.protocols.coding = { stray: 'systemone' }
    expect(openCodeCredentials(draft).opencode_model_protocols).toEqual({ payg: {}, go: {} })
  })

  // OpenCode keys can belong to payg or Go. The shared query predicate exposes
  // allowance refresh only for Go, even when a saved query switch is enabled.
  it('enables upstream allowance queries only for Go API keys', () => {
    const provider = {
      platform: 'opencode',
      type: 'apikey',
      credentials: {},
      extra: {},
    } as Provider
    expect(supportsUpstreamUsageQuery(provider)).toBe(false)
    provider.credentials.provider_mode = 'go'
    expect(supportsUpstreamUsageQuery(provider)).toBe(true)
    provider.type = 'oauth'
    expect(supportsUpstreamUsageQuery(provider)).toBe(false)
  })
})
