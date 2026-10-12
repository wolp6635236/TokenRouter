<template>
  <RuleListEditor
    :items="modelValue"
    :add-label="t('admin.providers.tempUnschedulable.addRule')"
    add-placement="footer"
    variant="card"
    :item-label="(index) => t('admin.providers.tempUnschedulable.ruleIndex', { index: index + 1 })"
    reorderable
    @add="appendRule(createEmptyRule())"
    @remove="removeRule"
    @move="moveRule"
  >
    <template #header-extra>
      <SettingsNotice>{{ t('admin.providers.tempUnschedulable.notice') }}</SettingsNotice>
      <div class="flex flex-wrap gap-2">
        <button
          v-for="preset in presets"
          :key="preset.label"
          type="button"
          class="rounded-control bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600"
          @click="appendRule({ ...preset.rule })"
        >
          + {{ preset.label }}
        </button>
      </div>
    </template>
    <template #row="{ item: rule }">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.providers.tempUnschedulable.errorCode') }}</label>
          <input
            v-model.number="rule.error_code"
            type="number"
            min="100"
            max="599"
            class="input"
            :placeholder="t('admin.providers.tempUnschedulable.errorCodePlaceholder')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.providers.tempUnschedulable.durationMinutes') }}</label>
          <input
            v-model.number="rule.duration_minutes"
            type="number"
            min="1"
            class="input"
            :placeholder="t('admin.providers.tempUnschedulable.durationPlaceholder')"
          />
        </div>
        <div class="sm:col-span-2">
          <label class="input-label">{{ t('admin.providers.tempUnschedulable.keywords') }}</label>
          <input
            v-model="rule.keywords"
            type="text"
            class="input"
            :placeholder="t('admin.providers.tempUnschedulable.keywordsPlaceholder')"
          />
          <p class="input-hint">{{ t('admin.providers.tempUnschedulable.keywordsHint') }}</p>
        </div>
        <div class="sm:col-span-2">
          <label class="input-label">{{ t('admin.providers.tempUnschedulable.description') }}</label>
          <input
            v-model="rule.description"
            type="text"
            class="input"
            :placeholder="t('admin.providers.tempUnschedulable.descriptionPlaceholder')"
          />
        </div>
      </div>
    </template>
  </RuleListEditor>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsNotice from '@/components/common/settings/SettingsNotice.vue'
import RuleListEditor from '@/components/common/RuleListEditor.vue'

/** TempUnschedRuleForm 是临时不可调度规则的表单行，关键词以逗号分隔的文本编辑。 */
export interface TempUnschedRuleForm {
  error_code: number | null
  keywords: string
  duration_minutes: number | null
  description: string
}

const props = defineProps<{
  modelValue: TempUnschedRuleForm[]
}>()

const emit = defineEmits<{
  'update:modelValue': [rules: TempUnschedRuleForm[]]
}>()

const { t } = useI18n()

const presets = computed(() => [
  {
    label: t('admin.providers.tempUnschedulable.presets.overloadLabel'),
    rule: {
      error_code: 529,
      keywords: 'overloaded, too many',
      duration_minutes: 60,
      description: t('admin.providers.tempUnschedulable.presets.overloadDesc'),
    },
  },
  {
    label: t('admin.providers.tempUnschedulable.presets.rateLimitLabel'),
    rule: {
      error_code: 429,
      keywords: 'rate limit, too many requests',
      duration_minutes: 10,
      description: t('admin.providers.tempUnschedulable.presets.rateLimitDesc'),
    },
  },
  {
    label: t('admin.providers.tempUnschedulable.presets.unavailableLabel'),
    rule: {
      error_code: 503,
      keywords: 'unavailable, maintenance',
      duration_minutes: 30,
      description: t('admin.providers.tempUnschedulable.presets.unavailableDesc'),
    },
  },
])

const createEmptyRule = (): TempUnschedRuleForm => ({
  error_code: null,
  keywords: '',
  duration_minutes: 30,
  description: '',
})

const appendRule = (rule: TempUnschedRuleForm) => {
  emit('update:modelValue', [...props.modelValue, rule])
}

const removeRule = (index: number) => {
  emit(
    'update:modelValue',
    props.modelValue.filter((_, ruleIndex) => ruleIndex !== index),
  )
}

// 上下移动只交换相邻两条规则，其余规则保持原位。
const moveRule = (from: number, to: number) => {
  const rules = [...props.modelValue]
  const current = rules[from]
  rules[from] = rules[to]
  rules[to] = current
  emit('update:modelValue', rules)
}
</script>
