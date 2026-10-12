<template>
  <BaseDialog
    :show="show"
    :title="t('admin.providers.bulkEdit.title')"
    width="wide"
    :body-scroll="false"
    @close="handleClose"
  >
    <form
      id="bulk-edit-provider-form"
      novalidate
      class="flex min-h-0 flex-1 flex-col"
      @submit.prevent="() => handleSubmit()"
    >
      <div class="shrink-0 space-y-2 pb-2">
        <SettingsNotice>
          {{ t('admin.providers.bulkEdit.selectionInfo', { count: targetMode === 'filtered' ? targetPreviewCount : providerIds.length }) }}
        </SettingsNotice>
        <SettingsNotice v-if="isMixedPlatform" tone="warning">
          {{ t('admin.providers.bulkEdit.mixedPlatformWarning', { platforms: targetSelectedPlatforms.join(', ') }) }}
        </SettingsNotice>
      </div>

      <SettingsTabs
        id-prefix="bulk-edit-provider"
        :tabs="formTabs"
        :label="t('admin.providers.tabs.label')"
      >
        <template #basic>
          <BulkApplyField id="bulk-edit-status" v-model="enableStatus" :label="t('common.status')">
            <Select
              v-model="status"
              :options="statusOptions"
              aria-labelledby="bulk-edit-status-label"
            />
          </BulkApplyField>

          <BulkApplyField id="bulk-edit-groups" v-model="enableGroups" :label="t('nav.groups')">
            <GroupSelector
              v-model="groupIds"
              :groups="groups"
              aria-labelledby="bulk-edit-groups-label"
            />
          </BulkApplyField>

          <BulkApplyField id="bulk-edit-proxy" v-model="enableProxy" :label="t('admin.providers.proxy')">
            <ProxySelector
              v-model="proxyId"
              :proxies="proxies"
              aria-labelledby="bulk-edit-proxy-label"
            />
          </BulkApplyField>

          <!-- Base URL 只对 API Key、上游中转和 Grok OAuth 账号生效。 -->
          <BulkApplyField
            v-if="allBaseUrlCapable"
            id="bulk-edit-base-url"
            v-model="enableBaseUrl"
            :label="t('admin.providers.baseUrl')"
            :hint="t('admin.providers.bulkEdit.baseUrlNotice')"
          >
            <input
              id="bulk-edit-base-url"
              v-model="baseUrl"
              type="text"
              class="input"
              :placeholder="t('admin.providers.bulkEdit.baseUrlPlaceholder')"
              aria-labelledby="bulk-edit-base-url-label"
            />
            <GrokBaseUrlPresets
              v-if="allTargetsGrok"
              @select="baseUrl = $event; enableBaseUrl = true"
            />
          </BulkApplyField>
        </template>

        <template #models>
          <BulkApplyField
            id="bulk-edit-model-restriction"
            v-model="enableModelRestriction"
            :label="t('admin.providers.modelRestriction')"
            :hint="t('admin.providers.modelRestrictionCombinedHint')"
          >
            <SettingsSegmented
              v-model="modelRestrictionMode"
              block
              :ariaLabel="t('admin.providers.modelRestriction')"
              :options="modelRestrictionModeOptions"
            />
            <div v-if="modelRestrictionMode === 'whitelist'" v-content-reveal class="space-y-2">
              <SettingsNotice>{{ t('admin.providers.selectAllowedModels') }}</SettingsNotice>
              <ModelWhitelistSelector
                v-model="allowedModels"
                :platforms="targetSelectedPlatforms"
              />
              <p class="input-hint">
                {{ t('admin.providers.selectedModels', { count: allowedModels.length }) }}
                <span v-if="allowedModels.length === 0">{{ t('admin.providers.supportsAllModels') }}</span>
              </p>
            </div>
            <ProviderModelMappingEditor
              v-else
              v-model="modelMappings"
            />
          </BulkApplyField>
        </template>

        <template #scheduling>
          <SettingsSection :title="t('admin.providers.sections.scheduling')">
            <div class="grid gap-4 md:grid-cols-2">
              <BulkApplyField id="bulk-edit-concurrency" v-model="enableConcurrency" :label="t('admin.providers.concurrency')" plain>
                <input
                  id="bulk-edit-concurrency"
                  v-model.number="concurrency"
                  type="number"
                  min="1"
                  class="input"
                  aria-labelledby="bulk-edit-concurrency-label"
                  @input="concurrency = Math.max(1, concurrency || 1)"
                />
              </BulkApplyField>
              <BulkApplyField id="bulk-edit-load-factor" v-model="enableLoadFactor" :label="t('admin.providers.loadFactor')" plain>
                <input
                  id="bulk-edit-load-factor"
                  v-model.number="loadFactor"
                  type="number"
                  min="1"
                  class="input"
                  aria-labelledby="bulk-edit-load-factor-label"
                  @input="loadFactor = (loadFactor && loadFactor >= 1) ? loadFactor : null"
                />
                <p class="input-hint">{{ t('admin.providers.loadFactorHint') }}</p>
              </BulkApplyField>
              <BulkApplyField id="bulk-edit-priority" v-model="enablePriority" :label="t('admin.providers.priority')" plain>
                <input
                  id="bulk-edit-priority"
                  v-model.number="priority"
                  type="number"
                  min="1"
                  class="input"
                  aria-labelledby="bulk-edit-priority-label"
                />
                <p class="input-hint">{{ t('admin.providers.priorityHint') }}</p>
              </BulkApplyField>
              <BulkApplyField id="bulk-edit-rate-multiplier" v-model="enableRateMultiplier" :label="t('admin.providers.billingRateMultiplier')" plain>
                <input
                  id="bulk-edit-rate-multiplier"
                  v-model.number="rateMultiplier"
                  type="number"
                  min="0"
                  step="0.001"
                  class="input"
                  aria-labelledby="bulk-edit-rate-multiplier-label"
                />
                <p class="input-hint">{{ t('admin.providers.billingRateMultiplierHint') }}</p>
              </BulkApplyField>
            </div>
          </SettingsSection>

          <BulkApplyField
            id="bulk-edit-custom-error-codes"
            v-model="enableCustomErrorCodes"
            :label="t('admin.providers.customErrorCodes')"
            :hint="t('admin.providers.customErrorCodesHint')"
          >
            <CustomErrorCodesFields v-model:codes="selectedErrorCodes" hide-toggle />
          </BulkApplyField>

          <!-- 预热请求拦截只对 Anthropic 与 Antigravity 生效。 -->
          <BulkApplyField
            v-if="allInterceptWarmupCapable"
            id="bulk-edit-intercept-warmup"
            v-model="enableInterceptWarmup"
            :label="t('admin.providers.interceptWarmupRequests')"
            :hint="t('admin.providers.interceptWarmupRequestsDesc')"
          >
            <template #control>
              <Toggle
                id="bulk-edit-intercept-warmup-toggle"
                v-model="interceptWarmupRequests"
                :aria-label="t('admin.providers.interceptWarmupRequests')"
              />
            </template>
          </BulkApplyField>

          <SettingsSection
            v-if="allOpenAIOAuth"
            :title="t('admin.providers.sections.autoPause')"
            :hint="t('admin.providers.autoPauseThresholdHint')"
          >
            <div class="grid gap-4 md:grid-cols-2">
              <BulkApplyField
                id="bulk-edit-openai-auto-pause-5h-disabled"
                v-model="enableAutoPause5hDisabled"
                :label="t('admin.providers.autoPause5hDisabled')"
                :hint="t('admin.providers.autoPauseDisabledHint')"
                plain
              >
                <template #control>
                  <Toggle
                    id="bulk-edit-openai-auto-pause-5h-disabled-toggle"
                    v-model="autoPause5hDisabled"
                    :aria-label="t('admin.providers.autoPause5hDisabled')"
                  />
                </template>
              </BulkApplyField>
              <BulkApplyField
                id="bulk-edit-openai-auto-pause-7d-disabled"
                v-model="enableAutoPause7dDisabled"
                :label="t('admin.providers.autoPause7dDisabled')"
                :hint="t('admin.providers.autoPauseDisabledHint')"
                plain
              >
                <template #control>
                  <Toggle
                    id="bulk-edit-openai-auto-pause-7d-disabled-toggle"
                    v-model="autoPause7dDisabled"
                    :aria-label="t('admin.providers.autoPause7dDisabled')"
                  />
                </template>
              </BulkApplyField>
              <BulkApplyField
                id="bulk-edit-openai-auto-pause-5h-threshold"
                v-model="enableAutoPause5hThreshold"
                :label="t('admin.providers.autoPause5hThreshold')"
                plain
              >
                <input
                  id="bulk-edit-openai-auto-pause-5h-threshold"
                  v-model.number="autoPause5hThreshold"
                  type="number"
                  min="0"
                  max="100"
                  step="0.1"
                  class="input"
                  aria-labelledby="bulk-edit-openai-auto-pause-5h-threshold-label"
                  @input="autoPause5hThreshold = normalizeAutoPauseThresholdInput(autoPause5hThreshold)"
                />
              </BulkApplyField>
              <BulkApplyField
                id="bulk-edit-openai-auto-pause-7d-threshold"
                v-model="enableAutoPause7dThreshold"
                :label="t('admin.providers.autoPause7dThreshold')"
                plain
              >
                <input
                  id="bulk-edit-openai-auto-pause-7d-threshold"
                  v-model.number="autoPause7dThreshold"
                  type="number"
                  min="0"
                  max="100"
                  step="0.1"
                  class="input"
                  aria-labelledby="bulk-edit-openai-auto-pause-7d-threshold-label"
                  @input="autoPause7dThreshold = normalizeAutoPauseThresholdInput(autoPause7dThreshold)"
                />
              </BulkApplyField>
            </div>
          </SettingsSection>
        </template>

        <template #quota>
          <!-- 用户消息限速随 RPM 限制一起应用，未勾选时不提交。 -->
          <BulkApplyField
            v-if="allAnthropicOAuthOrSetupToken"
            id="bulk-edit-rpm-limit"
            v-model="enableRpmLimit"
            :label="t('admin.providers.quotaControl.rpmLimit.label')"
            :hint="t('admin.providers.quotaControl.rpmLimit.hint')"
          >
            <template #control>
              <Toggle
                id="bulk-edit-rpm-limit-toggle"
                v-model="rpmLimitEnabled"
                :aria-label="t('admin.providers.quotaControl.rpmLimit.label')"
              />
            </template>
            <Collapse :open="rpmLimitEnabled" unmount-on-hide>
              <RpmLimitPanel
                v-model:base-rpm="bulkBaseRpm"
                v-model:strategy="bulkRpmStrategy"
                v-model:sticky-buffer="bulkRpmStickyBuffer"
              />
            </Collapse>
            <div class="space-y-2">
              <div>
                <span class="text-sm font-medium text-primary-900 dark:text-dark-50">{{ t('admin.providers.quotaControl.rpmLimit.userMsgQueue') }}</span>
                <p class="input-hint">{{ t('admin.providers.quotaControl.rpmLimit.userMsgQueueHint') }}</p>
              </div>
              <SettingsSegmented
                v-model="userMsgQueueMode"
                deselectable
                :ariaLabel="t('admin.providers.quotaControl.rpmLimit.userMsgQueue')"
                :options="umqModeOptions"
              />
            </div>
          </BulkApplyField>
        </template>

        <template #request>
          <!-- 协议选择器挂载时按目录初始化原生集合，因此勾选应用后再创建。 -->
          <BulkApplyField
            v-if="targetSelectedPlatforms.length === 1 && targetSelectedTypes.length === 1"
            id="bulk-native-protocols"
            v-model="enableUpstreamProtocols"
            :label="t('admin.protocols.nativeTitle')"
          >
            <ProviderProtocolSelector
              v-if="enableUpstreamProtocols"
              v-model="upstreamProtocols"
              :platform="targetSelectedPlatforms[0] ?? ''"
              :type="targetSelectedTypes[0] ?? ''"
              auth-mode="*"
              hide-title
            />
          </BulkApplyField>

          <BulkApplyField
            v-if="allHeaderOverrideCapable"
            id="bulk-edit-header-override"
            v-model="enableHeaderOverride"
            :label="t('admin.providers.headerOverride.title')"
            :hint="t('admin.providers.headerOverride.hint')"
          >
            <template #control>
              <Toggle
                id="bulk-edit-header-override-toggle"
                v-model="headerOverrideEnabled"
                :aria-label="t('admin.providers.headerOverride.title')"
              />
            </template>
            <template v-if="headerOverrideEnabled">
              <SettingsNotice>
                <p>{{ t('admin.providers.headerOverride.info') }}</p>
              </SettingsNotice>
              <SettingsNotice tone="warning">{{ t('admin.providers.headerOverride.bulkReplaceHint') }}</SettingsNotice>
              <HeaderOverrideEditor
                :rows="headerOverrideRows"
                @update:rows="headerOverrideRows = $event"
              />
            </template>
            <p v-else class="input-hint">{{ t('admin.providers.headerOverride.bulkDisableHint') }}</p>
          </BulkApplyField>

          <template v-if="allOpenAIPassthroughCapable">
            <BulkApplyField
              id="bulk-edit-openai-passthrough"
              v-model="enableOpenAIPassthrough"
              :label="t('admin.providers.openai.oauthPassthrough')"
              :hint="t('admin.providers.openai.oauthPassthroughDesc')"
            >
              <template #control>
                <Toggle
                  id="bulk-edit-openai-passthrough-toggle"
                  v-model="openaiPassthroughEnabled"
                  :aria-label="t('admin.providers.openai.oauthPassthrough')"
                />
              </template>
            </BulkApplyField>
          </template>

          <BulkApplyField
            v-if="allOpenAIOAuth"
            id="bulk-edit-openai-flatten-namespaces"
            v-model="enableOpenAIFlattenNamespaces"
            :label="t('admin.providers.openai.flattenNamespaces')"
            :hint="t('admin.providers.openai.flattenNamespacesDesc')"
          >
            <template #control>
              <Toggle
                id="bulk-edit-openai-flatten-namespaces-toggle"
                v-model="openaiFlattenNamespacesEnabled"
                :aria-label="t('admin.providers.openai.flattenNamespaces')"
              />
            </template>
          </BulkApplyField>

          <BulkApplyField
            v-if="allOpenAIAPIKey"
            id="bulk-edit-openai-continuation-supported"
            v-model="enableOpenAIResponsesContinuationSupported"
            :label="t('admin.providers.openai.responsesContinuationSupported')"
            :hint="t('admin.providers.openai.responsesContinuationSupportedDesc')"
            apply-testid="bulk-edit-openai-continuation-supported-apply"
          >
            <template #control>
              <Toggle
                v-model="openAIResponsesContinuationSupported"
                data-testid="bulk-edit-openai-continuation-supported"
                :aria-label="t('admin.providers.openai.responsesContinuationSupportedEnabled')"
              />
            </template>
          </BulkApplyField>

          <BulkApplyField
            v-if="allOpenAIOAuth"
            id="bulk-edit-openai-ws-mode"
            v-model="enableResponsesWSConnectionMode"
            :label="t('admin.providers.openai.wsMode')"
            :hint="`${t('admin.providers.openai.wsModeDesc')} ${t(openAIWSModeConcurrencyHintKey)}`"
          >
            <Select
              v-model="openaiOAuthResponsesWebSocketV2Mode"
              data-testid="bulk-edit-openai-ws-mode-select"
              :options="openAIWSModeOptions"
              aria-labelledby="bulk-edit-openai-ws-mode-label"
            />
          </BulkApplyField>

          <BulkApplyField
            v-if="allOpenAIAPIKey"
            id="bulk-edit-openai-apikey-ws-mode"
            v-model="enableOpenAIAPIKeyWSMode"
            :label="t('admin.providers.openai.wsMode')"
            :hint="`${t('admin.providers.openai.wsModeDesc')} ${t(openAIAPIKeyWSModeConcurrencyHintKey)}`"
          >
            <Select
              v-model="openaiAPIKeyResponsesWebSocketV2Mode"
              data-testid="bulk-edit-openai-apikey-ws-mode-select"
              :options="openAIWSModeOptions"
              aria-labelledby="bulk-edit-openai-apikey-ws-mode-label"
            />
          </BulkApplyField>

          <template v-if="allOpenAIOAuth">
            <BulkApplyField
              id="bulk-edit-openai-codex-cli-only"
              v-model="enableCodexCLIOnly"
              :label="t('admin.providers.openai.clientPolicy')"
              :hint="t('admin.providers.openai.clientPolicyDesc')"
            >
              <Select
                v-model="openAIOAuthClientPolicy"
                data-testid="bulk-edit-openai-client-policy-select"
                :options="openAIOAuthClientPolicyOptions"
                aria-labelledby="bulk-edit-openai-codex-cli-only-label"
              />
            </BulkApplyField>

            <!-- 同时修改客户端策略且不是“仅 Codex”时，放行 Claude Code 没有意义，保持锁定。 -->
            <BulkApplyField
              id="bulk-edit-openai-codex-allow-claude-code"
              v-model="enableCodexCLIOnlyAllowClaudeCode"
              :label="t('admin.providers.openai.codexCLIOnlyAllowClaudeCode')"
              :hint="codexAllowClaudeCodeLocked
                ? `${t('admin.providers.openai.codexCLIOnlyAllowClaudeCodeDesc')} ${t('admin.providers.openai.clientPolicyClaudeCodeHint')}`
                : t('admin.providers.openai.codexCLIOnlyAllowClaudeCodeDesc')"
              :locked="codexAllowClaudeCodeLocked"
            >
              <template #control>
                <Toggle
                  id="bulk-edit-openai-codex-allow-claude-code-toggle"
                  v-model="codexCLIOnlyAllowClaudeCodeEnabled"
                  :aria-label="t('admin.providers.openai.codexCLIOnlyAllowClaudeCode')"
                />
              </template>
            </BulkApplyField>

            <BulkApplyField
              id="bulk-edit-codex-fingerprint-mode"
              v-model="enableCodexFingerprintMode"
              :label="t('admin.providers.openai.codexFingerprintMode')"
              :hint="t('admin.providers.openai.codexFingerprintModeDesc')"
            >
              <Select
                v-model="codexFingerprintMode"
                data-testid="bulk-codex-fingerprint-mode-select"
                :options="codexFingerprintModeOptions"
                aria-labelledby="bulk-edit-codex-fingerprint-mode-label"
              />
            </BulkApplyField>
          </template>

          <template v-if="allOpenAIPassthroughCapable">
            <BulkApplyField
              id="bulk-edit-codex-image-tool"
              v-model="enableCodexImageToolMode"
              :label="t('admin.protocols.imagePolicy')"
              :hint="t('admin.providers.openai.codexImageToolDesc')"
            >
              <CodexImageToolModeSelector
                v-model="codexImageToolMode"
                test-id-prefix="bulk-edit-codex-image-tool"
                hide-title
              />
            </BulkApplyField>

            <BulkApplyField
              id="bulk-edit-openai-native-compaction-v2-mode"
              v-model="enableOpenAINativeCompactionV2Mode"
              :label="t('admin.providers.openai.nativeCompactV2Mode')"
              :hint="t('admin.providers.openai.nativeCompactV2ModeDesc')"
            >
              <template #control>
                <Toggle
                  :model-value="openAINativeCompactionV2Mode === 'force_on'"
                  data-testid="bulk-edit-openai-native-compaction-v2-mode-select"
                  :aria-label="t('admin.providers.openai.nativeCompactV2Mode')"
                  @update:model-value="openAINativeCompactionV2Mode = $event ? 'force_on' : 'force_off'"
                />
              </template>
            </BulkApplyField>

            <BulkApplyField
              id="bulk-edit-openai-compact-mode"
              v-model="enableOpenAICompactMode"
              :label="t('admin.providers.openai.compactMode')"
              :hint="t('admin.providers.openai.compactModeDesc')"
            >
              <template #control>
                <Toggle
                  :model-value="openAICompactMode === 'force_on'"
                  data-testid="bulk-edit-openai-compact-mode-select"
                  :aria-label="t('admin.providers.openai.compactMode')"
                  @update:model-value="openAICompactMode = $event ? 'force_on' : 'force_off'"
                />
              </template>
            </BulkApplyField>

            <BulkApplyField
              v-if="enableOpenAICompactMode && openAICompactMode !== 'force_off'"
              id="bulk-edit-openai-compact-model-mapping"
              v-model="enableOpenAICompactModelMapping"
              :label="t('admin.providers.openai.compactModelMapping')"
              :hint="t('admin.providers.openai.compactModelMappingDesc')"
            >
              <ProviderModelMappingEditor
                v-model="openAICompactModelMappings"
                :hint="''"
                :source-placeholder="t('admin.providers.fromModel')"
                :target-placeholder="t('admin.providers.toModel')"
                test-id="bulk-edit-openai-compact-model-mapping"
              />
            </BulkApplyField>
          </template>

          <BulkApplyField
            v-if="allTLSFingerprintCapable"
            id="bulk-edit-tls-fingerprint"
            v-model="enableTLSFingerprint"
            :label="t('admin.providers.quotaControl.tlsFingerprint.label')"
            :hint="t('admin.providers.quotaControl.tlsFingerprint.hint')"
          >
            <template #control>
              <Toggle
                id="bulk-edit-tls-fingerprint-toggle"
                v-model="tlsFingerprintEnabled"
                :aria-label="t('admin.providers.quotaControl.tlsFingerprint.label')"
              />
            </template>
            <Collapse :open="tlsFingerprintEnabled" unmount-on-hide>
              <SettingsSubpanel>
                <div>
                  <label for="bulk-edit-tls-fingerprint-profile" class="input-label">{{ t('admin.providers.quotaControl.tlsFingerprint.profile') }}</label>
                  <Select
                    id="bulk-edit-tls-fingerprint-profile"
                    v-model="tlsFingerprintProfileId"
                    data-testid="bulk-edit-tls-fingerprint-profile"
                    :options="tlsFingerprintProfileOptions"
                  />
                </div>
                <div v-if="allOpenAIOAuth">
                  <label for="bulk-edit-tls-fingerprint-router" class="input-label">{{ t('admin.providers.quotaControl.tlsFingerprint.router') }}</label>
                  <Select
                    id="bulk-edit-tls-fingerprint-router"
                    v-model="tlsFingerprintRouterId"
                    data-testid="bulk-edit-tls-fingerprint-router"
                    :options="tlsFingerprintRouterOptions"
                  />
                  <p class="input-hint">{{ t('admin.providers.quotaControl.tlsFingerprint.routerHint') }}</p>
                </div>
              </SettingsSubpanel>
            </Collapse>
          </BulkApplyField>
        </template>
      </SettingsTabs>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="bulk-edit-provider-form"
          :disabled="submitting"
          class="btn btn-primary"
        >
          <Icon
            name="loader"
            size="sm"
            :animate-on-hover="false"
            v-if="submitting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
          />
          {{
            submitting ? t('admin.providers.bulkEdit.updating') : t('admin.providers.bulkEdit.submit')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { vContentReveal } from '@/directives/contentReveal'
import Collapse from '@/components/common/Collapse.vue'

import ProviderProtocolSelector from './ProviderProtocolSelector.vue'
import type { ProtocolID } from '@/types'
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type {
  Proxy as ProxyConfig,
  AdminGroup,
  ProviderPlatform,
  ProviderType,
  Provider,
  OpenAICompactMode,
  OpenAIOAuthClientPolicy,
} from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import CodexImageToolModeSelector from '@/components/provider/CodexImageToolModeSelector.vue'
import ModelWhitelistSelector from '@/components/provider/ModelWhitelistSelector.vue'
import Icon from '@/components/icons/Icon.vue'
import SettingsNotice from '@/components/common/settings/SettingsNotice.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import SettingsSegmented from '@/components/common/settings/SettingsSegmented.vue'
import SettingsSubpanel from '@/components/common/settings/SettingsSubpanel.vue'
import SettingsTabs from '@/components/common/settings/SettingsTabs.vue'
import BulkApplyField from '@/components/provider/form/BulkApplyField.vue'
import CustomErrorCodesFields from '@/components/provider/form/CustomErrorCodesFields.vue'
import RpmLimitPanel from '@/components/provider/form/RpmLimitPanel.vue'
import {
  useCodexFingerprintModeOptions,
  useOpenAIOAuthClientPolicyOptions,
  useResponsesWSConnectionModeOptions,
  useUserMsgQueueModeOptions,
  type CodexFingerprintMode,
  type RpmStrategy
} from '@/components/provider/form/providerFormOptions'
import ProviderModelMappingEditor from '@/components/provider/ProviderModelMappingEditor.vue'
import type { ModelMappingRow } from '@/utils/modelMappingRules'
import {
  buildModelMappingObject,
  buildPersistedModelRestriction,
  normalizeModelWhitelist,
  splitQoderPersistedModelRestriction,
  splitModelMappingObject,
  splitPersistedModelRestriction
} from '@/composables/useModelWhitelist'
import HeaderOverrideEditor from '@/components/provider/HeaderOverrideEditor.vue'
import {
  buildHeaderOverridesObject,
  isHeaderOverrideCapable,
  validateHeaderOverrideRows,
  HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY,
  HEADER_OVERRIDES_CREDENTIAL_KEY,
  type HeaderOverrideRow
} from '@/components/provider/credentialsBuilder'
import GrokBaseUrlPresets from '@/components/provider/GrokBaseUrlPresets.vue'
import {
  RESPONSES_WS_POOLED,
  responsesWSConnectionHint
} from '@/utils/responsesWsConnection'
import type { ResponsesWSConnectionMode } from '@/utils/responsesWsConnection'
import {
  applyCodexImageToolMode,
  type CodexImageToolMode
} from '@/utils/codexImageToolMode'
interface Props {
  show: boolean
  providerIds: number[]
  selectedPlatforms: ProviderPlatform[]
  selectedTypes: ProviderType[]
  target?: {
    mode: 'selected' | 'filtered'
    filters?: Record<string, unknown>
    previewCount?: number
    selectedPlatforms?: ProviderPlatform[]
    selectedTypes?: ProviderType[]
  }
  proxies: ProxyConfig[]
  groups: AdminGroup[]
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  updated: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

// Platform awareness
const targetMode = computed(() => props.target?.mode ?? 'selected')
const targetPreviewCount = computed(() => props.target?.previewCount ?? props.providerIds.length)
const targetSelectedPlatforms = computed(() => props.target?.selectedPlatforms ?? props.selectedPlatforms)
const targetSelectedTypes = computed(() => props.target?.selectedTypes ?? props.selectedTypes)
// 所选提供商全部属于 grok 平台时展示快捷端点。
const allTargetsGrok = computed(
  () =>
    targetSelectedPlatforms.value.length > 0 &&
    targetSelectedPlatforms.value.every((p) => p === 'grok')
)
const isMixedPlatform = computed(() => targetSelectedPlatforms.value.length > 1)

const allOpenAIPassthroughCapable = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'openai' &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(t => t === 'oauth' || t === 'apikey')
  )
})

