<template>
  <SettingsSection>
    <!-- 与“对话中的图片生成”使用同一布局：标题、作用范围、全宽下拉和当前选项说明。 -->
    <div class="space-y-2">
      <label :for="`${uid}-select`" class="input-label">{{ t('admin.providers.openai.wsConnectionTitle') }}</label>
      <p :id="`${uid}-scope`" class="input-hint">{{ t('admin.providers.openai.wsConnectionScope') }}</p>
      <Select
        :id="`${uid}-select`"
        :aria-label="t('admin.providers.openai.wsConnectionTitle')"
        :aria-describedby="`${uid}-scope ${uid}-description`"
        :data-testid="`${testIdPrefix}-select`"
        :model-value="modelValue"
        :options="options"
        @update:model-value="emit('update:modelValue', String($event) as ResponsesWSConnectionMode)"
      />
      <p :id="`${uid}-description`" class="input-hint">{{ t(responsesWSConnectionHint(modelValue)) }}</p>
    </div>
  </SettingsSection>
</template>

<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'

import Select from '@/components/common/Select.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import { useResponsesWSConnectionModeOptions } from '@/components/provider/form/providerFormOptions'
import { responsesWSConnectionHint, type ResponsesWSConnectionMode } from '@/utils/responsesWsConnection'

withDefaults(defineProps<{
  modelValue: ResponsesWSConnectionMode
  testIdPrefix?: string
}>(), {
  testIdPrefix: 'responses-ws-connection',
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: ResponsesWSConnectionMode): void
}>()

const { t } = useI18n()
const uid = useId()
const options = useResponsesWSConnectionModeOptions()
</script>
