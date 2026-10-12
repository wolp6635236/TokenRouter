<template>
  <BaseDialog
    :show="show"
    :title="t('admin.providers.reAuthorizeProvider')"
    width="normal"
    @close="handleClose"
  >
    <div v-if="provider" class="space-y-4">
      <!-- Provider Info -->
      <div
        class="rounded-surface border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800"
      >
        <div class="flex items-center gap-3">
          <div
            :class="[
              'flex h-10 w-10 items-center justify-center rounded-control bg-gradient-to-br',
              isOpenAILike
                ? 'from-green-500 to-green-600'
                : isGemini
                  ? 'from-blue-500 to-blue-600'
                  : isAntigravity
                    ? 'from-purple-500 to-purple-600'
                    : isGrok
                      ? 'from-zinc-700 to-zinc-900'
                      : 'from-orange-500 to-orange-600'
            ]"
          >
            <Icon name="sparkles" size="md" class="text-white" />
          </div>
          <div>
            <span class="block font-semibold text-gray-900 dark:text-white">{{
              provider.name
            }}</span>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                isOpenAI
                  ? t('admin.providers.openaiProvider')
                  : isGemini
                    ? t('admin.providers.geminiProvider')
                    : isAntigravity
                      ? t('admin.providers.antigravityProvider')
                      : isGrok
                        ? t('admin.providers.grokProvider')
                        : t('admin.providers.claudeCodeProvider')
              }}
            </span>
          </div>
        </div>
      </div>

      <!-- Add Method Selection (Claude only) -->
      <fieldset v-if="isAnthropic" class="border-0 p-0">
        <legend class="input-label">{{ t('admin.providers.oauth.authMethod') }}</legend>
        <div class="mt-2 flex gap-4">
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="oauth"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.providers.types.oauth')
            }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.providers.setupTokenLongLived')
            }}</span>
          </label>
        </div>
      </fieldset>

      <!-- Gemini OAuth Type Display (read-only) -->
      <div v-if="isGemini" class="rounded-surface border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800">
        <div class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.providers.oauth.gemini.oauthTypeLabel') }}
        </div>
        <div class="flex items-center gap-3">
          <div
            :class="[
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-control',
              geminiOAuthType === 'google_one'
                ? 'bg-purple-500 text-white'
                : geminiOAuthType === 'code_assist'
                  ? 'bg-blue-500 text-white'
                  : 'bg-amber-500 text-white'
            ]"
          >
            <Icon v-if="geminiOAuthType === 'google_one'" name="user" size="sm" />
            <Icon v-else-if="geminiOAuthType === 'code_assist'" name="cloud" size="sm" />
            <Icon v-else name="sparkles" size="sm" />
          </div>
          <div>
            <span class="block text-sm font-medium text-gray-900 dark:text-white">
              {{
                geminiOAuthType === 'google_one'
                  ? 'Google One'
                  : geminiOAuthType === 'code_assist'
                    ? t('admin.providers.gemini.oauthType.builtInTitle')
                    : t('admin.providers.gemini.oauthType.customTitle')
              }}
            </span>
            <span class="text-xs text-gray-500 dark:text-gray-400">
              {{
                geminiOAuthType === 'google_one'
                  ? '个人提供商'
                  : geminiOAuthType === 'code_assist'
                    ? t('admin.providers.gemini.oauthType.builtInDesc')
                    : t('admin.providers.gemini.oauthType.customDesc')
              }}
            </span>
          </div>
        </div>
      </div>

      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        :add-method="addMethod"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentLoading"
        :error="currentError"
        :show-help="isAnthropic"
        :show-proxy-warning="isAnthropic"
        :show-cookie-option="isAnthropic"
        :show-refresh-token-option="isOpenAI || isAntigravity || isGrok"
        :show-sso-option="isGrok"
        :show-email-password-option="false"
        :allow-multiple="false"
        :method-label="t('admin.providers.inputMethod')"
        :platform="isOpenAI ? 'openai' : isGemini ? 'gemini' : isAntigravity ? 'antigravity' : isGrok ? 'grok' : 'anthropic'"
        :show-project-id="isGemini && geminiOAuthType === 'code_assist'"
        :initial-input-method="grokInitialInputMethod"
        @generate-url="handleGenerateUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleValidateRefreshToken"
        @import-sso="handleGrokImportSSO"
      />

    </div>

    <template #footer>
      <div v-if="provider" class="flex justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          v-if="isManualInputMethod"
          type="button"
          :disabled="!canExchangeCode"
          class="btn btn-primary"
          @click="handleExchangeCode"
        >
          <Icon
            name="loader"
            size="sm"
            :animate-on-hover="false"
            v-if="currentLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
          />
          {{
            currentLoading
              ? t('admin.providers.oauth.verifying')
              : t('admin.providers.oauth.completeAuth')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import {
  useProviderOAuth,
  type AddMethod,
  type AuthInputMethod
} from '@/composables/useProviderOAuth'
import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'
import { useGeminiOAuth } from '@/composables/useGeminiOAuth'
import { useAntigravityOAuth } from '@/composables/useAntigravityOAuth'
import { useGrokOAuth } from '@/composables/useGrokOAuth'
import type { Provider } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import OAuthAuthorizationFlow from '@/components/provider/OAuthAuthorizationFlow.vue'

// Type for exposed OAuthAuthorizationFlow component
// Note: defineExpose automatically unwraps refs, so we use the unwrapped types
interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  projectId: string
  sessionKey: string
  inputMethod: AuthInputMethod
  reset: () => void
}

interface Props {
  show: boolean
  provider: Provider | null
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  reauthorized: [provider: Provider]
}>()

const appStore = useAppStore()
const { t } = useI18n()

// OAuth composables
const claudeOAuth = useProviderOAuth()
const openaiOAuth = useOpenAIOAuth()
const geminiOAuth = useGeminiOAuth()
const antigravityOAuth = useAntigravityOAuth()
const grokOAuth = useGrokOAuth()

// Refs
const oauthFlowRef = ref<OAuthFlowExposed | null>(null)

// State
const addMethod = ref<AddMethod>('oauth')
const geminiOAuthType = ref<'code_assist' | 'google_one' | 'ai_studio'>('code_assist')

// Computed - check platform
const isOpenAI = computed(() => props.provider?.platform === 'openai')
const isOpenAILike = computed(() => isOpenAI.value)
const isGemini = computed(() => props.provider?.platform === 'gemini')
const isAnthropic = computed(() => props.provider?.platform === 'anthropic')
const isAntigravity = computed(() => props.provider?.platform === 'antigravity')
const isGrok = computed(() => props.provider?.platform === 'grok')

/**
 * Grok 重新认证默认标签页，密码认证保持隐藏：
 * - 刷新令牌可能仍有效时使用 refresh_token
 * - 其他情况使用 SSO Cookie
 */
const grokInitialInputMethod = computed<AuthInputMethod>(() => {
  if (!isGrok.value) return 'manual'
  const creds = (props.provider?.credentials || {}) as Record<string, unknown>
  const hasRT =
    (typeof creds.refresh_token === 'string' && creds.refresh_token.trim() !== '') ||
    (typeof creds.has_refresh_token === 'boolean' && creds.has_refresh_token)
  if (hasRT) return 'refresh_token'
  return 'sso_cookie'
})

// Computed - current OAuth state based on platform
const currentAuthUrl = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.authUrl.value
  if (isGemini.value) return geminiOAuth.authUrl.value
  if (isAntigravity.value) return antigravityOAuth.authUrl.value
  if (isGrok.value) return grokOAuth.authUrl.value
  return claudeOAuth.authUrl.value
})
const currentSessionId = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.sessionId.value
  if (isGemini.value) return geminiOAuth.sessionId.value
  if (isAntigravity.value) return antigravityOAuth.sessionId.value
  if (isGrok.value) return grokOAuth.sessionId.value
  return claudeOAuth.sessionId.value
})
const currentLoading = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.loading.value
  if (isGemini.value) return geminiOAuth.loading.value
  if (isAntigravity.value) return antigravityOAuth.loading.value
  if (isGrok.value) return grokOAuth.loading.value
  return claudeOAuth.loading.value
})
const currentError = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.error.value
  if (isGemini.value) return geminiOAuth.error.value
  if (isAntigravity.value) return antigravityOAuth.error.value
  if (isGrok.value) return grokOAuth.error.value
  return claudeOAuth.error.value
})