const allOpenAIOAuth = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'openai' &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(t => t === 'oauth')
  )
})

const allOpenAIAPIKey = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'openai' &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(t => t === 'apikey')
  )
})

// 是否全部为支持请求头覆写的平台/提供商类型
// 所选平台 × 所选类型的全组合均需具备覆写资格（实际选中提供商是该组合的子集，
// 按交叉积判定偏保守但绝不放行不合资格的提供商）
const allHeaderOverrideCapable = computed(() => {
  return (
    targetSelectedPlatforms.value.length > 0 &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedPlatforms.value.every(p =>
      targetSelectedTypes.value.every(ty => isHeaderOverrideCapable(p, ty))
    )
  )
})

// 是否全部为 Anthropic OAuth/SetupToken（RPM 配置仅在此条件下显示）
const allAnthropicOAuthOrSetupToken = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'anthropic' &&
    targetSelectedTypes.value.every(t => t === 'oauth' || t === 'setup-token')
  )
})

const isTLSFingerprintCapableTarget = (platform: ProviderPlatform, type: ProviderType) => {
  // TLS 指纹伪装支持 Anthropic OAuth/SetupToken、OpenAI OAuth 与 Qoder COSY，OpenAI API Key 不开放。
  return (
    (platform === 'anthropic' && (type === 'oauth' || type === 'setup-token')) ||
    (platform === 'openai' && type === 'oauth') ||
    (platform === 'qoder' && type === 'cosy')
  )
}

