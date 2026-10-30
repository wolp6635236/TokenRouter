<template>
  <div>
    <label :for="inputID" class="input-label">{{ t('admin.pricing.referenceTarget') }}</label>
    <input
      :id="inputID"
      :value="modelValue"
      type="text"
      class="input"
      autocomplete="off"
      autocapitalize="off"
      :spellcheck="false"
      :placeholder="t('admin.pricing.referencePlaceholder')"
      :aria-label="t('admin.pricing.referenceTarget')"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      @change="emit('change')"
    />
    <p class="input-hint">{{ t('admin.pricing.referenceHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'

// ModelReferenceInput.vue edits a concrete target model ID as plain text.
// Input events keep the parent draft current, while change events let attribute
// editors refresh their default comparison after editing finishes. Profile validation
// checks the target when the draft is previewed or saved.

// Parent-owned modelValue retains the editable target exactly as entered.
defineProps<{
  modelValue: string // Target model ID draft.
}>()
// emit separates immediate draft edits from completed field changes for default lookups.
const emit = defineEmits<{
  'update:modelValue': [value: string] // Edited target identity.
  change: [] // Completed field edit.
}>()
// Translations label the target field, and inputID connects its label to this instance.
const { t } = useI18n()
const inputID = useId()
</script>