// 授权码交换流程使用页脚“完成认证”按钮。
const isManualInputMethod = computed(() => {
  const method = oauthFlowRef.value?.inputMethod
  if (method === 'sso_cookie' || method === 'email_password' || method === 'refresh_token') {
    return false
  }
  // OpenAI、Gemini、Antigravity 与 Grok 默认使用手动粘贴代码，不采用 Cookie 认证。
  return (
    isOpenAILike.value ||
    isGemini.value ||
    isAntigravity.value ||
    isGrok.value ||
    method === 'manual'
  )
})

const canExchangeCode = computed(() => {
  const authCode = oauthFlowRef.value?.authCode || ''
  const sessionId = currentSessionId.value
  const loading = currentLoading.value
  return authCode.trim() && sessionId && !loading
})

// Watchers
watch(
  () => props.show,
  (newVal) => {
    if (newVal && props.provider) {
      // Initialize addMethod based on current provider type (Claude only)
      if (
        isAnthropic.value &&
        (props.provider.type === 'oauth' || props.provider.type === 'setup-token')
      ) {
        addMethod.value = props.provider.type as AddMethod
      }
      if (isGemini.value) {
        const creds = (props.provider.credentials || {}) as Record<string, unknown>
        geminiOAuthType.value =
          creds.oauth_type === 'google_one'
            ? 'google_one'
            : creds.oauth_type === 'ai_studio'
              ? 'ai_studio'
              : 'code_assist'
      }
    } else {
      resetState()
    }
  }
)