const allTLSFingerprintCapable = computed(() => {
  const platforms = targetSelectedPlatforms.value
  const types = targetSelectedTypes.value
  if (platforms.length === 0 || types.length === 0) return false
  if (!platforms.every(platform => platform === 'anthropic' || platform === 'openai' || platform === 'qoder')) return false
  if (!types.every(type => type === 'oauth' || type === 'setup-token' || type === 'cosy')) return false
  if (platforms.length === 1) {
    return types.every(type => isTLSFingerprintCapableTarget(platforms[0], type))
  }
  return platforms.every(platform => platform === 'anthropic' || platform === 'openai') &&
    platforms.includes('openai') &&
    platforms.includes('anthropic') &&
    types.every(type => type === 'oauth' || type === 'setup-token')
})

// Base URL 对 API Key 与上游中转账号生效；Grok OAuth 订阅账号也用它改写转发端点。
const allBaseUrlCapable = computed(() => {
  const types = targetSelectedTypes.value
  return types.length > 0 && types.every(type =>
    type === 'apikey' || type === 'upstream' || (type === 'oauth' && allTargetsGrok.value)
  )
})

// 预热请求拦截只在 Anthropic 与 Antigravity 网关中实现。
const allInterceptWarmupCapable = computed(() => {
  const platforms = targetSelectedPlatforms.value
  return platforms.length > 0 && platforms.every(platform => platform === 'anthropic' || platform === 'antigravity')
})

