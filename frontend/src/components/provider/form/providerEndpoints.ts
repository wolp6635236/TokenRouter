import type { IconName } from '@/components/icons/registry'
import type { ProtocolID } from '@/api/protocols.types'

// providerEndpoints.ts supplies per-platform connection presets for the provider
// create and edit forms: default forwarding addresses, input placeholders,
// provider modes, and the api_base_urls address slots. Both dialogs read these
// tables, so each platform's defaults are declared once and the forms branch on
// table entries.
//
// CN platforms keep complete preset addresses for each billing mode and
// protocol, and adaptive forms prefill every slot from those presets. OpenCode
// derives each protocol address from a mode-specific service root plus a fixed
// path, so its slots stay empty until an administrator overrides one. Grok
// customization checks compare parsed hosts with the default CLI gateway.

// GROK_DEFAULT_GATEWAY_HOST identifies the CLI gateway written by ordinary
// Grok OAuth creation and refresh operations.
const GROK_DEFAULT_GATEWAY_HOST = 'cli-chat-proxy.grok.com'

// isCustomGrokBaseUrl reports whether a stored address selects a custom Grok
// forwarding host. Empty, non-string, malformed, and default CLI gateway
// addresses return false; other valid URL hosts return true.
export function isCustomGrokBaseUrl(value: unknown): boolean {
  if (typeof value !== 'string') return false
  const trimmed = value.trim()
  if (!trimmed) return false
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    // Malformed stored addresses cannot initialize a custom endpoint
    // selection, so the form treats them as unset.
    return false
  }
  return parsed.hostname.toLowerCase() !== GROK_DEFAULT_GATEWAY_HOST
}

// GrokBaseUrlPreset describes a selectable endpoint and its translated or
// literal label.
export interface GrokBaseUrlPreset {
  // label and labelKey provide alternate names for a preset. The preset control
  // uses a supplied literal label before translating the optional labelKey.
  labelKey?: 'cli' | 'official' // Translation label suffix.
  label?: string // Literal regional label.
  url: string // Forwarding endpoint URL.
}

// GROK_BASE_URL_PRESETS supplies clickable forwarding endpoints while the
// form continues accepting administrator-entered addresses as well.
export const GROK_BASE_URL_PRESETS: GrokBaseUrlPreset[] = [
  { labelKey: 'cli', url: 'https://cli-chat-proxy.grok.com/v1' },
  { labelKey: 'official', url: 'https://api.x.ai/v1' },
  { label: 'us-east-1', url: 'https://us-east-1.api.x.ai/v1' },
  { label: 'us-west-2', url: 'https://us-west-2.api.x.ai/v1' },
  { label: 'eu-west-1', url: 'https://eu-west-1.api.x.ai/v1' },
]

// CnProviderMode identifies the billing mode used to choose provider
// endpoint defaults.
export type CnProviderMode = 'payg' | 'coding'

// ProviderMode is the credentials.provider_mode value edited by provider forms.
// CN platforms use payg and coding, while OpenCode uses payg and go.
export type ProviderMode = CnProviderMode | 'go'

// ProviderModeChoice describes one selectable provider_mode card or segment.
export interface ProviderModeChoice {
  value: ProviderMode // Persisted provider_mode value.
  icon: IconName // Choice card icon.
  titleKey: string // Translation key of the choice title.
  descriptionKey: string // Translation key of the choice description.
}

// CN_PAYG_MODE and CN_CODING_MODE present the CN billing modes shared by Kimi,
// Zhipu, and DeepSeek mode selectors.
const CN_PAYG_MODE: ProviderModeChoice = {
  value: 'payg',
  icon: 'creditCard',
  titleKey: 'admin.providers.cnProviders.providerMode.payg',
  descriptionKey: 'admin.providers.cnProviders.providerMode.paygDesc',
}
const CN_CODING_MODE: ProviderModeChoice = {
  value: 'coding',
  icon: 'bolt',
  titleKey: 'admin.providers.cnProviders.providerMode.coding',
  descriptionKey: 'admin.providers.cnProviders.providerMode.codingDesc',
}

// PROVIDER_MODE_CHOICES lists provider_mode values each API-key platform accepts,
// in display order. Forms show a mode selector only for listed platforms and use
// the first entry when the current mode is not offered by the selected platform.
const PROVIDER_MODE_CHOICES: Partial<Record<string, ProviderModeChoice[]>> = {
  kimi: [CN_PAYG_MODE, CN_CODING_MODE],
  zhipu: [CN_PAYG_MODE, CN_CODING_MODE],
  deepseek: [CN_PAYG_MODE],
  opencode: [
    {
      value: 'payg',
      icon: 'creditCard',
      titleKey: 'admin.providers.opencode.payg',
      descriptionKey: 'admin.providers.opencode.paygDesc',
    },
    {
      value: 'go',
      icon: 'bolt',
      titleKey: 'admin.providers.opencode.go',
      descriptionKey: 'admin.providers.opencode.goDesc',
    },
  ],
}

