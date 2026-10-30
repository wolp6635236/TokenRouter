import { describe, expect, it } from 'vitest'
import type { Provider } from '@/api/admin/providers.types'
import {
  isUpstreamUsageQueryEnabled,
  resolveUpstreamUsageConfig,
  supportsUpstreamUsageQuery,
} from '../upstreamUsage'

// upstreamUsage.spec.ts checks effective adapter selection and query admission
// from saved provider configuration. Fixed vendor modes, relay preferences, and
// default switches share assertions so display predicates cannot disagree with
// the controller's expected response adapter.

describe('effective upstream usage configuration', () => {
  // Saved manual preferences coexist with platform-owned adapters and unsupported
  // modes. All public reads agree about support, enablement, and the expected query adapter.
  it.each([
    ['opencode', 'go', 'new_api', 'opencode_go'],
    ['opencode', 'payg', 'new_api', null],
    ['opencode', undefined, 'sub2api', null],
    ['kimi', 'coding', 'sub2api', 'kimi_coding'],
    ['kimi', 'payg', 'sub2api', 'kimi_balance'],
    ['zhipu', 'coding', 'sub2api', 'zhipu_coding'],
    ['zhipu', 'payg', 'sub2api', null],
    ['deepseek', 'payg', 'sub2api', 'deepseek_balance'],
    ['openai', undefined, 'derouter', 'derouter'],
    ['openai', undefined, 'new_api', 'new_api'],
    ['openai', undefined, 'zivv', 'zivv'],
    ['openai', undefined, 'unknown', 'sub2api'],
  ])('resolves %s/%s with saved %s to %s', (platform, mode, savedAdapter, adapter) => {
    const provider = {
      type: 'apikey',
      platform,
      credentials: { provider_mode: mode },
      extra: { upstream_usage_query: { adapter: savedAdapter } },
    } as Pick<Provider, 'type' | 'platform' | 'credentials' | 'extra'>
    expect(resolveUpstreamUsageConfig(provider)).toEqual({
      supported: adapter !== null,
      enabled: adapter !== null,
      adapter,
    })
    expect(supportsUpstreamUsageQuery(provider)).toBe(adapter !== null)
    expect(isUpstreamUsageQueryEnabled(provider)).toBe(adapter !== null)
  })

  // A supported provider can disable queries independently of its adapter.
  // Missing and null switches follow platform defaults, while nonboolean flags never enable requests.
  it.each([
    ['opencode', undefined, true],
    ['opencode', null, true],
    ['opencode', false, false],
    ['opencode', 'true', false],
    ['jev', undefined, false],
    ['jev', null, false],
    ['jev', true, true],
  ])('resolves enabled state for %s with %s', (platform, enabled, expected) => {
    const provider = {
      type: 'apikey',
      platform,
      credentials: { provider_mode: 'go' },
      extra: { upstream_usage_query: { enabled } },
    } as Pick<Provider, 'type' | 'platform' | 'credentials' | 'extra'>
    expect(resolveUpstreamUsageConfig(provider).supported).toBe(true)
    expect(isUpstreamUsageQueryEnabled(provider)).toBe(expected)
  })

  // A non-API-key provider carries an otherwise valid Go mode and enabled flag.
  // Provider type eligibility prevents that configuration from exposing queries or accepting observations.
  it('rejects non-API-key providers despite enabled Go configuration', () => {
    expect(
      resolveUpstreamUsageConfig({
        type: 'oauth',
        platform: 'opencode',
        credentials: { provider_mode: 'go' },
        extra: { upstream_usage_query: { enabled: true } },
      }),
    ).toEqual({ supported: false, enabled: false, adapter: null })
  })
})