const modelRestrictionModeOptions = computed(() => [
  { value: 'whitelist' as const, label: t('admin.providers.modelWhitelist'), icon: 'checkCircle' as const },
  { value: 'mapping' as const, label: t('admin.providers.modelMapping'), icon: 'swap' as const }
])

// 同时修改客户端策略且新策略不是“仅 Codex”时，Claude Code 放行项不会生效。
const codexAllowClaudeCodeLocked = computed(() =>
  enableCodexCLIOnly.value && openAIOAuthClientPolicy.value !== 'codex_only'
)

// 只展示有可编辑项的页签；模型限制、调度和基本信息对所有目标可用。
const formTabs = computed(() => {
  const hasRequest =
    (targetSelectedPlatforms.value.length === 1 && targetSelectedTypes.value.length === 1) ||
    allHeaderOverrideCapable.value ||
    allOpenAIPassthroughCapable.value ||
    allTLSFingerprintCapable.value
  return [
    { key: 'basic', label: t('admin.providers.tabs.basic') },
    { key: 'models', label: t('admin.providers.tabs.models') },
    { key: 'scheduling', label: t('admin.providers.tabs.scheduling') },
    { key: 'quota', label: t('admin.providers.tabs.quota'), hidden: !allAnthropicOAuthOrSetupToken.value },
    { key: 'request', label: t('admin.providers.tabs.request'), hidden: !hasRequest }
  ]
})

