<template>
  <div class="space-y-6">
    <!-- 标题行：左侧平台标识与说明，右侧授权方式；两侧按垂直中线对齐，窄屏改为上下排列。 -->
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div class="flex min-w-0 items-center gap-3">
        <span
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-control"
          :class="platformBadgeLightClass(platform)"
        >
          <PlatformIcon :platform="platform" size="md" />
        </span>
        <div class="min-w-0">
          <h4 class="text-sm font-semibold text-primary-900 dark:text-dark-50">{{ oauthTitle }}</h4>
          <p v-if="inputMethod === 'manual'" class="input-hint">{{ oauthFollowSteps }}</p>
        </div>
      </div>

      <!-- 标签与控件同排，少量选项用分段控件，OpenAI 等选项较多的平台用下拉框。 -->
      <div v-if="showMethodSelection" class="flex shrink-0 items-center gap-3">
        <label :for="`${uid}-method`" class="shrink-0 text-sm text-gray-500 dark:text-gray-400">
          {{ methodLabel || t('admin.providers.oauth.authMethod') }}
        </label>
        <SettingsSegmented
          v-if="methodOptions.length <= 4"
          v-model="inputMethod"
          :ariaLabel="methodLabel || t('admin.providers.oauth.authMethod')"
          :options="methodOptions"
        />
        <Select
          v-else
          :id="`${uid}-method`"
          v-model="inputMethod"
          class="min-w-0 flex-1 sm:w-56 sm:flex-none"
          data-testid="oauth-method-select"
          :options="methodOptions"
        />
      </div>
    </div>

    <!-- Refresh Token（OpenAI / Antigravity / Grok / Mobile RT） -->
    <OAuthCredentialImport
      v-if="inputMethod === 'refresh_token' || inputMethod === 'mobile_refresh_token'"
      v-model="refreshTokenInput"
      :description="t(getOAuthKey('refreshTokenDesc'))"
      label="Refresh Token"
      :placeholder="t(getOAuthKey('refreshTokenPlaceholder'))"
      :count="parsedRefreshTokenCount"
      :count-hint="t('admin.providers.oauth.batchCreateProviders', { count: parsedRefreshTokenCount })"
      :loading="loading"
      :error="error"
      :submit-label="t(getOAuthKey('validateAndCreate'))"
      :loading-label="t(getOAuthKey('validating'))"
      @submit="handleValidateRefreshToken"
    />

    <!-- Grok Web SSO 转换为 Grok Build -->
    <OAuthCredentialImport
      v-if="inputMethod === 'sso_cookie'"
      v-model="ssoCookieInput"
      :description="t(getOAuthKey('ssoCookieDesc'))"
      :label="t(getOAuthKey('ssoCookieLabel'))"
      :placeholder="t(getOAuthKey('ssoCookiePlaceholder'))"
      :hint="t(getOAuthKey('ssoCookieHint'))"
      :count="parsedSSOCount"
      :rows="5"
      :loading="loading"
      :error="error"
      :submit-label="t(getOAuthKey('convertSSOAndCreate'))"
      :loading-label="t(getOAuthKey('convertingSSO'))"
      @submit="handleImportSSO"
    />

    <!-- Codex auth.json、Agent Identity 与会话凭据共用批量导入表单。 -->
    <OAuthCredentialImport
      v-if="inputMethod === 'codex_session' || inputMethod === 'agent_identity'"
      v-model="codexSessionInput"
      :description="t(isAgentIdentityInput ? 'admin.providers.oauth.openai.agentIdentityDesc' : 'admin.providers.oauth.openai.codexSessionDesc')"
      :label="t(isAgentIdentityInput ? 'admin.providers.oauth.openai.agentIdentityInputLabel' : 'admin.providers.oauth.openai.codexSessionInputLabel')"
      :placeholder="t(isAgentIdentityInput ? 'admin.providers.oauth.openai.agentIdentityPlaceholder' : 'admin.providers.oauth.openai.codexSessionPlaceholder')"
      :hint="t(isAgentIdentityInput ? 'admin.providers.oauth.openai.agentIdentityHint' : 'admin.providers.oauth.openai.codexSessionHint')"
      :count="parsedCodexSessionCount"
      :rows="8"
      :loading="loading"
      :error="error"
      :submit-label="t('admin.providers.oauth.openai.codexSessionImportAndCreate')"
      :loading-label="t('admin.providers.oauth.openai.validating')"
      @submit="handleImportCodexSession"
    />

    <!-- Codex PAT -->
    <OAuthCredentialImport
      v-if="inputMethod === 'codex_pat'"
      v-model="codexPATInput"
      password
      :description="t('admin.providers.oauth.openai.codexPatDesc')"
      :label="t('admin.providers.oauth.openai.codexPatInputLabel')"
      :placeholder="t('admin.providers.oauth.openai.codexPatPlaceholder')"
      :hint="t('admin.providers.oauth.openai.codexPatHint')"
      :loading="loading"
      :error="error"
      :submit-label="t('admin.providers.oauth.openai.codexPatImportAndCreate')"
      :loading-label="t('admin.providers.oauth.openai.validating')"
      @submit="handleImportCodexPAT"
    />

    <!-- Cookie 自动授权 -->
    <OAuthCredentialImport
      v-if="inputMethod === 'cookie'"
      v-model="sessionKeyInput"
      :description="t('admin.providers.oauth.cookieAutoAuthDesc')"
      :label="t('admin.providers.oauth.sessionKey')"
      :placeholder="allowMultiple ? t('admin.providers.oauth.sessionKeyPlaceholder') : t('admin.providers.oauth.sessionKeyPlaceholderSingle')"
      :count="allowMultiple ? parsedKeyCount : 0"
      :count-hint="t('admin.providers.oauth.batchCreateProviders', { count: parsedKeyCount })"
      :loading="loading"
      :error="error"
      :submit-label="t('admin.providers.oauth.startAutoAuth')"
      :loading-label="t('admin.providers.oauth.authorizing')"
      @submit="handleCookieAuth"
    >
      <template v-if="showHelp" #label-extra>
        <button
          type="button"
          class="inline-flex rounded-compact text-gray-400 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :aria-label="t('admin.providers.oauth.howToGetSessionKey')"
          :aria-expanded="showHelpDialog"
          @click="showHelpDialog = !showHelpDialog"
        >
          <Icon name="questionCircle" size="sm" />
        </button>
      </template>
      <Collapse :open="showHelpDialog && showHelp" unmount-on-hide>
        <SettingsNotice>
          <p class="font-medium">{{ t('admin.providers.oauth.howToGetSessionKey') }}</p>
          <ol class="list-inside list-decimal space-y-1">
            <li>{{ t('admin.providers.oauth.step1') }}</li>
            <li>{{ t('admin.providers.oauth.step2') }}</li>
            <li>{{ t('admin.providers.oauth.step3') }}</li>
            <li>{{ t('admin.providers.oauth.step4') }}</li>
            <li>{{ t('admin.providers.oauth.step5') }}</li>
            <li>{{ t('admin.providers.oauth.step6') }}</li>
          </ol>
          <p v-text="t('admin.providers.oauth.sessionKeyFormat')"></p>
        </SettingsNotice>
      </Collapse>
    </OAuthCredentialImport>

    <!-- 手动授权：生成链接、浏览器授权、粘贴授权码 -->
    <ol v-if="inputMethod === 'manual'">
      <OAuthStep :index="1" :title="oauthStep1GenerateUrl" :done="hasGeneratedUrl">
        <div v-if="showProjectId && platform === 'gemini'">
          <div class="mb-1.5 flex items-center gap-2">
            <label :for="`${uid}-project-id`" class="input-label mb-0">{{ t('admin.providers.oauth.gemini.projectIdLabel') }}</label>
            <a
              href="https://console.cloud.google.com/"
              target="_blank"
              rel="noopener noreferrer"
              class="inline-flex items-center gap-1 text-xs text-primary-600 hover:underline dark:text-primary-500"
            >
              <Icon name="questionCircle" size="xs" />
              {{ t('admin.providers.oauth.gemini.howToGetProjectId') }}
            </a>
          </div>
          <input
            :id="`${uid}-project-id`"
            v-model="projectId"
            type="text"
            class="input font-mono text-sm"
            :placeholder="t('admin.providers.oauth.gemini.projectIdPlaceholder')"
          />
          <p class="input-hint">{{ t('admin.providers.oauth.gemini.projectIdHint') }}</p>
        </div>

        <!-- OpenAI 支持追加多个授权链接，批量创建 -->
        <template v-if="isOpenAI">
          <div v-if="openAIAuthSessions.length > 0" class="space-y-2">
            <div
              v-for="(session, index) in openAIAuthSessions"
              :key="session.sessionId"
              class="flex items-center gap-2"
            >
              <span class="w-8 shrink-0 text-center text-xs font-medium tabular-nums text-gray-500 dark:text-gray-400">
                #{{ index + 1 }}
              </span>
              <input
                :value="session.authUrl"
                readonly
                type="text"
                class="input min-w-0 flex-1 font-mono text-xs"
                :aria-label="t('admin.providers.oauth.openAuthUrl')"
              />
              <button
                type="button"
                class="btn btn-secondary btn-icon shrink-0 px-0"
                :title="t('admin.providers.oauth.copyAuthUrl')"
                :aria-label="t('admin.providers.oauth.copyAuthUrl')"
                @click="handleCopySessionUrl(session.authUrl)"
              >
                <Icon name="copy" size="sm" />
              </button>
              <a
                :href="session.authUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-primary btn-icon shrink-0 px-0"
                :title="t('admin.providers.oauth.openAuthUrl')"
                :aria-label="t('admin.providers.oauth.openAuthUrl')"
              >
                <Icon name="externalLink" size="sm" />
              </a>
              <button
                v-if="isOpenAIBatchAuth"
                type="button"
                class="btn btn-secondary btn-icon shrink-0 px-0 text-red-600 hover:text-red-700 dark:text-red-400"
                :title="t('common.delete')"
                :aria-label="t('common.delete')"
                @click="handleRemoveAuthSession(session.sessionId)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <p v-if="isOpenAIBatchAuth" class="input-hint">
              {{ t('admin.providers.oauth.openai.multiAuthUrlHint', { count: openAIAuthSessions.length }) }}
            </p>
          </div>
          <button
            type="button"
            :disabled="loading"
            :class="openAIAuthSessions.length > 0 ? 'btn btn-secondary' : 'btn btn-primary'"
            @click="handleGenerateUrl"
          >
            <Icon v-if="loading" name="loader" size="sm" :animate-on-hover="false" class="animate-spin" />
            <Icon v-else name="plus" size="sm" />
            {{
              loading
                ? t('admin.providers.oauth.generating')
                : isOpenAIBatchAuth
                  ? t('admin.providers.oauth.openai.appendAuthUrl')
                  : oauthGenerateAuthUrl
            }}
          </button>
        </template>

        <button
          v-else-if="!authUrl"
          type="button"
          :disabled="loading"
          class="btn btn-primary"
          @click="handleGenerateUrl"
        >
          <Icon v-if="loading" name="loader" size="sm" :animate-on-hover="false" class="animate-spin" />
          <Icon v-else name="link" size="sm" />
          {{ loading ? t('admin.providers.oauth.generating') : oauthGenerateAuthUrl }}
        </button>

        <div v-else class="space-y-2">
          <div class="flex items-center gap-2">
            <input
              :value="authUrl"
              readonly
              type="text"
              class="input min-w-0 flex-1 font-mono text-xs"
              :aria-label="t('admin.providers.oauth.openAuthUrl')"
            />
            <button
              type="button"
              class="btn btn-secondary btn-icon shrink-0 px-0"
              :title="t('admin.providers.oauth.copyAuthUrl')"
              :aria-label="t('admin.providers.oauth.copyAuthUrl')"
              @click="handleCopyUrl"
            >
              <Icon v-if="!copied" name="clipboard" size="sm" />
              <Icon v-else name="check" size="sm" class="text-green-500" :stroke-width="2" :animate-on-hover="false" />
            </button>
            <a
              :href="authUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-primary btn-icon shrink-0 px-0"
              :title="t('admin.providers.oauth.openAuthUrl')"
              :aria-label="t('admin.providers.oauth.openAuthUrl')"
            >
              <Icon name="externalLink" size="sm" />
            </a>
          </div>
          <button
            type="button"
            :disabled="loading"
            class="inline-flex items-center gap-1 text-xs text-primary-600 hover:text-primary-700 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-500"
            @click="handleRegenerate"
          >
            <Icon name="refresh" size="xs" />
            {{ t('admin.providers.oauth.regenerate') }}
          </button>
        </div>
      </OAuthStep>

      <OAuthStep :index="2" :title="oauthStep2OpenUrl" :description="oauthOpenUrlDesc" :done="hasAuthCode">
        <!-- 平台相关的回调说明与代理提示 -->
        <SettingsNotice v-if="showLocalCallbackNotice || oauthImportantNotice" tone="warning">
          <p v-text="oauthImportantNotice"></p>
        </SettingsNotice>
        <SettingsNotice v-if="showProxyWarning" tone="warning">
          <p v-text="t('admin.providers.oauth.proxyWarning')"></p>
        </SettingsNotice>
      </OAuthStep>

      <OAuthStep :index="3" :title="oauthStep3EnterCode" :description="oauthAuthCodeDesc" :done="hasAuthCode" last>
        <div>
          <div class="mb-1.5 flex items-center gap-2">
            <label :for="`${uid}-auth-code`" class="input-label mb-0">{{ oauthAuthCode }}</label>
            <span
              v-if="isOpenAI && parsedAuthCodeLineCount > 1"
              class="rounded-full bg-primary-50 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300"
            >
              {{ t('admin.providers.oauth.keysCount', { count: parsedAuthCodeLineCount }) }}
            </span>
          </div>
          <textarea
            :id="`${uid}-auth-code`"
            v-model="authCodeInput"
            :rows="isOpenAI ? 5 : 3"
            :class="['input font-mono text-sm', isOpenAI ? 'resize-y' : 'resize-none']"
            :placeholder="oauthAuthCodePlaceholder"
          ></textarea>
          <p v-if="isOpenAI && parsedAuthCodeLineCount > 1" class="input-hint">
            {{ t('admin.providers.oauth.batchCreateProviders', { count: parsedAuthCodeLineCount }) }}
          </p>
          <p class="input-hint">{{ oauthAuthCodeHint }}</p>
        </div>

        <!-- Gemini 回调必须保留 state 参数 -->
        <SettingsNotice v-if="platform === 'gemini'" tone="warning">
          <p class="font-medium">{{ t('admin.providers.oauth.gemini.stateWarningTitle') }}</p>
          <p>{{ t('admin.providers.oauth.gemini.stateWarningDesc') }}</p>
        </SettingsNotice>

        <SettingsNotice v-if="error" tone="error">
          <p class="whitespace-pre-line">{{ error }}</p>
        </SettingsNotice>
      </OAuthStep>
    </ol>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import Icon from '@/components/icons/Icon.vue'