// Methods
const resetState = () => {
  addMethod.value = 'oauth'
  geminiOAuthType.value = 'code_assist'
  claudeOAuth.resetState()
  openaiOAuth.resetState()
  geminiOAuth.resetState()
  antigravityOAuth.resetState()
  grokOAuth.resetState()
  oauthFlowRef.value?.reset()
}

const handleClose = () => {
  emit('close')
}

const handleGenerateUrl = async () => {
  if (!props.provider) return

  if (isOpenAILike.value) {
    await openaiOAuth.generateAuthUrl(props.provider.proxy_id)
  } else if (isGemini.value) {
    const creds = (props.provider.credentials || {}) as Record<string, unknown>
    const tierId = typeof creds.tier_id === 'string' ? creds.tier_id : undefined
    const projectId = geminiOAuthType.value === 'code_assist' ? oauthFlowRef.value?.projectId : undefined
    await geminiOAuth.generateAuthUrl(props.provider.proxy_id, projectId, geminiOAuthType.value, tierId)
  } else if (isAntigravity.value) {
    await antigravityOAuth.generateAuthUrl(props.provider.proxy_id)
  } else if (isGrok.value) {
    await grokOAuth.generateAuthUrl(props.provider.proxy_id)
  } else {
    await claudeOAuth.generateAuthUrl(addMethod.value, props.provider.proxy_id)
  }
}