// Model mapping type
type OptionalNumberInputValue = number | null | ''

interface ParsedModelRestrictionState {
  mode: 'whitelist' | 'mapping'
  allowedModels: string[]
  modelMappings: ModelMappingRow[]
}

// State - field enable flags
const enableBaseUrl = ref(false)
const enableModelRestriction = ref(false)
const enableCustomErrorCodes = ref(false)
const enableInterceptWarmup = ref(false)
const enableHeaderOverride = ref(false)
const enableProxy = ref(false)
const enableConcurrency = ref(false)
const enableLoadFactor = ref(false)
const enablePriority = ref(false)
const enableRateMultiplier = ref(false)
const enableStatus = ref(false)
const enableGroups = ref(false)
const enableOpenAIPassthrough = ref(false)
const enableOpenAIFlattenNamespaces = ref(false)
const enableCodexImageToolMode = ref(false)
const enableUpstreamProtocols = ref(false)
const upstreamProtocols = ref<ProtocolID[] | undefined>(undefined)
watch(() => [targetSelectedPlatforms.value.join(','), targetSelectedTypes.value.join(',')], () => { enableUpstreamProtocols.value = false; upstreamProtocols.value = undefined })
const enableOpenAIResponsesContinuationSupported = ref(false)
const enableResponsesWSConnectionMode = ref(false)
const enableOpenAIAPIKeyWSMode = ref(false)
const enableCodexCLIOnly = ref(false)
const enableCodexCLIOnlyAllowClaudeCode = ref(false)
const enableAutoPause5hThreshold = ref(false)
const enableAutoPause7dThreshold = ref(false)
const enableAutoPause5hDisabled = ref(false)
const enableAutoPause7dDisabled = ref(false)
const enableOpenAICompactMode = ref(false)
const enableOpenAINativeCompactionV2Mode = ref(false)
const enableOpenAICompactModelMapping = ref(false)
const enableRpmLimit = ref(false)
const enableTLSFingerprint = ref(false)

// State - field values
const submitting = ref(false)
const baseUrl = ref('')
const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const allowedModels = ref<string[]>([])
const modelMappings = ref<ModelMappingRow[]>([])
const selectedErrorCodes = ref<number[]>([])
const interceptWarmupRequests = ref(false)
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const proxyId = ref<number | null>(null)
const concurrency = ref(1)
const loadFactor = ref<number | null>(null)
const priority = ref(1)
const rateMultiplier = ref(1)
const status = ref<'active' | 'inactive'>('active')
const groupIds = ref<number[]>([])
const openaiPassthroughEnabled = ref(false)
// OpenAI OAuth namespace 工具摊平兼容开关，缺省关闭即原样保留。
const openaiFlattenNamespacesEnabled = ref(false)
const codexImageToolMode = ref<CodexImageToolMode>('inherit')
// 勾选 continuation 后才提交该字段，未勾选时保留提供商的当前设置。
const openAIResponsesContinuationSupported = ref(false)
const openaiOAuthResponsesWebSocketV2Mode = ref<ResponsesWSConnectionMode>(RESPONSES_WS_POOLED)
const openaiAPIKeyResponsesWebSocketV2Mode = ref<ResponsesWSConnectionMode>(RESPONSES_WS_POOLED)
const openAIOAuthClientPolicy = ref<OpenAIOAuthClientPolicy>('any')
const codexCLIOnlyAllowClaudeCodeEnabled = ref(false)
const autoPause5hThreshold = ref<OptionalNumberInputValue>(null)
const autoPause7dThreshold = ref<OptionalNumberInputValue>(null)
const autoPause5hDisabled = ref(false)
const autoPause7dDisabled = ref(false)
const enableCodexFingerprintMode = ref(false)
const codexFingerprintMode = ref<CodexFingerprintMode>('off')
const codexFingerprintModeOptions = useCodexFingerprintModeOptions()
const openAICompactMode = ref<OpenAICompactMode>('force_on')
const openAINativeCompactionV2Mode = ref<OpenAICompactMode>('force_on')
const openAICompactModelMappings = ref<ModelMappingRow[]>([])
const rpmLimitEnabled = ref(false)
const bulkBaseRpm = ref<number | null>(null)
const bulkRpmStrategy = ref<RpmStrategy>('tiered')
const bulkRpmStickyBuffer = ref<number | null>(null)
const userMsgQueueMode = ref<string | null>(null)
const tlsFingerprintEnabled = ref(false)
const tlsFingerprintProfileId = ref(0)
const tlsFingerprintProfiles = ref<{ id: number; name: string }[]>([])
const tlsFingerprintRouterId = ref<number | null>(null)
const tlsFingerprintRouters = ref<{ id: number; name: string }[]>([])
const tlsFingerprintProfileOptions = computed(() => [
  { value: 0, label: t('admin.providers.quotaControl.tlsFingerprint.defaultProfile') },
  ...(tlsFingerprintProfiles.value.length > 0
    ? [{ value: -1, label: t('admin.providers.quotaControl.tlsFingerprint.randomProfile') }]
    : []),
  ...tlsFingerprintProfiles.value.map((profile) => ({ value: profile.id, label: profile.name }))
])
const tlsFingerprintRouterOptions = computed(() => [
  { value: null, label: t('admin.providers.quotaControl.tlsFingerprint.noRouter') },
  ...tlsFingerprintRouters.value.map((router) => ({ value: router.id, label: router.name }))
])
const modelRestrictionPrefillSeq = ref(0)
const umqModeOptions = useUserMsgQueueModeOptions()

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])

const openAIWSModeOptions = useResponsesWSConnectionModeOptions()
const openAIOAuthClientPolicyOptions = useOpenAIOAuthClientPolicyOptions()
const openAIWSModeConcurrencyHintKey = computed(() =>
  responsesWSConnectionHint(openaiOAuthResponsesWebSocketV2Mode.value)
)
const openAIAPIKeyWSModeConcurrencyHintKey = computed(() =>
  responsesWSConnectionHint(openaiAPIKeyResponsesWebSocketV2Mode.value)
)