// providerModeChoices returns the provider_mode choices for a platform. An empty
// list means the platform has no mode and its forms omit provider_mode.
export function providerModeChoices(platform: string): ProviderModeChoice[] {
  return PROVIDER_MODE_CHOICES[platform] ?? []
}

// CnApiProtocol identifies a native protocol or adaptive endpoint selection
// for CN providers.
export type CnApiProtocol = 'adaptive' | 'chat_completions' | 'anthropic' | 'responses'

// CnNativeApiProtocol identifies an individual native protocol with a
// configurable forwarding endpoint.
export type CnNativeApiProtocol = Exclude<CnApiProtocol, 'adaptive'>

// cnSupportsNativeResponses reports whether the CN platform offers native
// Responses endpoints.
export function cnSupportsNativeResponses(platform: string): boolean {
  return platform === 'deepseek' || platform === 'kimi'
}

// CnBaseUrlPreset associates a billing mode and protocol with a selectable
// endpoint.
export interface CnBaseUrlPreset {
  mode: CnProviderMode // Provider billing mode.
  protocol: CnApiProtocol // Selected API protocol.
  label: string // Literal preset label.
  url: string // Forwarding endpoint URL.
}

// CN_BASE_URL_PRESETS groups endpoint shortcuts by platform, billing mode,
// and native protocol for provider forms.
export const CN_BASE_URL_PRESETS: Record<'kimi' | 'zhipu' | 'deepseek', CnBaseUrlPreset[]> = {
  kimi: [
    {
      mode: 'payg',
      protocol: 'chat_completions',
      label: 'Moonshot',
      url: 'https://api.moonshot.cn/v1',
    },
    {
      mode: 'payg',
      protocol: 'anthropic',
      label: 'Moonshot Anthropic',
      url: 'https://api.moonshot.cn/anthropic',
    },
    {
      mode: 'payg',
      protocol: 'responses',
      label: 'Moonshot Responses',
      url: 'https://api.moonshot.cn/v1',
    },
    {
      mode: 'coding',
      protocol: 'chat_completions',
      label: 'Kimi For Coding',
      url: 'https://api.kimi.com/coding/v1',
    },
    {
      mode: 'coding',
      protocol: 'anthropic',
      label: 'Kimi Coding Anthropic',
      url: 'https://api.kimi.com/coding',
    },
    {
      mode: 'coding',
      protocol: 'responses',
      label: 'Kimi Coding Responses',
      url: 'https://api.kimi.com/coding/v1',
    },
  ],
  zhipu: [
    {
      mode: 'payg',
      protocol: 'chat_completions',
      label: 'GLM PaaS',
      url: 'https://open.bigmodel.cn/api/paas/v4',
    },
    {
      mode: 'payg',
      protocol: 'anthropic',
      label: 'GLM Anthropic',
      url: 'https://open.bigmodel.cn/api/anthropic',
    },
    {
      mode: 'coding',
      protocol: 'chat_completions',
      label: 'GLM Coding',
      url: 'https://open.bigmodel.cn/api/coding/paas/v4',
    },
    {
      mode: 'coding',
      protocol: 'anthropic',
      label: 'GLM Coding Anthropic',
      url: 'https://open.bigmodel.cn/api/anthropic',
    },
  ],
  deepseek: [
    {
      mode: 'payg',
      protocol: 'chat_completions',
      label: 'DeepSeek',
      url: 'https://api.deepseek.com',
    },
    {
      mode: 'payg',
      protocol: 'anthropic',
      label: 'DeepSeek Anthropic',
      url: 'https://api.deepseek.com/anthropic',
    },
    {
      mode: 'payg',
      protocol: 'responses',
      label: 'DeepSeek Responses',
      url: 'https://api.deepseek.com',
    },
  ],
}

// defaultCNBaseUrl returns the endpoint for a platform, billing mode, and
// protocol. Chat Completions is the default protocol; unknown platforms
// return an empty string, and adaptive selection uses that default address.
export function defaultCNBaseUrl(
  platform: string,
  mode: ProviderMode,
  protocol: CnApiProtocol = 'chat_completions',
): string {
  if (protocol === 'anthropic') {
    switch (platform) {
      case 'kimi':
        return mode === 'coding'
          ? 'https://api.kimi.com/coding'
          : 'https://api.moonshot.cn/anthropic'
      case 'zhipu':
        return 'https://open.bigmodel.cn/api/anthropic'
      case 'deepseek':
        return 'https://api.deepseek.com/anthropic'
      default:
        return ''
    }
  }
  // Responses and Chat Completions share the base address; the backend
  // appends the protocol-specific endpoint path.
  switch (platform) {
    case 'kimi':
      return mode === 'coding' ? 'https://api.kimi.com/coding/v1' : 'https://api.moonshot.cn/v1'
    case 'zhipu':
      return mode === 'coding'
        ? 'https://open.bigmodel.cn/api/coding/paas/v4'
        : 'https://open.bigmodel.cn/api/paas/v4'
    case 'deepseek':
      return 'https://api.deepseek.com'
    default:
      return ''
  }
}