import Collapse from '@/components/common/Collapse.vue'
import Select from '@/components/common/Select.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import SettingsNotice from '@/components/common/settings/SettingsNotice.vue'
import SettingsSegmented from '@/components/common/settings/SettingsSegmented.vue'
import OAuthCredentialImport from '@/components/provider/form/OAuthCredentialImport.vue'
import OAuthStep from '@/components/provider/form/OAuthStep.vue'
import { platformBadgeLightClass } from '@/utils/platformColors'
import type { AddMethod, AuthInputMethod } from '@/composables/useProviderOAuth'
import type { OpenAIOAuthSession } from '@/composables/useOpenAIOAuth'
import type { ProviderPlatform } from '@/types'

interface Props {
  addMethod: AddMethod
  authUrl?: string
  sessionId?: string
  loading?: boolean
  error?: string
  showHelp?: boolean
  showProxyWarning?: boolean
  allowMultiple?: boolean
  methodLabel?: string
  showCookieOption?: boolean // Whether to show cookie auto-auth option
  showRefreshTokenOption?: boolean // Whether to show refresh token input option (OpenAI only)
  showMobileRefreshTokenOption?: boolean // Whether to show mobile refresh token option (OpenAI only)
  showSessionTokenOption?: boolean
  showAccessTokenOption?: boolean
  showCodexSessionImportOption?: boolean
  showAgentIdentityOption?: boolean
  showCodexPatOption?: boolean
  showSsoOption?: boolean
  showManualOption?: boolean
  initialInputMethod?: AuthInputMethod
  platform?: ProviderPlatform // Platform type for different UI/text
  showProjectId?: boolean // New prop to control project ID visibility
  authSessions?: OpenAIOAuthSession[]
}

