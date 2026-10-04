<template>
  <SettingsCard
    data-testid="quality-probe-settings"
    :title="t('admin.settings.qualityProbe.title')"
    :description="t('admin.settings.qualityProbe.description')"
  >
    <template #actions>
      <button
        type="button"
        class="btn btn-secondary btn-icon shrink-0"
        :disabled="loading"
        :title="t('common.refresh')"
        :aria-label="t('common.refresh')"
        @click="load"
      >
        <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
      </button>
    </template>
    <ContentSkeleton v-if="loading && !loaded" variant="form" :rows="5" />
    <SettingsSection v-else>
      <SettingToggleRow
        id="quality-probe-enabled"
        v-model="form.enabled"
        :label="t('admin.settings.qualityProbe.enabled')"
        :hint="t('admin.settings.qualityProbe.enabledHint')"
      />
      <SettingRow
        id="quality-probe-groups"
        :label="t('admin.settings.qualityProbe.groups')"
        :hint="t('admin.settings.qualityProbe.groupsHint')"
      >
        <div class="max-h-40 overflow-auto rounded-control border border-gray-200 p-3 dark:border-dark-600">
          <p v-if="groupsLoading" class="py-2 text-center text-xs text-gray-500">{{ t('common.loading') }}</p>
          <p v-else-if="!groups.length" class="py-2 text-center text-xs text-gray-500">{{ t('admin.pricing.form.noGroupsAvailable') }}</p>
          <div v-else class="flex flex-wrap gap-2">
            <label
              v-for="group in groups"
              :key="group.id"
              class="inline-flex max-w-full cursor-pointer items-center gap-2 rounded-control p-1.5 transition-colors hover:bg-gray-50 dark:hover:bg-dark-700"
              :class="[
                form.group_ids.includes(group.id) ? 'bg-primary-50 dark:bg-primary-500/8 dark:text-primary-500' : '',
                group.status === 'inactive' ? 'opacity-60' : '',
              ]"
            >
              <input v-model="form.group_ids" type="checkbox" :value="group.id" class="h-4 w-4 shrink-0 rounded-compact border-gray-300 text-primary-600 focus:ring-primary-500" />
              <GroupBadge :name="group.name" :display-brand="group.display_brand" :rate-multiplier="group.rate_multiplier" class="min-w-0" />
              <span v-if="group.status === 'inactive'" class="shrink-0 text-xs text-gray-400">{{ t('admin.settings.qualityProbe.groupInactive') }}</span>
            </label>
          </div>
        </div>
      </SettingRow>
      <SettingRow
        id="quality-probe-interval"
        field
        label-for="quality-probe-interval"
        :label="t('admin.settings.qualityProbe.intervalMinutes')"
        :hint="t('admin.settings.qualityProbe.intervalHint')"
      >
        <input
          id="quality-probe-interval"
          v-model.number="form.interval_minutes"
          type="number"
          min="1"
          max="1440"
          class="input"
        />
      </SettingRow>
      <SettingRow
        id="quality-probe-model"
        field
        label-for="quality-probe-model"
        :label="t('admin.settings.qualityProbe.model')"
        :hint="t('admin.settings.qualityProbe.modelHint')"
      >
        <input
          id="quality-probe-model"
          v-model="form.model"
          type="text"
          class="input font-mono text-sm"
        />
      </SettingRow>
      <SettingRow
        id="quality-probe-cooldown"
        field
        label-for="quality-probe-cooldown"
        :label="t('admin.settings.qualityProbe.cooldownMinutes')"
        :hint="t('admin.settings.qualityProbe.cooldownHint')"
      >
        <input
          id="quality-probe-cooldown"
          v-model.number="form.cooldown_minutes"
          type="number"
          min="1"
          max="1440"
          class="input"
        />
      </SettingRow>
      <SettingRow
        id="quality-probe-attempts"
        field
        label-for="quality-probe-attempts"
        :label="t('admin.settings.qualityProbe.maxAttempts')"
        :hint="t('admin.settings.qualityProbe.maxAttemptsHint')"
      >
        <input
          id="quality-probe-attempts"
          v-model.number="form.max_attempts"
          type="number"
          min="1"
          max="10"
          class="input"
        />
      </SettingRow>
      <SettingRow
        id="quality-probe-email"
        field
        label-for="quality-probe-email"
        :label="t('admin.settings.qualityProbe.notifyEmail')"
        :hint="t('admin.settings.qualityProbe.notifyEmailHint')"
      >
        <input
          id="quality-probe-email"
          v-model="form.notify_email"
          type="email"
          class="input"
        />
      </SettingRow>
    </SettingsSection>
  </SettingsCard>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useSettingsSaveTarget } from '@/composables/useSettingsSaveRegistry'
import { adminAPI } from '@/api/admin'
import type { QualityProbeSettings } from '@/api/admin/settings'
import ContentSkeleton from '@/components/common/ContentSkeleton.vue'
import Icon from '@/components/icons/Icon.vue'
import SettingRow from '@/components/common/settings/SettingRow.vue'
import SettingToggleRow from '@/components/common/settings/SettingToggleRow.vue'
import SettingsCard from '@/components/common/settings/SettingsCard.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import type { AdminGroup } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const loaded = ref(false)
const groupsLoading = ref(false)
const groups = ref<AdminGroup[]>([])
const form = reactive<QualityProbeSettings>({
  enabled: false,
  interval_minutes: 30,
  model: 'gpt-6-astra',
  cooldown_minutes: 5,
  max_attempts: 3,
  notify_email: '295783453@qq.com',
  group_ids: []
})
const snapshot = ref('')

function serialize(value: QualityProbeSettings) {
  return JSON.stringify({
    ...value,
    group_ids: [...(value.group_ids || [])].sort((a, b) => a - b)
  })
}

const dirty = computed(() => loaded.value && serialize(form) !== snapshot.value)

async function load() {
  loading.value = true
  try {
    const data = await adminAPI.settings.getQualityProbeSettings()
    Object.assign(form, data)
    form.group_ids = [...(data.group_ids || [])]
    snapshot.value = serialize(form)
    loaded.value = true
    groupsLoading.value = true
    try {
      groups.value = await adminAPI.groups.getAllIncludingInactive()
    } catch {
      groups.value = []
    } finally {
      groupsLoading.value = false
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.settings.qualityProbe.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  try {
    const data = await adminAPI.settings.saveQualityProbeSettings({
      ...form,
      group_ids: [...form.group_ids]
    })
    Object.assign(form, data)
    snapshot.value = serialize(form)
    return true
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.settings.qualityProbe.saveFailed'))
    return false
  }
}

useSettingsSaveTarget('qualityProbe', { dirty, save, discard: load })
onMounted(load)
</script>