const cloneModelMappings = (mappings: ModelMappingRow[]) =>
  mappings.map(({ from, to }) => ({ from, to }))

const normalizeModelMappings = (mappings: ModelMappingRow[]) => {
  return cloneModelMappings(mappings)
    .map(({ from, to }) => ({
      from: from.trim(),
      to: to.trim()
    }))
    .filter(({ from, to }) => from.length > 0 && to.length > 0)
    .sort((a, b) => {
      const fromCmp = a.from.localeCompare(b.from)
      if (fromCmp !== 0) {
        return fromCmp
      }
      return a.to.localeCompare(b.to)
    })
}

const resetModelRestrictionDraft = () => {
  modelRestrictionMode.value = 'whitelist'
  allowedModels.value = []
  modelMappings.value = []
}

const isModelRestrictionDraftPristine = () =>
  modelRestrictionMode.value === 'whitelist' &&
  allowedModels.value.length === 0 &&
  modelMappings.value.length === 0

const getModelRestrictionSignature = (state: ParsedModelRestrictionState) => {
  return JSON.stringify({
    mode: state.mode,
    allowedModels: state.allowedModels,
    modelMappings: state.modelMappings
  })
}

const parseProviderModelRestriction = (provider: Provider): ParsedModelRestrictionState => {
  const credentials = (provider.credentials as Record<string, unknown>) || {}

  let allowedModels: string[]
  let modelMappings: ModelMappingRow[] = []

  if (provider.platform === 'antigravity') {
    const rawMapping = credentials.model_mapping as Record<string, string> | undefined
    if (rawMapping && typeof rawMapping === 'object') {
      const parsed = splitModelMappingObject(rawMapping)
      allowedModels = parsed.allowedModels
      modelMappings = parsed.modelMappings
    } else {
      allowedModels = normalizeModelWhitelist(credentials.model_whitelist)
    }
  } else if (provider.platform === 'qoder') {
    const parsed = splitQoderPersistedModelRestriction(
      credentials.model_mapping as Record<string, string> | undefined,
      credentials.model_whitelist
    )
    allowedModels = parsed.allowedModels
    modelMappings = parsed.modelMappings
  } else {
    const parsed = splitPersistedModelRestriction(
      credentials.model_mapping as Record<string, string> | undefined,
      credentials.model_whitelist
    )
    allowedModels = parsed.allowedModels
    modelMappings = parsed.modelMappings
  }

  const normalizedAllowedModels = Array.from(
    new Set(
      allowedModels
        .map((model) => model.trim())
        .filter((model) => model.length > 0)
    )
  ).sort((a, b) => a.localeCompare(b))
  const normalizedModelMappings = normalizeModelMappings(modelMappings)

  return {
    mode: normalizedModelMappings.length > 0 ? 'mapping' : 'whitelist',
    allowedModels: normalizedAllowedModels,
    modelMappings: normalizedModelMappings
  }
}

const hydrateModelRestrictionDraftFromProviders = (providers: Provider[]) => {
  if (props.selectedPlatforms.length !== 1 || providers.length === 0) {
    resetModelRestrictionDraft()
    return
  }

  const parsedStates = providers.map(parseProviderModelRestriction)
  const firstState = parsedStates[0]
  const firstSignature = getModelRestrictionSignature(firstState)

  if (!parsedStates.every((state) => getModelRestrictionSignature(state) === firstSignature)) {
    resetModelRestrictionDraft()
    return
  }

  modelRestrictionMode.value = firstState.mode
  allowedModels.value = [...firstState.allowedModels]
  modelMappings.value = cloneModelMappings(firstState.modelMappings)
}

const loadTLSFingerprintProfiles = async () => {
  try {
    const profiles = await adminAPI.tlsFingerprintProfiles.list()
    tlsFingerprintProfiles.value = profiles.map(p => ({ id: p.id, name: p.name }))
  } catch {
    tlsFingerprintProfiles.value = []
  }
}

const loadTLSFingerprintRouters = async () => {
  try {
    const routers = await adminAPI.tlsFingerprintRouters.list()
    tlsFingerprintRouters.value = routers.map(router => ({ id: router.id, name: router.name }))
  } catch {
    tlsFingerprintRouters.value = []
  }
}

const loadSelectedProviderDefaults = async () => {
  const requestSeq = ++modelRestrictionPrefillSeq.value
  if (!props.show || props.providerIds.length === 0) {
    return
  }
  if (props.selectedPlatforms.length !== 1) {
    resetModelRestrictionDraft()
    return
  }

  try {
    const providers = await Promise.all(props.providerIds.map((id) => adminAPI.providers.getById(id)))
    if (requestSeq !== modelRestrictionPrefillSeq.value || !props.show) {
      return
    }
    if (!isModelRestrictionDraftPristine()) {
      return
    }
    hydrateModelRestrictionDraftFromProviders(providers)
  } catch (error) {
    if (requestSeq !== modelRestrictionPrefillSeq.value) {
      return
    }
    if (!isModelRestrictionDraftPristine()) {
      return
    }
    resetModelRestrictionDraft()
    console.error('Failed to load bulk edit provider defaults:', error)
  }
}

// Model mapping helpers

const buildOpenAICompactModelMapping = (): Record<string, string> | null => {
  return buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
}

const normalizeAutoPauseThresholdInput = (value: OptionalNumberInputValue): OptionalNumberInputValue => {
  if (value === '' || value === null) {
    return null
  }

  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return null
  }
  return Math.min(parsed, 100)
}

const buildAutoPauseThresholdRatio = (value: OptionalNumberInputValue): number => {
  // UI 使用百分比，后端 extra 字段使用 0-1；0 表示清除提供商级覆盖并回退全局默认。
  const normalized = normalizeAutoPauseThresholdInput(value)
  return typeof normalized === 'number' ? normalized / 100 : 0
}

