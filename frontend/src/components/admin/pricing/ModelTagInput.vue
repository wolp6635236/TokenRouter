<template>
  <div>
    <!-- 容器与 .input 同底色和描边；标签允许长模型名换行，删除按钮保持可见。 -->
    <TransitionGroup name="motion-list" @before-leave="prepareListLeave" @before-enter="restoreEnteringElement" tag="div" class="relative flex min-h-9 flex-wrap gap-2 rounded-control border border-primary-900/10 bg-white p-2 transition duration-fast focus-within:ring-2 focus-within:ring-black/10 dark:border-dark-600 dark:bg-dark-950 dark:focus-within:border-dark-400 dark:focus-within:ring-white/6">
      <span
        v-for="(model, idx) in models"
        :key="model"
        class="inline-flex max-w-full items-center gap-2 rounded-compact px-2 py-1 text-sm"
        :class="getPlatformTagClass(props.platform || '')"
      >
        <span class="min-w-0 break-all">{{ model }}</span>
        <button
          type="button"
          @click="removeModel(idx)"
          :aria-label="`${t('common.delete')} ${model}`"
          class="shrink-0 rounded-compact opacity-70 hover:bg-black/10 hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-white/10"
        >
          <Icon name="x" size="xs" />
        </button>
      </span>
      <input
        key="model-input"
        v-model="inputValue"
        type="text"
        class="min-w-0 flex-1 basis-32 border-none bg-transparent text-sm outline-none placeholder:text-gray-400 dark:text-white"
        :aria-label="ariaLabel || placeholder || t('admin.pricing.form.modelInputHint')"
        :placeholder="models.length === 0 ? placeholder : ''"
        @keydown.enter.prevent="addModel"
        @keydown.tab.prevent="addModel"
        @keydown.delete="handleBackspace"
        @paste="handlePaste"
      />
    </TransitionGroup>
    <p class="mt-1 text-xs text-gray-400">
      {{ t('admin.pricing.form.modelInputHint', 'Press Enter to add, supports paste for batch import.') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { prepareListLeave, restoreEnteringElement } from '@/utils/leavingElement'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getPlatformTagClass } from './types'

const { t } = useI18n()

const props = defineProps<{
  models: string[]
  ariaLabel?: string
  placeholder?: string
  platform?: string
}>()

const emit = defineEmits<{
  'update:models': [models: string[]]
}>()

const inputValue = ref('')

function addModel() {
  const val = inputValue.value.trim()
  if (!val) return
  if (!props.models.includes(val)) {
    emit('update:models', [...props.models, val])
  }
  inputValue.value = ''
}

function removeModel(idx: number) {
  const newModels = [...props.models]
  newModels.splice(idx, 1)
  emit('update:models', newModels)
}

function handleBackspace() {
  if (inputValue.value === '' && props.models.length > 0) {
    removeModel(props.models.length - 1)
  }
}

function handlePaste(e: ClipboardEvent) {
  e.preventDefault()
  const text = e.clipboardData?.getData('text') || ''
  const items = text.split(/[,\n;]+/).map(s => s.trim()).filter(Boolean)
  if (items.length === 0) return
  const unique = [...new Set([...props.models, ...items])]
  emit('update:models', unique)
  inputValue.value = ''
}
</script>
