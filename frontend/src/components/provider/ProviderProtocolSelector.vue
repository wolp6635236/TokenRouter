<template>
  <SettingsSection
    :title="hideTitle ? undefined : t('admin.protocols.nativeTitle')"
    :hint="t('admin.protocols.nativeHint')"
  >
    <div v-if="protocolCatalogError" class="flex items-center gap-2 text-sm text-red-500" role="alert">
      <span>{{ t('admin.protocols.loadError') }}</span>
      <button
        type="button"
        class="btn btn-secondary"
        data-testid="protocol-catalog-retry"
        :disabled="protocolCatalogLoading"
        @click="retryCatalog"
      >
        {{ t('common.retry') }}
      </button>
    </div>
    <p v-else-if="!protocolCatalog" class="input-hint">{{ t('common.loading') }}</p>
    <!-- 与分组客户端协议列表同样采用逐行开关，名称右侧附带端点。 -->
    <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
      <div v-for="id in options" :key="id" class="flex items-center gap-3 py-2.5">
        <div class="flex min-w-0 flex-1 items-baseline gap-x-2">
          <span
            class="shrink-0 text-sm font-medium"
            :class="modelValue?.includes(id) ? 'text-primary-900 dark:text-dark-50' : 'text-gray-400 dark:text-dark-400'"
          >{{ protocolName(id) }}</span>
          <code
            v-if="protocolEndpoint(id)"
            class="hidden min-w-0 truncate text-xs text-gray-400 dark:text-dark-500 sm:inline"
          >{{ protocolEndpoint(id) }}</code>
        </div>
        <Toggle
          size="sm"
          class="shrink-0"
          :model-value="!!modelValue?.includes(id)"
          :data-native-protocol="id"
          :aria-label="protocolName(id)"
          @update:model-value="toggle(id)"
        />
      </div>
    </div>
  </SettingsSection>
</template>
<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProtocolID } from '@/types'
import Toggle from '@/components/common/Toggle.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import { loadProtocolCatalog, nativeProtocolOptions, protocolCatalog, protocolCatalogError, protocolCatalogLoading } from '@/api/admin/protocolCapabilities'
// hideTitle 供批量编辑使用，标题由外层的应用开关行展示。
const props = defineProps<{ modelValue?: ProtocolID[]; platform: string; type: string; authMode?: string; hideTitle?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: ProtocolID[]] }>()
const { t } = useI18n()
const options = computed(() => nativeProtocolOptions(props.platform, props.type, props.authMode))
// 所有表单共享加载状态；任一入口重试成功后同时恢复。
function retryCatalog() { void loadProtocolCatalog().catch(() => {}) }
retryCatalog()
// 提供商类型/认证方式切换时使用新原生集合；显式空集合在普通回显时保留。
watch(() => [props.platform, props.type, props.authMode, protocolCatalog.value], (_, previous) => {
  if (!protocolCatalog.value) return
  const changed = previous && (previous[0] !== props.platform || previous[1] !== props.type || previous[2] !== props.authMode)
  if (props.modelValue === undefined || changed) emit('update:modelValue', [...options.value])
}, { immediate: true })
function protocolName(id: ProtocolID) {
  if (id === 'openai_responses_websocket') return t('admin.protocols.responsesWebSocket')
  return protocolCatalog.value?.protocols.find(item => item.id === id)?.name ?? id
}
function protocolEndpoint(id: ProtocolID) {
  return protocolCatalog.value?.protocols.find(item => item.id === id)?.endpoint
}
function toggle(id: ProtocolID) {
  const selected = new Set(props.modelValue ?? [])
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  emit('update:modelValue', options.value.filter(item => selected.has(item)))
}
</script>
