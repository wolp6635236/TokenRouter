import type { Provider, UpstreamUsageAdapter } from '@/api/admin/providers.types'

/**
 * upstreamUsage.ts resolves query eligibility, enablement, and adapter identity
 * for provider forms and manual usage controls. API-key platform and mode rules
 * select fixed vendor adapters before saved relay preferences are considered.
 * OpenCode Go therefore uses the same adapter identity for visible controls,
 * response validation, and session-cache restoration. UPSTREAM_USAGE_ADAPTERS
 * then tells forms which controls the resolved adapter needs.
 *
 * Query support and the saved switch are separate facts: a supported provider
 * may have queries disabled. Missing switches use the platform default, with
 * Jev disabled by default. Consumers read the resolved values together when
 * validating an observation, or use the predicates for a single UI decision.
 */

/**
 * UpstreamUsageAdapterInfo describes how provider forms present one usage-query
 * adapter. It mirrors the backend adapter catalog's Automatic flag.
 */
export interface UpstreamUsageAdapterInfo {
  // automatic marks adapters chosen by platform and mode; forms hide the manual
  // adapter selector and keep the saved relay preference untouched.
  automatic: boolean
  // baseUrl describes the editable management address. Null means the adapter
  // queries a fixed vendor endpoint and forms show no address input. A missing
  // placeholder uses the translated "reuse the forwarding address" text.
  baseUrl: { placeholder?: string; hintKey: string } | null
}

/**
 * RELAY_ADAPTER and FIXED_VENDOR_ADAPTER describe manually selected relay formats
 * that default to the provider forwarding address, and platform-selected vendor
 * adapters with fixed endpoints.
 */
const RELAY_ADAPTER: UpstreamUsageAdapterInfo = {
  automatic: false,
  baseUrl: { hintKey: 'admin.providers.upstreamUsage.baseUrlHint' },
}
const FIXED_VENDOR_ADAPTER: UpstreamUsageAdapterInfo = { automatic: true, baseUrl: null }

/**
 * UPSTREAM_USAGE_ADAPTERS maps every adapter to its form presentation. Derouter and
 * OpenCode Go query dedicated management roots, which their placeholders show.
 */
export const UPSTREAM_USAGE_ADAPTERS: Record<UpstreamUsageAdapter, UpstreamUsageAdapterInfo> = {
  sub2api: RELAY_ADAPTER,
  new_api: RELAY_ADAPTER,
  zivv: RELAY_ADAPTER,
  derouter: {
    automatic: false,
    baseUrl: {
      placeholder: 'https://cf-api.derouter.ai',
      hintKey: 'admin.providers.upstreamUsage.derouterBaseUrlHint',
    },
  },
  opencode_go: {
    automatic: true,
    baseUrl: {
      placeholder: 'https://opencode.ai',
      hintKey: 'admin.providers.upstreamUsage.openCodeBaseUrlHint',
    },
  },
  kimi_balance: FIXED_VENDOR_ADAPTER,
  kimi_coding: FIXED_VENDOR_ADAPTER,
  zhipu_coding: FIXED_VENDOR_ADAPTER,
  deepseek_balance: FIXED_VENDOR_ADAPTER,
}

/** UpstreamUsageProvider contains the saved fields required to resolve query configuration. */
type UpstreamUsageProvider = Pick<Provider, 'type' | 'platform' | 'credentials' | 'extra'>

/** EffectiveUpstreamUsageConfig describes query eligibility and the expected response adapter. */
export interface EffectiveUpstreamUsageConfig {
  supported: boolean // Platform supports manual queries.
  enabled: boolean // Supported and switch enabled.
  // Unsupported provider types and modes have no adapter, so results cannot pass
  // response validation merely by naming a manually configured relay format.
  adapter: UpstreamUsageAdapter | null
}

// @project-doc docs/interfaces/upstream_usage.md#frontend_lifecycle
/**
 * resolveUpstreamUsageConfig combines saved provider identity and query settings.
 * Fixed platform adapters take precedence over manual relay selections; unsupported
 * types and modes return no adapter and cannot enable queries through a saved flag.
 *
 * Missing or null enabled flags use the platform default, while other configured
 * values must be boolean true. The same resolution governs buttons, request
 * admission, response identity, and cached observations so these consumers agree
 * when a provider changes platform or billing mode.
 */
export function resolveUpstreamUsageConfig(
  provider: UpstreamUsageProvider,
): EffectiveUpstreamUsageConfig {
  const config = provider.extra?.upstream_usage_query as Record<string, unknown> | undefined
  let adapter: UpstreamUsageAdapter | null
  if (provider.type !== 'apikey') {
    adapter = null
  } else if (provider.platform === 'opencode') {
    adapter = provider.credentials?.provider_mode === 'go' ? 'opencode_go' : null
  } else if (provider.platform === 'kimi') {
    adapter = provider.credentials?.provider_mode === 'coding' ? 'kimi_coding' : 'kimi_balance'
  } else if (provider.platform === 'zhipu') {
    adapter = provider.credentials?.provider_mode === 'coding' ? 'zhipu_coding' : null
  } else if (provider.platform === 'deepseek') {
    adapter = 'deepseek_balance'
  } else {
    // Unrecognized saved relay values use the server's default query format;
    // fixed vendor adapters above ignore those manual preferences entirely.
    adapter =
      config?.adapter === 'new_api' || config?.adapter === 'zivv' || config?.adapter === 'derouter'
        ? config.adapter
        : 'sub2api'
  }
  const supported = adapter !== null
  return {
    supported,
    enabled: supported && (config?.enabled ?? provider.platform !== 'jev') === true,
    adapter,
  }
}

/**
 * supportsUpstreamUsageQuery reports whether the provider has an applicable
 * query adapter. Supported providers can still disable queries, so callers
 * admitting requests use isUpstreamUsageQueryEnabled instead.
 */
export function supportsUpstreamUsageQuery(provider: UpstreamUsageProvider): boolean {
  return resolveUpstreamUsageConfig(provider).supported
}

/** isUpstreamUsageQueryEnabled reports whether the provider permits manual usage queries. */
export function isUpstreamUsageQueryEnabled(provider: UpstreamUsageProvider): boolean {
  return resolveUpstreamUsageConfig(provider).enabled
}