// defaultCNAdaptiveBaseUrls returns separate endpoint defaults for every
// native protocol entry. Responses receives an empty string for platforms
// that do not support it, allowing forms to retain the full record shape.
export function defaultCNAdaptiveBaseUrls(
  platform: 'kimi' | 'zhipu' | 'deepseek',
  mode: ProviderMode,
): Record<CnNativeApiProtocol, string> {
  return {
    chat_completions: defaultCNBaseUrl(platform, mode, 'chat_completions'),
    anthropic: defaultCNBaseUrl(platform, mode, 'anthropic'),
    responses: cnSupportsNativeResponses(platform)
      ? defaultCNBaseUrl(platform, mode, 'responses')
      : '',
  }
}

// EndpointSlot names an api_base_urls key that holds one protocol's complete
// forwarding address.
export type EndpointSlot = CnNativeApiProtocol | 'gemini' | 'systemone'

// EndpointSlotAddresses is the form draft for api_base_urls. CN adaptive forms
// always fill the three text slots; other slots exist only for platforms that
// list them.
export type EndpointSlotAddresses = Record<CnNativeApiProtocol, string> &
  Partial<Record<EndpointSlot, string>>

// ENDPOINT_SLOT_PROTOCOLS maps each address slot to the native protocol it
// serves, so forms can label slots with protocol catalog names.
export const ENDPOINT_SLOT_PROTOCOLS: Record<EndpointSlot, ProtocolID> = {
  chat_completions: 'openai_chat_completions',
  anthropic: 'anthropic_messages',
  responses: 'openai_responses',
  gemini: 'gemini_generate_content',
  systemone: 'systemone',
}

// OPENCODE_SERVICE_ROOTS holds the official OpenCode inference roots per mode.
// The backend applies the same roots when credentials.base_url is blank.
const OPENCODE_SERVICE_ROOTS = {
  payg: 'https://opencode.ai/inference',
  go: 'https://opencode.ai/inference/go',
}

// openCodeServiceRoot returns the official inference root for a provider_mode.
// Values other than go select the payg root, matching the backend default.
function openCodeServiceRoot(mode: string): string {
  return mode === 'go' ? OPENCODE_SERVICE_ROOTS.go : OPENCODE_SERVICE_ROOTS.payg
}

// ROOT_DERIVED_ENDPOINTS lists platforms whose protocol addresses derive from one
// service root. Each slot's path is appended to the root and includes the version
// segment expected by that protocol's request builder; the backend resolves the
// same addresses when a slot is blank.
const ROOT_DERIVED_ENDPOINTS: Partial<
  Record<
    string,
    {
      serviceRoot: (mode: string) => string // Official root for a provider_mode.
      slots: Array<{ slot: EndpointSlot; suffix: string }> // Slot paths after the root.
    }
  >
> = {
  opencode: {
    serviceRoot: openCodeServiceRoot,
    slots: [
      { slot: 'anthropic', suffix: '/anthropic/v1' },
      { slot: 'responses', suffix: '/openai/v1' },
      { slot: 'chat_completions', suffix: '/openai/v1' },
      { slot: 'gemini', suffix: '/google/v1beta' },
      { slot: 'systemone', suffix: '/systemone/v1' },
    ],
  },
}

// DerivedEndpointSlot is one optional address override with the address used
// when the administrator leaves it blank.
export interface DerivedEndpointSlot {
  slot: EndpointSlot // api_base_urls key.
  placeholder: string // Address derived from the service root.
}

// derivedEndpointSlots lists optional per-protocol overrides for platforms whose
// protocol addresses derive from one service root. The entered root takes
// precedence over the mode's official root in each placeholder. Platforms
// without derived slots, including CN, return an empty list.
export function derivedEndpointSlots(
  platform: string,
  root: string,
  mode: string,
): DerivedEndpointSlot[] {
  const endpoints = ROOT_DERIVED_ENDPOINTS[platform]
  if (!endpoints) return []
  const base = root.trim().replace(/\/+$/, '') || endpoints.serviceRoot(mode)
  return endpoints.slots.map(({ slot, suffix }) => ({ slot, placeholder: base + suffix }))
}