const props = withDefaults(defineProps<Props>(), {
  authUrl: '',
  sessionId: '',
  loading: false,
  error: '',
  showHelp: true,
  showProxyWarning: true,
  allowMultiple: false,
  methodLabel: '',
  showCookieOption: true,
  showRefreshTokenOption: false,
  showMobileRefreshTokenOption: false,
  showSessionTokenOption: false,
  showAccessTokenOption: false,
  showCodexSessionImportOption: false,
  showAgentIdentityOption: false,
  showCodexPatOption: false,
  showSsoOption: false,
  showManualOption: true,
  initialInputMethod: 'manual',
  platform: 'anthropic',
  showProjectId: true,
  authSessions: () => []
})

const emit = defineEmits<{
  'generate-url': []
  'remove-auth-session': [sessionId: string]
  'exchange-code': [code: string]
  'cookie-auth': [sessionKey: string]
  'validate-refresh-token': [refreshToken: string]
  'validate-mobile-refresh-token': [refreshToken: string]
  'validate-session-token': [sessionToken: string]
  'import-access-token': [accessToken: string]
  'import-codex-session': [content: string]
  'import-codex-pat': [accessToken: string]
  'import-sso': [content: string]
  'update:inputMethod': [method: AuthInputMethod]
}>()

const { t } = useI18n()