const handleExchangeCode = async () => {
  if (!props.provider) return

  const authCode = oauthFlowRef.value?.authCode || ''
  if (!authCode.trim()) return

  if (isOpenAILike.value) {
    // OpenAI OAuth flow
    const oauthClient = openaiOAuth
    const sessionId = oauthClient.sessionId.value
    if (!sessionId) return
    const stateToUse = (oauthFlowRef.value?.oauthState || oauthClient.oauthState.value || '').trim()
    if (!stateToUse) {
      oauthClient.error.value = t('admin.providers.oauth.authFailed')
      appStore.showError(oauthClient.error.value)
      return
    }

    const tokenInfo = await oauthClient.exchangeAuthCode(
      authCode.trim(),
      sessionId,
      stateToUse,
      props.provider.proxy_id,
      props.provider.tls_fingerprint_router_id
    )
    if (!tokenInfo) return

    // 构建新的 OAuth 凭据和可增量合并的提供商 Extra。
    const credentials = oauthClient.buildCredentials(tokenInfo)
    const extra = oauthClient.buildExtraInfo(tokenInfo)

    try {
      const updatedProvider = await adminAPI.providers.applyOAuthCredentials(props.provider.id, {
        type: 'oauth',
        credentials,
        extra
      })

      appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
      emit('reauthorized', updatedProvider)
      handleClose()
    } catch (error: any) {
      oauthClient.error.value = error.response?.data?.detail || t('admin.providers.oauth.authFailed')
      appStore.showError(oauthClient.error.value)
    }
  } else if (isGemini.value) {
    const sessionId = geminiOAuth.sessionId.value
    if (!sessionId) return

    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || geminiOAuth.state.value
    if (!stateToUse) return

    const tokenInfo = await geminiOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId,
      state: stateToUse,
      proxyId: props.provider.proxy_id,
      oauthType: geminiOAuthType.value,
      tierId: typeof (props.provider.credentials as any)?.tier_id === 'string' ? ((props.provider.credentials as any).tier_id as string) : undefined
    })
    if (!tokenInfo) return

    const credentials = geminiOAuth.buildCredentials(tokenInfo)

    try {
      await adminAPI.providers.update(props.provider.id, {
        type: 'oauth',
        credentials
      })
      const updatedProvider = await adminAPI.providers.clearError(props.provider.id)
      appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
      emit('reauthorized', updatedProvider)
      handleClose()
    } catch (error: any) {
      geminiOAuth.error.value = error.response?.data?.detail || t('admin.providers.oauth.authFailed')
      appStore.showError(geminiOAuth.error.value)
    }
  } else if (isAntigravity.value) {
    // Antigravity OAuth flow
    const sessionId = antigravityOAuth.sessionId.value
    if (!sessionId) return

    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || antigravityOAuth.state.value
    if (!stateToUse) return

    const tokenInfo = await antigravityOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId,
      state: stateToUse,
      proxyId: props.provider.proxy_id
    })
    if (!tokenInfo) return

    const credentials = antigravityOAuth.buildCredentials(tokenInfo)

    try {
      await adminAPI.providers.update(props.provider.id, {
        type: 'oauth',
        credentials
      })
      const updatedProvider = await adminAPI.providers.clearError(props.provider.id)
      appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
      emit('reauthorized', updatedProvider)
      handleClose()
    } catch (error: any) {
      antigravityOAuth.error.value = error.response?.data?.detail || t('admin.providers.oauth.authFailed')
      appStore.showError(antigravityOAuth.error.value)
    }
  } else if (isGrok.value) {
    const sessionId = grokOAuth.sessionId.value
    if (!sessionId) return

    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || grokOAuth.state.value
    if (!stateToUse) return

    const tokenInfo = await grokOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId,
      state: stateToUse,
      proxyId: props.provider.proxy_id
    })
    if (!tokenInfo) return

    const credentials = grokOAuth.buildCredentials(tokenInfo)
    const extra = grokOAuth.buildExtraInfo(tokenInfo)

    try {
      const updatedProvider = await adminAPI.providers.applyOAuthCredentials(props.provider.id, {
        type: 'oauth',
        credentials,
        extra
      })

      appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
      emit('reauthorized', updatedProvider)
      handleClose()
    } catch (error: any) {
      grokOAuth.error.value = error.response?.data?.detail || t('admin.providers.oauth.authFailed')
      appStore.showError(grokOAuth.error.value)
    }
  } else {
    // Claude OAuth flow
    const sessionId = claudeOAuth.sessionId.value
    if (!sessionId) return

    claudeOAuth.loading.value = true
    claudeOAuth.error.value = ''

    try {
      const proxyConfig = props.provider.proxy_id ? { proxy_id: props.provider.proxy_id } : {}
      const endpoint =
        addMethod.value === 'oauth'
          ? '/admin/providers/exchange-code'
          : '/admin/providers/exchange-setup-token-code'

      const tokenInfo = await adminAPI.providers.exchangeCode(endpoint, {
        session_id: sessionId,
        code: authCode.trim(),
        ...proxyConfig
      })

      const extra = claudeOAuth.buildExtraInfo(tokenInfo)

      // Update provider with new credentials and type
      await adminAPI.providers.update(props.provider.id, {
        type: addMethod.value, // Update type based on selected method
        credentials: tokenInfo,
        extra
      })

      // Clear error status after successful re-authorization
      const updatedProvider = await adminAPI.providers.clearError(props.provider.id)

      appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
      emit('reauthorized', updatedProvider)
      handleClose()
    } catch (error: any) {
      claudeOAuth.error.value = error.response?.data?.detail || t('admin.providers.oauth.authFailed')
      appStore.showError(claudeOAuth.error.value)
    } finally {
      claudeOAuth.loading.value = false
    }
  }
}