const buildUpdatePayload = (): Record<string, unknown> | null => {
  const updates: Record<string, unknown> = {}
  const credentials: Record<string, unknown> = {}
  let credentialsChanged = false
  const ensureExtra = (): Record<string, unknown> => {
    if (!updates.extra) {
      updates.extra = {}
    }
    return updates.extra as Record<string, unknown>
  }

  if (enableProxy.value) {
    // 后端用 proxy_id: 0 表示清除代理。
    updates.proxy_id = proxyId.value === null ? 0 : proxyId.value
  }

  if (enableConcurrency.value) {
    updates.concurrency = concurrency.value
  }

  if (enableLoadFactor.value) {
    // 空值/NaN/0 时发送 0（后端约定 <= 0 表示清除）
    const lf = loadFactor.value
    updates.load_factor = (lf != null && !Number.isNaN(lf) && lf > 0) ? lf : 0
  }

  if (enablePriority.value) {
    updates.priority = priority.value
  }

  if (enableRateMultiplier.value) {
    updates.rate_multiplier = rateMultiplier.value
  }

  if (enableStatus.value) {
    updates.status = status.value
  }

  if (enableGroups.value) {
    updates.group_ids = groupIds.value
  }

  if (enableBaseUrl.value && allBaseUrlCapable.value) {
    const baseUrlValue = baseUrl.value.trim()
    if (baseUrlValue) {
      credentials.base_url = baseUrlValue
      credentialsChanged = true
    }
  }

  if (enableOpenAIPassthrough.value) {
    const extra = ensureExtra()
    extra.openai_passthrough = openaiPassthroughEnabled.value
    if (!openaiPassthroughEnabled.value) {
      extra.openai_oauth_passthrough = false
    }
  }

  // 可见性也参与校验，防止目标筛选变更后把 OAuth 专属字段写到其他提供商。
  if (enableOpenAIFlattenNamespaces.value && allOpenAIOAuth.value) {
    const extra = ensureExtra()
    extra.openai_responses_flatten_namespaces = openaiFlattenNamespacesEnabled.value
  }

  if (enableCodexImageToolMode.value) {
    const extra = ensureExtra()
    applyCodexImageToolMode(extra, codexImageToolMode.value, 'null')
  }

  if (enableOpenAIResponsesContinuationSupported.value && allOpenAIAPIKey.value) {
    const extra = ensureExtra()
    extra.openai_responses_continuation_supported = openAIResponsesContinuationSupported.value
  }

  if (enableModelRestriction.value) {
    // 所有提供商共用独立的模型映射和最终白名单，空集合恢复默认目录。
    const persisted = buildPersistedModelRestriction(allowedModels.value, modelMappings.value)
    credentials.model_mapping = persisted.modelMapping ?? {}
    credentials.model_whitelist = persisted.modelWhitelist
    credentialsChanged = true
  }

  if (enableCustomErrorCodes.value) {
    credentials.custom_error_codes_enabled = true
    credentials.custom_error_codes = [...selectedErrorCodes.value]
    credentialsChanged = true
  }

  if (enableInterceptWarmup.value && allInterceptWarmupCapable.value) {
    credentials.intercept_warmup_requests = interceptWarmupRequests.value
    credentialsChanged = true
  }

  if (enableHeaderOverride.value) {
    // 后端使用 JSONB || merge 语义：关闭时显式写入 false + 空对象以清除旧配置
    credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] = headerOverrideEnabled.value
    credentials[HEADER_OVERRIDES_CREDENTIAL_KEY] = headerOverrideEnabled.value
      ? buildHeaderOverridesObject(headerOverrideRows.value)
      : {}
    credentialsChanged = true
  }

  if (enableResponsesWSConnectionMode.value) {
    const extra = ensureExtra()
    extra.responses_ws_connection_mode = openaiOAuthResponsesWebSocketV2Mode.value

  }

  if (enableOpenAIAPIKeyWSMode.value) {
    const extra = ensureExtra()
    extra.responses_ws_connection_mode = openaiAPIKeyResponsesWebSocketV2Mode.value

  }

  if (enableCodexCLIOnly.value) {
    const extra = ensureExtra()
    extra.openai_oauth_client_policy = openAIOAuthClientPolicy.value
    // 兼容 codex_cli_only 字段，非 codex_only 策略写入 false 以覆盖 JSONB 中的 true。
    extra.codex_cli_only = openAIOAuthClientPolicy.value === 'codex_only'
    if (openAIOAuthClientPolicy.value !== 'codex_only') {
      extra.codex_cli_only_allowed_clients = []
    }
  }

  if (enableCodexCLIOnlyAllowClaudeCode.value) {
    const extra = ensureExtra()
    extra.codex_cli_only_allowed_clients =
      (!enableCodexCLIOnly.value || openAIOAuthClientPolicy.value === 'codex_only') &&
      codexCLIOnlyAllowClaudeCodeEnabled.value
        ? ['claude_code']
        : []
  }

  if (enableAutoPause5hThreshold.value) {
    const extra = ensureExtra()
    extra.auto_pause_5h_threshold = buildAutoPauseThresholdRatio(autoPause5hThreshold.value)
  }

  if (enableAutoPause7dThreshold.value) {
    const extra = ensureExtra()
    extra.auto_pause_7d_threshold = buildAutoPauseThresholdRatio(autoPause7dThreshold.value)
  }

  if (enableAutoPause5hDisabled.value) {
    const extra = ensureExtra()
    extra.auto_pause_5h_disabled = autoPause5hDisabled.value
  }

  if (enableAutoPause7dDisabled.value) {
    const extra = ensureExtra()
    extra.auto_pause_7d_disabled = autoPause7dDisabled.value
  }

  if (enableTLSFingerprint.value) {
    const extra = ensureExtra()
    extra.enable_tls_fingerprint = tlsFingerprintEnabled.value
    extra.tls_fingerprint_profile_id = tlsFingerprintEnabled.value ? tlsFingerprintProfileId.value : 0
    if (allOpenAIOAuth.value) {
      // 批量关闭 TLS 指纹时写入 0，清除提供商的 TLS 路由器。
      extra.tls_fingerprint_router_id = tlsFingerprintEnabled.value ? (tlsFingerprintRouterId.value ?? 0) : 0
    }
  }

  if (enableCodexFingerprintMode.value) {
    const extra = ensureExtra()
    // 批量 extra 使用 JSONB merge；off 也必须显式写入，才能覆盖已有的收敛档位。
    extra.codex_fingerprint_mode = codexFingerprintMode.value
  }

  if (enableOpenAICompactMode.value) {
    const extra = ensureExtra()
    extra.openai_compact_mode = openAICompactMode.value
  }

  if (enableOpenAINativeCompactionV2Mode.value) {
    const extra = ensureExtra()
    extra.openai_native_compaction_v2_mode = openAINativeCompactionV2Mode.value
  }

  if (enableOpenAICompactModelMapping.value && enableOpenAICompactMode.value && openAICompactMode.value !== 'force_off') {
    credentials.compact_model_mapping = buildOpenAICompactModelMapping() ?? {}
    credentialsChanged = true
  }

  // RPM limit settings (写入 extra 字段)
  if (enableRpmLimit.value) {
    const extra = ensureExtra()
    if (rpmLimitEnabled.value && bulkBaseRpm.value != null && bulkBaseRpm.value > 0) {
      extra.base_rpm = bulkBaseRpm.value
      extra.rpm_strategy = bulkRpmStrategy.value
      if (bulkRpmStickyBuffer.value != null && bulkRpmStickyBuffer.value > 0) {
        extra.rpm_sticky_buffer = bulkRpmStickyBuffer.value
      }
    } else {
      // 关闭 RPM 限制 - 设置 base_rpm 为 0，并用空值覆盖关联字段
      // 后端使用 JSONB || merge 语义，不会删除已有 key，
      // 所以必须显式发送空值来重置（后端读取时会 fallback 到默认值）
      extra.base_rpm = 0
      extra.rpm_strategy = ''
      extra.rpm_sticky_buffer = 0
    }
    updates.extra = extra
  }

  // 用户消息限速随 RPM 限制的应用勾选一起提交，null 表示不修改。
  if (enableRpmLimit.value && userMsgQueueMode.value !== null) {
    const umqExtra = ensureExtra()
    umqExtra.user_msg_queue_mode = userMsgQueueMode.value  // '' = 清除提供商级覆盖
    umqExtra.user_msg_queue_enabled = false  // 清理旧字段（JSONB merge）
  }

  if (enableUpstreamProtocols.value && upstreamProtocols.value !== undefined) {
    credentials.upstream_protocols = [...upstreamProtocols.value]
    credentialsChanged = true
  }

  if (credentialsChanged) {
    updates.credentials = credentials
  }

  return Object.keys(updates).length > 0 ? updates : null
}

const handleClose = () => {
  emit('close')
}