const isOpenAI = computed(() => props.platform === 'openai')
const isOpenAIBatchAuth = computed(() => props.authSessions.length > 0)
const openAIAuthSessions = computed(() => {
  if (props.authSessions.length > 0) {
    return props.authSessions
  }
  if (!props.authUrl || !props.sessionId) {
    return []
  }
  return [{
    authUrl: props.authUrl,
    sessionId: props.sessionId,
    state: ''
  }]
})
const showLocalCallbackNotice = computed(() => props.platform === 'openai' || props.platform === 'grok')

// Get translation key based on platform
const getOAuthKey = (key: string) => {
  if (props.platform === 'openai') return `admin.providers.oauth.openai.${key}`
  if (props.platform === 'gemini') return `admin.providers.oauth.gemini.${key}`
  if (props.platform === 'antigravity') return `admin.providers.oauth.antigravity.${key}`
  if (props.platform === 'qoder') return `admin.providers.oauth.qoder.${key}`
  if (props.platform === 'grok') return `admin.providers.oauth.grok.${key}`
  return `admin.providers.oauth.${key}`
}

// Computed translations for current platform
const oauthTitle = computed(() => t(getOAuthKey('title')))
const oauthFollowSteps = computed(() => t(getOAuthKey('followSteps')))
const oauthStep1GenerateUrl = computed(() => t(getOAuthKey('step1GenerateUrl')))
const oauthGenerateAuthUrl = computed(() => t(getOAuthKey('generateAuthUrl')))
const oauthStep2OpenUrl = computed(() => t(getOAuthKey('step2OpenUrl')))
const oauthOpenUrlDesc = computed(() => t(getOAuthKey('openUrlDesc')))
const oauthStep3EnterCode = computed(() => t(getOAuthKey('step3EnterCode')))
const oauthAuthCodeDesc = computed(() => t(getOAuthKey('authCodeDesc')))
const oauthAuthCode = computed(() => t(getOAuthKey('authCode')))
const oauthAuthCodePlaceholder = computed(() => t(getOAuthKey('authCodePlaceholder')))
const oauthAuthCodeHint = computed(() => t(getOAuthKey('authCodeHint')))
const oauthImportantNotice = computed(() => {
  if (props.platform === 'openai') return t('admin.providers.oauth.openai.importantNotice')
  if (props.platform === 'antigravity') return t('admin.providers.oauth.antigravity.importantNotice')
  if (props.platform === 'qoder') return t('admin.providers.oauth.qoder.importantNotice')
  if (props.platform === 'grok') return t('admin.providers.oauth.grok.importantNotice')
  return ''
})