const handleCookieAuth = async (sessionKey: string) => {
  if (!props.provider || isOpenAILike.value) return

  claudeOAuth.loading.value = true
  claudeOAuth.error.value = ''

  try {
    const proxyConfig = props.provider.proxy_id ? { proxy_id: props.provider.proxy_id } : {}
    const endpoint =
      addMethod.value === 'oauth'
        ? '/admin/providers/cookie-auth'
        : '/admin/providers/setup-token-cookie-auth'

    const tokenInfo = await adminAPI.providers.exchangeCode(endpoint, {
      session_id: '',
      code: sessionKey.trim(),
      ...proxyConfig
    })

    const extra = claudeOAuth.buildExtraInfo(tokenInfo)

    // Update provider with new credentials and type
    await adminAPI.providers.update(props.provider.id, {
      type: addMethod.value, // Update type based on selected method
      credentials: tokenInfo,
      extra
    })

    // Clear error status after successful re-authorization
    const updatedProvider = await adminAPI.providers.clearError(props.provider.id)

    appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
    emit('reauthorized', updatedProvider)
    handleClose()
  } catch (error: any) {
    claudeOAuth.error.value =
      error.response?.data?.detail || t('admin.providers.oauth.cookieAuthFailed')
  } finally {
    claudeOAuth.loading.value = false
  }
}

/** 将 Grok Build OAuth 令牌应用到现有提供商，绝不存储密码或 SSO 数据。 */
const applyGrokReauthTokenInfo = async (tokenInfo: {
  access_token?: string
  refresh_token?: string
  email?: string
  [key: string]: unknown
}) => {
  if (!props.provider) return
  const credentials = grokOAuth.buildCredentials(tokenInfo as any)
  const extra = grokOAuth.buildExtraInfo(tokenInfo as any)
  const updatedProvider = await adminAPI.providers.applyOAuthCredentials(props.provider.id, {
    type: 'oauth',
    credentials,
    extra
  })
  appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
  emit('reauthorized', updatedProvider)
  handleClose()
}

