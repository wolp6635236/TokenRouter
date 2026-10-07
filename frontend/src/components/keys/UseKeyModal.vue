<template>
  <BaseDialog :show="show" :title="t('keys.useKeyModal.title')" width="wide" @close="emit('close')">
    <div class="space-y-4">
      <template v-if="compositeGroups?.length">
        <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('keys.useKeyModal.compositeDescription') }}</p>
        <div v-for="binding in compositeGroups" :key="binding.group_id" class="rounded-control border border-gray-200 p-3 dark:border-dark-600">
          <p class="text-sm font-medium">{{ (binding.group?.display_name || binding.group?.name) || `#${binding.group_id}` }}</p>
          <template v-if="binding.group?.models?.length">
            <code class="text-xs">{{ binding.prefix }}/{{ binding.group.models[0] }}</code>
            <button type="button" class="btn btn-secondary btn-sm ml-2" @click="copyContent(`${binding.prefix}/${binding.group.models[0]}`, binding.group_id)">{{ t('keys.useKeyModal.copy') }}</button>
          </template>
          <p v-else class="input-hint">{{ t('keys.useKeyModal.noModels') }}</p>
        </div>
      </template>
      <div v-else-if="!group" class="rounded-control bg-amber-50 p-4 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        <p class="font-medium">{{ t('keys.useKeyModal.noGroupTitle') }}</p>
        <p>{{ t('keys.useKeyModal.noGroupDescription') }}</p>
      </div>
      <div v-else-if="!clients.length" data-testid="no-text-protocols" class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('keys.useKeyModal.noTextProtocolsDescription') }}
      </div>
      <template v-else>
        <div class="flex flex-wrap gap-2" role="tablist">
          <button v-for="client in clients" :key="client" type="button" role="tab" :aria-selected="activeClient === client" class="btn btn-secondary btn-sm" :class="{ 'text-primary-600': activeClient === client }" @click="activeClient = client">
            <Icon v-if="client === 'opencode'" name="terminal" size="sm" />
            <svg v-else width="16" height="16" viewBox="0 0 24 24" fill-rule="evenodd" class="shrink-0" aria-hidden="true">
              <path v-for="(path, index) in modelIconData[CLIENT_ICON_KEYS[client]].paths" :key="index" :d="path" :fill="modelIconData[CLIENT_ICON_KEYS[client]].color" />
            </svg>
            {{ CLIENT_LABELS[client] }}
          </button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <div :class="{ 'sm:col-span-2': activeClient === 'opencode' }">
            <label class="input-label">{{ t('keys.useKeyModal.model') }}</label>
            <Select v-model="selectedModel" :options="modelOptions" searchable :placeholder="t('keys.useKeyModal.selectModel')" />
            <p v-if="!modelOptions.length" class="input-hint">{{ t('keys.useKeyModal.noModels') }}</p>
          </div>
          <div v-if="activeClient !== 'opencode'">
            <label class="input-label">{{ t('keys.useKeyModal.shell') }}</label>
            <Select v-model="shell" :options="shellOptions" />
          </div>
        </div>
        <div v-if="activeClient === 'codex'" class="space-y-4">
          <div class="flex items-center justify-between gap-4">
            <div>
              <p class="text-sm font-medium">{{ t('keys.useKeyModal.directAuth') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('keys.useKeyModal.directAuthDescription') }}</p>
            </div>
            <Toggle v-model="directAuth" />
          </div>
          <div v-if="websocketAllowed" class="flex items-center justify-between gap-4">
            <div>
              <p class="text-sm font-medium">Responses WebSocket</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('keys.useKeyModal.websocketDescription') }}</p>
            </div>
            <Toggle v-model="websocket" />
          </div>
        </div>
        <div v-for="(file, index) in files" :key="file.path" class="overflow-hidden rounded-control border border-gray-200 dark:border-dark-700">
          <div class="flex items-center justify-between gap-3 bg-gray-50 px-4 py-2 dark:bg-dark-800">
            <code class="text-xs">{{ file.path }}</code>
            <button type="button" class="btn btn-secondary btn-sm" @click="copyContent(file.content, index)">{{ copiedIndex === index ? t('keys.useKeyModal.copied') : t('keys.useKeyModal.copy') }}</button>
          </div>
          <pre class="overflow-x-auto whitespace-pre-wrap break-all p-4 text-xs"><code>{{ file.content }}</code></pre>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiKeyCompositeGroup, Group } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { COPY_FEEDBACK_MS } from '@/constants/ui'
import { modelIconData } from '@/utils/modelIconData'
import { availableClients, buildClientConfig, clientProtocol, CLIENT_ICON_KEYS, CLIENT_LABELS, modelsForProtocol, type ClientKind, type ConfigShell } from '@/utils/clientConfig'

const props = defineProps<{ show: boolean; apiKey: string; baseUrl: string; group?: Group | null; compositeGroups?: ApiKeyCompositeGroup[] }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const activeClient = ref<ClientKind>('claude')
const selectedModel = ref('')
const shell = ref<ConfigShell>('unix')
const directAuth = ref(false)
const websocket = ref(false)
const copiedIndex = ref<number | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | undefined
const shellOptions = [{ value: 'unix', label: 'macOS / Linux' }, { value: 'powershell', label: 'Windows PowerShell' }, { value: 'cmd', label: 'Windows CMD' }]
const protocols = computed(() => props.group?.allowed_protocols ?? [])
const clients = computed(() => availableClients(protocols.value))
const protocol = computed(() => clientProtocol(activeClient.value, protocols.value))
const modelOptions = computed(() => protocol.value ? modelsForProtocol(props.group ?? undefined, protocol.value).map(model => ({ value: model, label: model })) : [])
const websocketAllowed = computed(() => protocols.value.includes('openai_responses_websocket') && (!props.group?.model_protocols || props.group.model_protocols[selectedModel.value]?.includes('openai_responses_websocket')))
const files = computed(() => protocol.value && modelOptions.value.some(option => option.value === selectedModel.value) ? buildClientConfig({
  client: activeClient.value, protocol: protocol.value, model: selectedModel.value,
  baseUrl: props.baseUrl || window.location.origin, apiKey: props.apiKey, shell: shell.value,
  attributes: props.group?.model_attributes?.[selectedModel.value],
  directAuth: directAuth.value, websocket: websocketAllowed.value && websocket.value,
}) : [])

// 换 Key 或协议后，只保留仍可用的客户端和真实模型。
watch(clients, values => { if (!values.includes(activeClient.value) && values[0]) activeClient.value = values[0] }, { immediate: true })
watch(modelOptions, values => { if (!values.some(option => option.value === selectedModel.value)) selectedModel.value = values[0]?.value ?? '' }, { immediate: true })
watch(() => props.show, show => { if (show) { shell.value = 'unix'; directAuth.value = false; websocket.value = false } })
async function copyContent(content: string, index: number) {
  if (!await copyToClipboard(content)) return
  copiedIndex.value = index
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copiedIndex.value = null }, COPY_FEEDBACK_MS)
}
onBeforeUnmount(() => clearTimeout(copiedTimer))
</script>