const handleSubmit = async () => {
  if (targetMode.value === 'selected' && props.providerIds.length === 0) {
    appStore.showError(t('admin.providers.bulkEdit.noSelection'))
    return
  }

  const hasAnyFieldEnabled =
    enableUpstreamProtocols.value ||
    (enableBaseUrl.value && allBaseUrlCapable.value) ||
    enableOpenAIPassthrough.value ||
    enableOpenAIFlattenNamespaces.value ||
    enableCodexImageToolMode.value ||
    enableOpenAIResponsesContinuationSupported.value ||
    enableModelRestriction.value ||
    enableCustomErrorCodes.value ||
    (enableInterceptWarmup.value && allInterceptWarmupCapable.value) ||
    enableHeaderOverride.value ||
    enableProxy.value ||
    enableConcurrency.value ||
    enableLoadFactor.value ||
    enablePriority.value ||
    enableRateMultiplier.value ||
    enableStatus.value ||
    enableGroups.value ||
    enableResponsesWSConnectionMode.value ||
    enableOpenAIAPIKeyWSMode.value ||
    enableCodexCLIOnly.value ||
    enableCodexCLIOnlyAllowClaudeCode.value ||
    enableAutoPause5hThreshold.value ||
    enableAutoPause7dThreshold.value ||
    enableAutoPause5hDisabled.value ||
    enableAutoPause7dDisabled.value ||
    enableTLSFingerprint.value ||
    enableCodexFingerprintMode.value ||
    enableOpenAICompactMode.value ||
    enableOpenAINativeCompactionV2Mode.value ||
    enableOpenAICompactModelMapping.value ||
    enableRpmLimit.value

  if (!hasAnyFieldEnabled) {
    appStore.showError(t('admin.providers.bulkEdit.noFieldsSelected'))
    return
  }


  // base_url 现在也会作用于 Grok OAuth 订阅提供商的转发端点；坏值会让请求期
  // 校验失败、提供商请求全挂，因此保存前强制格式校验（与单提供商编辑一致）。
  if (enableBaseUrl.value && allBaseUrlCapable.value) {
    const trimmedBaseUrl = baseUrl.value.trim()
    if (trimmedBaseUrl && !/^https?:\/\//i.test(trimmedBaseUrl)) {
      appStore.showError(t('admin.providers.grokCustomBaseUrl.invalid'))
      return
    }
  }

  if (enableHeaderOverride.value && headerOverrideEnabled.value) {
    // 批量保存对 header_overrides 是整键替换：开启但没有任何有效行会把所选提供商的
    // 既有覆写配置静默清空，必须显式拦截（清空请走关闭开关的路径，有专门提示）
    if (!headerOverrideRows.value.some((row) => row.name.trim())) {
      appStore.showError(t('admin.providers.headerOverride.bulkEmptyRows'))
      return
    }
    const headerError = validateHeaderOverrideRows(headerOverrideRows.value)
    if (headerError) {
      appStore.showError(t(`admin.providers.headerOverride.${headerError}`))
      return
    }
  }

  const built = buildUpdatePayload()
  if (!built) {
    appStore.showError(t('admin.providers.bulkEdit.noFieldsSelected'))
    return
  }


  await submitBulkUpdate(built)
}

const submitBulkUpdate = async (baseUpdates: Record<string, unknown>) => {
  const updates = baseUpdates

  submitting.value = true

  try {
    const res = targetMode.value === 'filtered' && props.target?.filters
      ? await adminAPI.providers.bulkUpdate({
        filters: props.target.filters,
        ...updates
      })
      : await adminAPI.providers.bulkUpdate(props.providerIds, updates)
    const success = res.success || 0
    const failed = res.failed || 0

    if (success > 0 && failed === 0) {
      appStore.showSuccess(t('admin.providers.bulkEdit.success', { count: success }))
    } else if (success > 0) {
      appStore.showError(t('admin.providers.bulkEdit.partialSuccess', { success, failed }))
    } else {
      appStore.showError(t('admin.providers.bulkEdit.failed'))
    }

    if (success > 0) {
      emit('updated')
      handleClose()
    }
  } catch (error: any) {appStore.showError(error.message || t('admin.providers.bulkEdit.failed'))
console.error('Error bulk updating providers:', error)
  } finally {
    submitting.value = false
  }
}

const resetBulkEditFormState = () => {
  enableBaseUrl.value = false
  enableModelRestriction.value = false
  enableCustomErrorCodes.value = false
  enableInterceptWarmup.value = false
  enableHeaderOverride.value = false
  enableProxy.value = false
  enableConcurrency.value = false
  enableLoadFactor.value = false
  enablePriority.value = false
  enableRateMultiplier.value = false
  enableStatus.value = false
  enableGroups.value = false
  enableOpenAIPassthrough.value = false
  enableOpenAIFlattenNamespaces.value = false
  enableCodexImageToolMode.value = false
  enableUpstreamProtocols.value = false
  upstreamProtocols.value = undefined
  enableOpenAIResponsesContinuationSupported.value = false
  enableResponsesWSConnectionMode.value = false
  enableOpenAIAPIKeyWSMode.value = false
  enableCodexCLIOnly.value = false
  enableCodexCLIOnlyAllowClaudeCode.value = false
  enableAutoPause5hThreshold.value = false
  enableAutoPause7dThreshold.value = false
  enableAutoPause5hDisabled.value = false
  enableAutoPause7dDisabled.value = false
  enableCodexFingerprintMode.value = false
  enableOpenAICompactMode.value = false
  enableOpenAINativeCompactionV2Mode.value = false
  enableOpenAICompactModelMapping.value = false
  enableRpmLimit.value = false
  enableTLSFingerprint.value = false

  baseUrl.value = ''
  openaiPassthroughEnabled.value = false
  openaiFlattenNamespacesEnabled.value = false
  codexImageToolMode.value = 'inherit'
  openAIResponsesContinuationSupported.value = false
  resetModelRestrictionDraft()
  selectedErrorCodes.value = []
  interceptWarmupRequests.value = false
  headerOverrideEnabled.value = false
  headerOverrideRows.value = []
  proxyId.value = null
  concurrency.value = 1
  loadFactor.value = null
  priority.value = 1
  rateMultiplier.value = 1
  status.value = 'active'
  groupIds.value = []
  openaiOAuthResponsesWebSocketV2Mode.value = RESPONSES_WS_POOLED
  openaiAPIKeyResponsesWebSocketV2Mode.value = RESPONSES_WS_POOLED
  openAIOAuthClientPolicy.value = 'any'
  codexCLIOnlyAllowClaudeCodeEnabled.value = false
  autoPause5hThreshold.value = null
  autoPause7dThreshold.value = null
  autoPause5hDisabled.value = false
  autoPause7dDisabled.value = false
  codexFingerprintMode.value = 'off'
  openAICompactMode.value = 'force_on'
  openAINativeCompactionV2Mode.value = 'force_on'
  openAICompactModelMappings.value = []
  rpmLimitEnabled.value = false
  bulkBaseRpm.value = null
  bulkRpmStrategy.value = 'tiered'
  bulkRpmStickyBuffer.value = null
  userMsgQueueMode.value = null
  tlsFingerprintEnabled.value = false
  tlsFingerprintProfileId.value = 0
  tlsFingerprintRouterId.value = null
}

watch(
  [
    () => props.show,
    () => props.providerIds.join(','),
    () => props.selectedPlatforms.join(','),
    () => props.selectedTypes.join(',')
  ],
  ([newShow]) => {
    if (newShow) {
      if (allTLSFingerprintCapable.value) {
        void loadTLSFingerprintProfiles()
      }
      if (allOpenAIOAuth.value) {
        void loadTLSFingerprintRouters()
      }
      void loadSelectedProviderDefaults()
      return
    }

    modelRestrictionPrefillSeq.value++
    resetBulkEditFormState()
  },
  { immediate: true }
)
</script>