/** 使用单个刷新令牌重新认证现有提供商；Grok 仍沿用其专用校验流程。 */
const handleValidateRefreshToken = async (refreshTokenInput: string) => {
  if (!props.provider) return
  if (isGrok.value) {
    await handleGrokValidateRefreshToken(refreshTokenInput)
    return
  }

  const refreshToken = refreshTokenInput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)[0]
  if (!refreshToken) return

  if (isOpenAILike.value) {
    openaiOAuth.loading.value = true
    openaiOAuth.error.value = ''
    try {
      const tokenInfo = await openaiOAuth.validateRefreshToken(refreshToken, props.provider.proxy_id)
      if (!tokenInfo) return

      const updatedProvider = await adminAPI.providers.applyOAuthCredentials(props.provider.id, {
        type: 'oauth',
        credentials: openaiOAuth.buildCredentials(tokenInfo),
        extra: openaiOAuth.buildExtraInfo(tokenInfo)
      })
      appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
      emit('reauthorized', updatedProvider)
      handleClose()
    } catch (error: any) {
      openaiOAuth.error.value =
        error.response?.data?.detail ||
        error.response?.data?.message ||
        error.message ||
        t('admin.providers.oauth.authFailed')
      appStore.showError(openaiOAuth.error.value)
    } finally {
      openaiOAuth.loading.value = false
    }
    return
  }

  if (!isAntigravity.value) return
  antigravityOAuth.loading.value = true
  antigravityOAuth.error.value = ''
  try {
    const tokenInfo = await antigravityOAuth.validateRefreshToken(refreshToken, props.provider.proxy_id)
    if (!tokenInfo) return

    const updatedProvider = await adminAPI.providers.applyOAuthCredentials(props.provider.id, {
      type: 'oauth',
      credentials: antigravityOAuth.buildCredentials(tokenInfo, refreshToken)
    })
    appStore.showSuccess(t('admin.providers.reAuthorizedSuccess'))
    emit('reauthorized', updatedProvider)
    handleClose()
  } catch (error: any) {
    antigravityOAuth.error.value =
      error.response?.data?.detail ||
      error.response?.data?.message ||
      error.message ||
      t('admin.providers.oauth.authFailed')
    appStore.showError(antigravityOAuth.error.value)
  } finally {
    antigravityOAuth.loading.value = false
  }
}

/** 使用单个 SSO Cookie 重新认证并转换为 Build OAuth，不执行批量创建。 */
const handleGrokImportSSO = async (ssoInput: string) => {
  if (!props.provider || !isGrok.value) return
  const ssoToken = ssoInput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)[0]
  if (!ssoToken) return

  grokOAuth.loading.value = true
  grokOAuth.error.value = ''
  try {
    const tokenInfo = await grokOAuth.validateSSOToken(ssoToken, props.provider.proxy_id)
    if (!tokenInfo) return
    await applyGrokReauthTokenInfo(tokenInfo)
  } catch (error: any) {
    grokOAuth.error.value =
      error.response?.data?.detail ||
      error.message ||
      t('admin.providers.oauth.grok.failedToValidateSSO', 'Failed to validate Grok SSO')
    appStore.showError(grokOAuth.error.value)
  } finally {
    grokOAuth.loading.value = false
  }
}

/** 使用单个刷新令牌重新认证。 */
const handleGrokValidateRefreshToken = async (refreshTokenInput: string) => {
  if (!props.provider || !isGrok.value) return
  const refreshToken = refreshTokenInput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)[0]
  if (!refreshToken) {
    grokOAuth.error.value = t('admin.providers.oauth.grok.pleaseEnterRefreshToken')
    return
  }

  grokOAuth.loading.value = true
  grokOAuth.error.value = ''
  try {
    const tokenInfo = await grokOAuth.validateRefreshToken(refreshToken, props.provider.proxy_id)
    if (!tokenInfo) return
    await applyGrokReauthTokenInfo(tokenInfo)
  } catch (error: any) {
    grokOAuth.error.value =
      error.response?.data?.detail ||
      error.message ||
      t('admin.providers.oauth.grok.failedToValidateRT')
    appStore.showError(grokOAuth.error.value)
  } finally {
    grokOAuth.loading.value = false
  }
}
</script>