// derivedEndpointOverrides returns the nonblank, trimmed address overrides for a
// platform's root-derived slots. Platforms without derived slots return undefined
// and keep their api_base_urls unchanged. Derived platforms always receive an
// object, so saving with every slot blank removes earlier overrides.
export function derivedEndpointOverrides(
  platform: string,
  addresses: Partial<Record<EndpointSlot, string>>,
): Partial<Record<EndpointSlot, string>> | undefined {
  const endpoints = ROOT_DERIVED_ENDPOINTS[platform]
  if (!endpoints) return undefined
  const overrides: Partial<Record<EndpointSlot, string>> = {}
  for (const { slot } of endpoints.slots) {
    const address = addresses[slot]?.trim()
    if (address) overrides[slot] = address
  }
  return overrides
}

// PlatformApiKeyPreset describes API-key connection defaults shown by both
// provider dialogs.
interface PlatformApiKeyPreset {
  // defaultBaseUrl fills a blank forwarding address on hydration and submit. An
  // empty string stores no address, letting the backend apply its mode default.
  defaultBaseUrl: string
  // baseUrlPlaceholder overrides the default address shown in the empty input;
  // a function receives the current provider_mode.
  baseUrlPlaceholder?: string | ((mode: string) => string)
  apiKeyPlaceholder: string // Example key prefix.
  // baseUrlHintKey selects the address hint; an empty string hides it and an
  // omitted key uses the generic hint.
  baseUrlHintKey?: string
  apiKeyOnly?: boolean // Platform accepts only API-key credentials.
}

// ANTHROPIC_APIKEY_PRESET applies to Anthropic and to platforms without their
// own entry, matching the forms' Anthropic-compatible default.
const ANTHROPIC_APIKEY_PRESET: PlatformApiKeyPreset = {
  defaultBaseUrl: 'https://api.anthropic.com',
  apiKeyPlaceholder: 'sk-ant-...',
}

// PLATFORM_APIKEY_PRESETS holds platform-specific API-key defaults. CN forms
// derive their addresses from CN_BASE_URL_PRESETS and take the remaining fields
// from ANTHROPIC_APIKEY_PRESET.
const PLATFORM_APIKEY_PRESETS: Partial<Record<string, PlatformApiKeyPreset>> = {
  openai: {
    defaultBaseUrl: 'https://api.openai.com',
    apiKeyPlaceholder: 'sk-proj-...',
    baseUrlHintKey: 'admin.providers.openai.baseUrlHint',
  },
  gemini: {
    defaultBaseUrl: 'https://generativelanguage.googleapis.com',
    apiKeyPlaceholder: 'AIza...',
    baseUrlHintKey: 'admin.providers.gemini.baseUrlHint',
  },
  // Antigravity stores the Anthropic-compatible address; its examples name Cloud Code.
  antigravity: {
    defaultBaseUrl: 'https://api.anthropic.com',
    baseUrlPlaceholder: 'https://cloudcode-pa.googleapis.com',
    apiKeyPlaceholder: 'sk-...',
  },
  grok: { defaultBaseUrl: 'https://api.x.ai/v1', apiKeyPlaceholder: 'xai-...', baseUrlHintKey: '' },
  jev: {
    defaultBaseUrl: 'https://api.typesafe.ai',
    apiKeyPlaceholder: 'api-key-...',
    apiKeyOnly: true,
  },
  opencode: {
    defaultBaseUrl: '',
    baseUrlPlaceholder: openCodeServiceRoot,
    apiKeyPlaceholder: 'oc_sk_...',
    baseUrlHintKey: 'admin.providers.opencode.baseUrlHint',
    apiKeyOnly: true,
  },
}

// ApiKeyPreset is a platform preset resolved for one provider_mode.
export interface ApiKeyPreset {
  defaultBaseUrl: string // Address stored when the input is blank.
  baseUrlPlaceholder: string // Example address for the empty input.
  apiKeyPlaceholder: string // Example key prefix.
  baseUrlHintKey: string // Address hint key; empty hides the hint.
  apiKeyOnly: boolean // Platform accepts only API-key credentials.
}

// apiKeyPreset resolves API-key defaults for a platform and provider_mode.
// Unknown platforms receive the Anthropic-compatible defaults. Callers apply
// their own variants afterwards, such as Gemini relay sources.
export function apiKeyPreset(platform: string, mode = ''): ApiKeyPreset {
  const preset = PLATFORM_APIKEY_PRESETS[platform] ?? ANTHROPIC_APIKEY_PRESET
  const placeholder = preset.baseUrlPlaceholder ?? preset.defaultBaseUrl
  return {
    defaultBaseUrl: preset.defaultBaseUrl,
    baseUrlPlaceholder: typeof placeholder === 'function' ? placeholder(mode) : placeholder,
    apiKeyPlaceholder: preset.apiKeyPlaceholder,
    baseUrlHintKey: preset.baseUrlHintKey ?? 'admin.providers.baseUrlHint',
    apiKeyOnly: preset.apiKeyOnly === true,
  }
}
