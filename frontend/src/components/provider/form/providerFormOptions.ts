import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpenAIOAuthClientPolicy } from '@/types'
import { RESPONSES_WS_POOLED, RESPONSES_WS_PER_SESSION } from '@/utils/responsesWsConnection'

export type CodexFingerprintMode = 'off' | 'device' | 'session' | 'full'
export type RpmStrategy = 'tiered' | 'sticky_exempt'
export type AnthropicAPIKeyAuthScheme = 'x_api_key' | 'authorization_bearer'

// 以下选项在创建、编辑和批量编辑中含义一致，统一在这里维护文案与取值。

export function useCodexFingerprintModeOptions() {
  const { t } = useI18n()
  return computed(() => [
    { value: 'off' as CodexFingerprintMode, label: t('admin.providers.openai.codexFingerprintOff') },
    { value: 'device' as CodexFingerprintMode, label: t('admin.providers.openai.codexFingerprintDevice') },
    { value: 'session' as CodexFingerprintMode, label: t('admin.providers.openai.codexFingerprintSession') },
    { value: 'full' as CodexFingerprintMode, label: t('admin.providers.openai.codexFingerprintFull') }
  ])
}

export function useResponsesWSConnectionModeOptions() {
  const { t } = useI18n()
  return computed(() => [
    { value: RESPONSES_WS_POOLED, label: t('admin.providers.openai.wsConnectionPooled') },
    { value: RESPONSES_WS_PER_SESSION, label: t('admin.providers.openai.wsConnectionPerSession') }
  ])
}

export function useOpenAIOAuthClientPolicyOptions() {
  const { t } = useI18n()
  return computed(() => [
    { value: 'any' as OpenAIOAuthClientPolicy, label: t('admin.providers.openai.clientPolicyAny') },
    { value: 'codex_only' as OpenAIOAuthClientPolicy, label: t('admin.providers.openai.clientPolicyCodexOnly') },
    {
      value: 'tls_router_matched_only' as OpenAIOAuthClientPolicy,
      label: t('admin.providers.openai.clientPolicyTLSRouterMatchedOnly')
    }
  ])
}

export function useUserMsgQueueModeOptions() {
  const { t } = useI18n()
  return computed(() => [
    { value: '', label: t('admin.providers.quotaControl.rpmLimit.umqModeOff') },
    { value: 'throttle', label: t('admin.providers.quotaControl.rpmLimit.umqModeThrottle') },
    { value: 'serialize', label: t('admin.providers.quotaControl.rpmLimit.umqModeSerialize') }
  ])
}

export function useWebSearchEmulationOptions() {
  const { t } = useI18n()
  return computed(() => [
    { value: 'default', label: t('admin.providers.anthropic.webSearchDefault') },
    { value: 'enabled', label: t('admin.providers.anthropic.webSearchEnabled') },
    { value: 'disabled', label: t('admin.providers.anthropic.webSearchDisabled') }
  ])
}

export function useAnthropicAPIKeyAuthSchemeOptions() {
  const { t } = useI18n()
  return computed(() => [
    { value: 'x_api_key' as AnthropicAPIKeyAuthScheme, label: t('admin.providers.anthropic.apiKeyAuthSchemeXApiKey') },
    {
      value: 'authorization_bearer' as AnthropicAPIKeyAuthScheme,
      label: t('admin.providers.anthropic.apiKeyAuthSchemeBearer')
    }
  ])
}

export const CACHE_TTL_OVERRIDE_TARGET_OPTIONS = [
  { value: '5m', label: '5m' },
  { value: '1h', label: '1h' }
]