// Local state
const inputMethod = ref<AuthInputMethod>(props.initialInputMethod)
const isAgentIdentityInput = computed(() => inputMethod.value === 'agent_identity')
const authCodeInput = ref('')
const sessionKeyInput = ref('')
const refreshTokenInput = ref('')
const sessionTokenInput = ref('')
const codexSessionInput = ref('')
const codexPATInput = ref('')
const ssoCookieInput = ref('')
const showHelpDialog = ref(false)
const oauthState = ref('')
const projectId = ref('')

// 仅在存在多个输入方式时显示方式选择区。
const methodOptionCount = computed(() => [
  props.showManualOption,
  props.showCookieOption,
  props.showRefreshTokenOption,
  props.showMobileRefreshTokenOption,
  props.showSessionTokenOption,
  props.showAccessTokenOption,
  props.showCodexSessionImportOption,
  props.showAgentIdentityOption,
  props.showCodexPatOption,
  props.showSsoOption
].filter(Boolean).length)
const showMethodSelection = computed(() => methodOptionCount.value > 1)

// 授权方式按调用方开放的选项生成，顺序与旧版单选框一致。
const methodOptions = computed(() => {
  const options: Array<{ value: AuthInputMethod; label: string; show: boolean }> = [
    { value: 'manual', label: t('admin.providers.oauth.manualAuth'), show: props.showManualOption },
    { value: 'cookie', label: t('admin.providers.oauth.cookieAutoAuth'), show: props.showCookieOption },
    { value: 'refresh_token', label: t(getOAuthKey('refreshTokenAuth')), show: props.showRefreshTokenOption },
    { value: 'sso_cookie', label: t(getOAuthKey('ssoCookieAuth')), show: props.showSsoOption },
    { value: 'mobile_refresh_token', label: t('admin.providers.oauth.openai.mobileRefreshTokenAuth', '手动输入 Mobile RT'), show: props.showMobileRefreshTokenOption },
    { value: 'session_token', label: t(getOAuthKey('sessionTokenAuth')), show: props.showSessionTokenOption },
    { value: 'access_token', label: t('admin.providers.oauth.openai.accessTokenAuth', '手动输入 AT'), show: props.showAccessTokenOption },
    { value: 'codex_session', label: t('admin.providers.oauth.openai.codexSessionAuth'), show: props.showCodexSessionImportOption },
    { value: 'agent_identity', label: t('admin.providers.oauth.openai.agentIdentityAuth'), show: props.showAgentIdentityOption },
    { value: 'codex_pat', label: t('admin.providers.oauth.openai.codexPatAuth'), show: props.showCodexPatOption },
  ]
  return options
    .filter(option => option.show)
    .map(option => ({ value: option.value, label: option.label, testid: `oauth-method-${option.value}` }))
})

