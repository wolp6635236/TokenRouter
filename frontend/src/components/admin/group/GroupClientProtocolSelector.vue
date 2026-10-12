<template>
  <SettingsSection
    :title="t('admin.protocols.groupTitle')"
    :hint="t('admin.protocols.groupHint')"
  >
    <div
      v-if="protocolCatalogError"
      class="flex items-center gap-2 text-sm text-red-500"
      role="alert"
    >
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
    <p v-else-if="!protocolCatalog" class="input-hint">
      {{ t('common.loading') }}
    </p>
    <div
      data-testid="client-protocol-list"
      class="divide-y divide-gray-100 dark:divide-dark-700"
    >
      <div
        v-for="protocol in protocols"
        :key="protocol.id"
        class="py-2.5"
      >
        <div class="flex items-center gap-3">
          <div class="flex min-w-0 flex-1 items-baseline gap-x-2">
            <span
              class="shrink-0 text-sm font-medium"
              :class="
                isEnabled(protocol.id)
                  ? 'text-primary-900 dark:text-dark-50'
                  : 'text-gray-400 dark:text-dark-400'
              "
              >{{ protocol.name }}</span
            ><code
              class="hidden min-w-0 truncate text-xs text-gray-400 dark:text-dark-500 sm:inline"
              :data-protocol-endpoint="protocol.id"
              >{{ protocol.endpoint }}</code
            >
          </div>
          <template v-if="isEnabled(protocol.id) && hasFallbackTargets(protocol.id)">
            <span
              class="hidden shrink-0 text-xs text-gray-400 dark:text-dark-500 md:inline"
              >{{ t('admin.protocols.fallbackWhen') }}</span
            >
            <Select
              class="protocol-mode-select w-32 shrink-0 sm:w-40"
              :aria-label="`${protocol.name}: ${t('admin.protocols.fallback')}`"
              :model-value="fallbackMode(protocol.id)"
              :options="modeOptions"
              @update:model-value="setMode(protocol.id, String($event))"
            />
          </template>
          <Toggle
            size="sm"
            class="shrink-0"
            :model-value="modelValue.includes(protocol.id)"
            :data-protocol="protocol.id"
            :aria-label="protocol.name"
            @update:model-value="toggle(protocol.id)"
          />
        </div>
        <p v-if="protocol.id === 'openai_responses_websocket' && isEnabled(protocol.id) && fallbackMode(protocol.id) !== 'native'" class="mt-2 input-hint">
          {{ t('admin.protocols.responsesWebSocketHTTPHint') }}
        </p>
        <!-- 回退目标可互换位置，行 key 使用下标并关闭列表动效。 -->
        <RuleListEditor
          v-if="
            isEnabled(protocol.id) &&
            fallbackMode(protocol.id) === 'restricted' &&
            hasFallbackTargets(protocol.id)
          "
          class="mt-2 rounded-compact border border-gray-100 bg-gray-50/50 p-3 dark:border-dark-700 dark:bg-dark-800/40"
          :items="fallbacks?.[protocol.id] ?? []"
          :animated="false"
          add-placement="footer"
          :add-disabled="!remainingTarget(protocol.id)"
          @add="addTarget(protocol.id)"
          @remove="(index) => removeTarget(protocol.id, index)"
        >
          <template #row="{ item: target, index }">
            <div class="flex items-center gap-2">
              <span
                class="w-4 shrink-0 text-right text-xs tabular-nums text-gray-400 dark:text-dark-500"
                >{{ index + 1 }}</span
              >
              <Select
                class="min-w-0 flex-1"
                :aria-label="`${protocol.name}: ${t('admin.protocols.fallback')} ${index + 1}`"
                :model-value="target"
                :options="targetOptions(protocol.id)"
                @update:model-value="
                  setTarget(protocol.id, index, String($event) as ProtocolID)
                "
              />
            </div>
          </template>
        </RuleListEditor>
      </div>
    </div>
    <div class="space-y-2 border-t border-gray-200 pt-6 dark:border-dark-600">
      <label :for="`${idPrefix}-image-policy`" class="input-label">{{
        t('admin.protocols.imagePolicy')
      }}</label>
      <p :id="`${idPrefix}-image-policy-scope`" class="input-hint">
        {{ t('admin.protocols.imagePolicyHint') }}
      </p>
      <Select
        :aria-label="t('admin.protocols.imagePolicy')"
        :aria-describedby="`${idPrefix}-image-policy-scope ${idPrefix}-image-policy-description`"
        :id="`${idPrefix}-image-policy`"
        :model-value="imagePolicy ?? 'inherit'"
        :options="imageOptions"
        @update:model-value="
          emit('update:imagePolicy', String($event) as CodexImageToolMode)
        "
      />
      <p :id="`${idPrefix}-image-policy-description`" class="input-hint">
        {{ t(`admin.protocols.imagePolicyOptions.${imagePolicy ?? 'inherit'}.description`) }}
      </p>
    </div>
  </SettingsSection>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import RuleListEditor from '@/components/common/RuleListEditor.vue'
