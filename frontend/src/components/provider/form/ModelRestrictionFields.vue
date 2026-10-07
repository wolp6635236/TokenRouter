<template>
  <SettingsSection
    :title="t('admin.providers.modelRestriction')"
    :hint="hint ?? t('admin.providers.modelRestrictionCombinedHint')"
  >
    <SettingsSegmented
      v-model="mode"
      block
      :ariaLabel="t('admin.providers.modelRestriction')"
      :options="modeOptions"
    />

    <div v-if="mode === 'whitelist'" v-content-reveal class="space-y-2">
      <ModelWhitelistSelector
        :model-value="allowedModels"
        :platform="platform"
        :platforms="platforms"
        :provider-id="providerId"
        :models="models"
        :sync-credentials="syncCredentials"
        @update:model-value="allowedModels = $event"
      />
      <p class="input-hint">
        {{ t('admin.providers.selectedModels', { count: allowedModels.length }) }}
        <span v-if="allowedModels.length === 0">{{ t('admin.providers.supportsAllModels') }}</span>
      </p>
    </div>

    <ProviderModelMappingEditor
      v-else
      v-model="mappings"
      :presets="presets"
      :source-placeholder="sourcePlaceholder"
      :target-placeholder="targetPlaceholder"
      @add="emit('add')"
      @remove="emit('remove')"
      @preset="(from, to) => emit('preset', from, to)"
    />
  </SettingsSection>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import SettingsSegmented from '@/components/common/settings/SettingsSegmented.vue'
import { vContentReveal } from '@/directives/contentReveal'
import type { ModelMappingRow } from '@/utils/modelMappingRules'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import ProviderModelMappingEditor, { type ProviderMappingPreset } from '../ProviderModelMappingEditor.vue'

export type ModelRestrictionMode = 'whitelist' | 'mapping'

// 白名单与映射二选一的模型限制区域，覆盖各平台和各账号类型的副本。
defineProps<{
  platform?: string
  platforms?: string[]
  providerId?: number
  models?: string[]
  syncCredentials?: {
    platform: string
    type: string
    base_url?: string
    api_key: string
  }
  presets?: ProviderMappingPreset[]
  sourcePlaceholder?: string
  targetPlaceholder?: string
  hint?: string
}>()
const mode = defineModel<ModelRestrictionMode>('mode', { required: true })
const allowedModels = defineModel<string[]>('allowedModels', { required: true })
const mappings = defineModel<ModelMappingRow[]>('mappings', { required: true })
const emit = defineEmits<{
  add: []
  remove: []
  preset: [from: string, to: string]
}>()

const { t } = useI18n()
const modeOptions = computed(() => [
  {
    value: 'whitelist' as const,
    label: t('admin.providers.modelWhitelist'),
    icon: 'checkCircle' as const,
    testid: 'model-restriction-mode-whitelist'
  },
  {
    value: 'mapping' as const,
    label: t('admin.providers.modelMapping'),
    icon: 'swap' as const,
    testid: 'model-restriction-mode-mapping'
  }
])
</script>