const uid = useId()
// 步骤完成状态控制视觉提示。
const hasGeneratedUrl = computed(() => !!props.authUrl || props.authSessions.length > 0)
const hasAuthCode = computed(() => authCodeInput.value.trim() !== '')

// Clipboard
const { copied, copyToClipboard } = useClipboard()

// Computed
const parsedKeyCount = computed(() => {
  return sessionKeyInput.value
    .split('\n')
    .map((k) => k.trim())
    .filter((k) => k).length
})

// Computed: count of refresh tokens entered
const parsedRefreshTokenCount = computed(() => {
  return refreshTokenInput.value
    .split('\n')
    .map((rt) => rt.trim())
    .filter((rt) => rt).length
})

const parsedAuthCodeLineCount = computed(() => {
  return authCodeInput.value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line).length
})

const parsedCodexSessionCount = computed(() => {
  const trimmed = codexSessionInput.value.trim()
  if (!trimmed) return 0
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) return 1
  return trimmed
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item).length
})

const extractCallbackParam = (value: string, param: 'code' | 'state') => {
  try {
    const url = new URL(value)
    const fromSearch = url.searchParams.get(param)
    if (fromSearch) return fromSearch.trim()
    if (url.hash) {
      return new URLSearchParams(url.hash.replace(/^#/, '')).get(param)?.trim() || ''
    }
  } catch {
    const match = value.match(new RegExp(`(?:^|[?#&])${param}=([^&#\\s]+)`))
    if (!match?.[1]) return ''
    try {
      return decodeURIComponent(match[1].replace(/\+/g, ' ')).trim()
    } catch {
      return match[1].trim()
    }
  }
  return ''
}

const shouldNormalizeSingleCallbackInput = computed(() => {
  return props.platform !== 'openai' || !isOpenAIBatchAuth.value
})

const parsedSSOCount = computed(() => {
  return ssoCookieInput.value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item).length
})

// Watchers
watch(() => props.initialInputMethod, (newVal) => {
  inputMethod.value = newVal
})

watch(inputMethod, (newVal) => {
  emit('update:inputMethod', newVal)
})

// 从单条回调链接自动提取授权码，OpenAI 批量导入保留多行原始输入给父组件匹配 state。
// 例如：http://localhost:8085/callback?code=xxx...&state=...
watch(authCodeInput, (newVal) => {
  if (!shouldNormalizeSingleCallbackInput.value) return

  const trimmed = newVal.trim()
  if (!trimmed || !/(?:^|[?#&])(?:code|state)=/.test(trimmed)) return

  const stateParam = extractCallbackParam(trimmed, 'state')
  if (stateParam) {
    oauthState.value = stateParam
  }
  const code = extractCallbackParam(trimmed, 'code')
  if (code && code !== trimmed) {
    authCodeInput.value = code
  }
})

// Methods
const handleGenerateUrl = () => {
  emit('generate-url')
}

const handleCopyUrl = () => {
  if (props.authUrl) {
    copyToClipboard(props.authUrl, 'URL copied to clipboard')
  }
}

const handleCopySessionUrl = (urlValue: string) => {
  copyToClipboard(urlValue, 'URL copied to clipboard')
}

const handleRemoveAuthSession = (targetSessionId: string) => {
  emit('remove-auth-session', targetSessionId)
}

const handleRegenerate = () => {
  authCodeInput.value = ''
  oauthState.value = ''
  emit('generate-url')
}

const handleCookieAuth = () => {
  if (sessionKeyInput.value.trim()) {
    emit('cookie-auth', sessionKeyInput.value)
  }
}

const handleValidateRefreshToken = () => {
  if (refreshTokenInput.value.trim()) {
    if (inputMethod.value === 'mobile_refresh_token') {
      emit('validate-mobile-refresh-token', refreshTokenInput.value.trim())
    } else {
      emit('validate-refresh-token', refreshTokenInput.value.trim())
    }
  }
}

const handleImportCodexSession = () => {
  if (codexSessionInput.value.trim()) {
    emit('import-codex-session', codexSessionInput.value.trim())
  }
}

const handleImportCodexPAT = () => {
  if (codexPATInput.value.trim()) {
    emit('import-codex-pat', codexPATInput.value.trim())
  }
}

const handleImportSSO = () => {
  if (ssoCookieInput.value.trim()) {
    emit('import-sso', ssoCookieInput.value.trim())
  }
}

// Expose methods and state
defineExpose({
  authCode: authCodeInput,
  oauthState,
  projectId,
  sessionKey: sessionKeyInput,
  refreshToken: refreshTokenInput,
  sessionToken: sessionTokenInput,
  codexSession: codexSessionInput,
  codexPAT: codexPATInput,
  ssoCookie: ssoCookieInput,
  inputMethod,
  reset: () => {
    authCodeInput.value = ''
    oauthState.value = ''
    projectId.value = ''
    sessionKeyInput.value = ''
    refreshTokenInput.value = ''
    sessionTokenInput.value = ''
    codexSessionInput.value = ''
    codexPATInput.value = ''
    ssoCookieInput.value = ''
    inputMethod.value = props.initialInputMethod
    showHelpDialog.value = false
  }
})
</script>
