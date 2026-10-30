<template>
  <div data-testid="opencode-fields">
    <details class="space-y-3">
      <summary class="cursor-pointer text-sm font-medium">{{
        t('admin.providers.opencode.modelProtocols')
      }}</summary>
      <p class="input-hint">{{ t('admin.providers.opencode.modelProtocolsHint') }}</p>
      <div
        v-for="(target, model) in displayedProtocols"
        :key="model"
        class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2"
      >
        <span class="break-all text-sm">{{ model }}</span>
        <Select
          :model-value="target"
          :options="protocolOptions"
          @update:model-value="setProtocol(model, String($event))"
        />
        <button
          v-if="hasOverride(model)"
          type="button"
          class="btn btn-ghost btn-sm"
          :aria-label="t('common.delete')"
          @click="removeProtocol(model)"
          ><Icon name="trash" size="sm"
        /></button>
        <span v-else class="text-xs text-gray-500 dark:text-gray-400">{{
          t('admin.providers.opencode.catalogSuggestion')
        }}</span>
      </div>
      <div class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
        <input
          v-model="newModel"
          class="input"
          :placeholder="t('admin.providers.fromModel')"
          :aria-label="t('admin.providers.fromModel')"
        />
        <Select
          v-model="newProtocol"
          :options="protocolOptions"
          :placeholder="t('admin.providers.opencode.selectProtocol')"
        />
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="!newModel.trim() || !newProtocol"
          @click="addProtocol"
          >{{ t('common.add') }}</button
        >
      </div>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  loadProtocolCatalog,
  nativeProtocolOptions,
  protocolName,
} from '@/api/admin/protocolCapabilities'
import type { ProtocolID } from '@/api/protocols.types'
import type { OpenCodeDraft } from './openCodeDraft'

// OpenCodeFields.vue edits exact model protocol overrides for the selected
// OpenCode provider_mode. Each row shows a model's manual override or its accepted
// catalog suggestion; deleting an override restores the suggestion. Model
// synchronization, mode selection, and addresses belong to the shared provider
// form, which also stores sync suggestions in this draft and saves both tables.
//
// Protocol choices come from the catalog's OpenCode API-key profile, limited to
// the protocols enabled on the form, so a model cannot be assigned a protocol
// the provider will refuse to call.

// props supplies the selected mode and the form's enabled upstream protocols.
const props = defineProps<{
  mode: string // Selected provider_mode whose tables are edited.
  // Enabled upstream protocols; undefined means the catalog defaults, which enable
  // every native OpenCode protocol.
  protocols?: ProtocolID[]
}>()
// The parent owns persistence, while this component edits its protocol tables in place.
const draft = defineModel<OpenCodeDraft>({ required: true })
// t, newModel, and newProtocol serve the translated labels and the pending manual row.
const { t } = useI18n()
const newModel = ref('')
const newProtocol = ref('')

// Load the shared catalog so protocol names and the native option list are available
// when this panel renders before the protocol selector has requested it.
void loadProtocolCatalog().catch(() => {})

// protocolOptions lists enabled native OpenCode protocols with their catalog names.
const protocolOptions = computed(() =>
  nativeProtocolOptions('opencode', 'apikey')
    .filter((id) => !props.protocols || props.protocols.includes(id))
    .map((id) => ({ value: id, label: protocolName(id) })),
)
// displayedProtocols keeps current manual choices ahead of the selected mode's catalog suggestions.
const displayedProtocols = computed(() => ({
  ...draft.value.suggestions[props.mode],
  ...draft.value.protocols[props.mode],
}))

// hasOverride reports whether the selected mode stores a manual protocol for model.
function hasOverride(model: string): boolean {
  return Object.prototype.hasOwnProperty.call(draft.value.protocols[props.mode] ?? {}, model)
}

// setProtocol assigns an administrator-selected protocol to one exact model identity.
function setProtocol(model: string, target: string): void {
  draft.value.protocols[props.mode] ??= {}
  draft.value.protocols[props.mode]![model] = target as ProtocolID
}

// removeProtocol removes a manual override and restores its accepted catalog choice.
function removeProtocol(model: string): void {
  delete draft.value.protocols[props.mode]?.[model]
}

// addProtocol stores the selected protocol and clears the new-model input fields.
function addProtocol(): void {
  if (!newModel.value.trim() || !newProtocol.value) return
  setProtocol(newModel.value.trim(), newProtocol.value)
  newModel.value = ''
  newProtocol.value = ''
}
</script>