import {
  loadProtocolCatalog,
  protocolCatalog,
  protocolCatalogError,
  protocolCatalogLoading,
} from '@/api/admin/protocolCapabilities'
import type { ProtocolID } from '@/types'
import type { CodexImageToolMode } from '@/utils/codexImageToolMode'
import { setGroupClientProtocol } from '@/utils/groupClientProtocols'
const props = withDefaults(
  defineProps<{
    idPrefix?: string
    modelValue: ProtocolID[]
    fallbacks?: Partial<Record<ProtocolID, ProtocolID[]>>
    imagePolicy?: CodexImageToolMode
  }>(),
  { idPrefix: 'group-protocol' },
)
const emit = defineEmits<{
  'update:modelValue': [value: ProtocolID[]]
  'update:fallbacks': [value: Partial<Record<ProtocolID, ProtocolID[]>>]
  'update:imagePolicy': [value: CodexImageToolMode]
}>()
const { t } = useI18n()
// 分组图片策略有继承、启用、禁用和拦截四种取值，选项说明各自的来源和请求行为。
const imageModes = ['inherit', 'enabled', 'disabled', 'block'] as const
const imageOptions = computed(() =>
  imageModes.map((value) => ({
    value,
    label: t(`admin.protocols.imagePolicyOptions.${value}.label`),
  })),
)
// 所有表单共享加载状态；任一入口重试成功后同时恢复。
function retryCatalog() {
  void loadProtocolCatalog().catch(() => {})
}
retryCatalog()
const profile = computed(() => protocolCatalog.value?.groups[0])
const protocols = computed(
  () =>
    protocolCatalog.value?.protocols.filter((protocol) =>
      profile.value?.protocols.includes(protocol.id),
    ).map(protocol => ({ ...protocol, name: protocol.id === 'openai_responses_websocket' ? t('admin.protocols.responsesWebSocket') : protocol.name })) ?? [],
)
function isEnabled(id: ProtocolID) {
  return props.modelValue.includes(id)
}
// 关闭的入口不承接请求，转换策略无意义，行内控件随开关显隐。
function hasFallbackTargets(id: ProtocolID) {
  return !!profile.value?.fallback_targets[id]?.length
}
function targetOptions(source: ProtocolID) {
  return (profile.value?.fallback_targets[source] ?? []).map((id) => ({
    value: id,
    label:
      source === 'openai_responses_websocket' && id === 'openai_responses' ? t('admin.protocols.responsesWebSocketHTTP') : protocolCatalog.value?.protocols.find((protocol) => protocol.id === id)
        ?.name ?? id,
  }))
}
function toggle(id: ProtocolID) {
  emit(
    'update:modelValue',
    setGroupClientProtocol(
      props.modelValue,
      id,
      !props.modelValue.includes(id),
    ),
  )
}
// 缺少入口采用自动转换；空数组仅允许原生，显式列表按顺序尝试。
const modeOptions = computed(() => [
  { value: 'auto', label: t('admin.protocols.auto') },
  { value: 'native', label: t('admin.protocols.nativeOnly') },
  { value: 'restricted', label: t('admin.protocols.restricted') },
])
function fallbackMode(source: ProtocolID) {
  const targets = props.fallbacks?.[source]
  return targets === undefined
    ? 'auto'
    : targets.length
      ? 'restricted'
      : 'native'
}
function setMode(source: ProtocolID, mode: string) {
  const next = { ...props.fallbacks }
  if (mode === 'auto') delete next[source]
  else
    next[source] =
      mode === 'restricted'
        ? (profile.value?.fallback_targets[source] ?? []).slice(0, 1)
        : []
  emit('update:fallbacks', next)
}
function remainingTarget(source: ProtocolID) {
  return profile.value?.fallback_targets[source]?.find(
    (target) => !props.fallbacks?.[source]?.includes(target),
  )
}
function addTarget(source: ProtocolID) {
  const target = remainingTarget(source)
  if (target)
    emit('update:fallbacks', {
      ...props.fallbacks,
      [source]: [...(props.fallbacks?.[source] ?? []), target],
    })
}
function setTarget(source: ProtocolID, index: number, target: ProtocolID) {
  const targets = [...(props.fallbacks?.[source] ?? [])]
  const existing = targets.indexOf(target)
  if (existing >= 0 && existing !== index) targets[existing] = targets[index]
  targets[index] = target
  emit('update:fallbacks', { ...props.fallbacks, [source]: targets })
}
function removeTarget(source: ProtocolID, index: number) {
  emit('update:fallbacks', {
    ...props.fallbacks,
    [source]: props.fallbacks?.[source]?.filter((_, i) => i !== index) ?? [],
  })
}
</script>

<style scoped>
/* 组件内的转换策略下拉框高 32px，与紧凑协议行同高。 */
.protocol-mode-select :deep(.input-trigger) {
  @apply min-h-8 px-3 py-1 text-xs;
}
</style>
