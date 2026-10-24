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
      <SettingToggleRow
        id="quality-probe-schedule-enabled"
        v-model="form.schedule_enabled"
        :label="t('admin.settings.qualityProbe.scheduleEnabled')"
        :hint="t('admin.settings.qualityProbe.scheduleEnabledHint')"
      />
      <div v-if="form.schedule_enabled" class="grid gap-4 sm:grid-cols-2">
        <SettingRow
          id="quality-probe-schedule-start"
          field
          label-for="quality-probe-schedule-start"
          :label="t('admin.settings.qualityProbe.scheduleStart')"
          :hint="t('admin.settings.qualityProbe.scheduleStartHint')"
        >
          <input
            id="quality-probe-schedule-start"
            v-model="form.schedule_start"
            type="time"
            class="input"
          />
        </SettingRow>
        <SettingRow
          id="quality-probe-schedule-end"
          field
          label-for="quality-probe-schedule-end"
          :label="t('admin.settings.qualityProbe.scheduleEnd')"
          :hint="t('admin.settings.qualityProbe.scheduleEndHint')"
        >
          <input
            id="quality-probe-schedule-end"
            v-model="form.schedule_end"
            type="time"
            class="input"
          />
        </SettingRow>
      </div>
      <SettingToggleRow
        id="quality-probe-unschedule-on-degraded"
        v-model="form.unschedule_on_degraded"
        :label="t('admin.settings.qualityProbe.unscheduleOnDegraded')"
        :hint="t('admin.settings.qualityProbe.unscheduleOnDegradedHint')"
      />
      <div class="space-y-2">
        <div>
          <p class="text-sm font-medium text-primary-900 dark:text-dark-50">
            {{ t('admin.settings.qualityProbe.groups') }}
          </p>
          <p class="input-hint">{{ t('admin.settings.qualityProbe.groupsHint') }}</p>
        </div>
        <div
          class="max-h-40 overflow-auto rounded-control border border-gray-200 p-3 dark:border-dark-600"
        >
          <p v-if="groupsLoading" class="py-2 text-center text-xs text-gray-500">{{
            t('common.loading')
          }}</p>
          <p v-else-if="!groups.length" class="py-2 text-center text-xs text-gray-500">{{
            t('admin.pricing.form.noGroupsAvailable')
          }}</p>
          <div v-else class="flex flex-wrap gap-2">
            <label
              v-for="group in groups"
              :key="group.id"
              class="inline-flex max-w-full cursor-pointer items-center gap-2 rounded-control p-1.5 transition-colors hover:bg-gray-50 dark:hover:bg-dark-700"
              :class="[
                form.group_ids.includes(group.id)
                  ? 'bg-primary-50 dark:bg-primary-500/8 dark:text-primary-500'
                  : '',
                group.status === 'inactive' ? 'opacity-60' : '',
              ]"
            >
              <input
                v-model="form.group_ids"
                type="checkbox"
                :value="group.id"
                class="h-4 w-4 shrink-0 rounded-compact border-gray-300 text-primary-600 focus:ring-primary-500"
                @change="loadModels()"
              />
              <GroupBadge
                :name="group.name"
                :display-brand="group.display_brand"
                :rate-multiplier="group.rate_multiplier"
                class="min-w-0"
              />
              <span v-if="group.status === 'inactive'" class="shrink-0 text-xs text-gray-400">{{
                t('admin.settings.qualityProbe.groupInactive')
              }}</span>
            </label>
          </div>
        </div>
      </div>
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
        id="quality-probe-models"
        field
        label-for="quality-probe-models"
        :label="t('admin.settings.qualityProbe.model')"
        :hint="t('admin.settings.qualityProbe.modelHint')"
      >
        <ModelWhitelistSelector
          id="quality-probe-models"
          v-model="form.models"
          :models="models"
          :show-quick-actions="false"
          :allow-custom="false"
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
        <input id="quality-probe-email" v-model="form.notify_email" type="email" class="input" />
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
import ModelWhitelistSelector from '@/components/provider/ModelWhitelistSelector.vue'
import type { AdminGroup } from '@/api/groups.types'

// QualityProbeSettingsCard.vue edits automatic OpenAI quality-probe settings and
// registers its draft with the settings page save bar. Settings persist independently
// from the available group and model catalogs. Initial loading and discard retain
// saved model choices when eligible providers are absent or a catalog request fails,
// so saving another setting keeps the administrator's configured probe models.
//
// Group edits refresh candidates and filter selections against the requested scope.
// Each request has a generation token because responses can finish out of order.
// Successful initialization exposes controls after candidate loading completes.
// Later reloads capture the server baseline before catalog requests so intervening
// edits remain dirty when those requests finish.

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
// loaded exposes controls after the first settings and candidate requests finish;
// later refreshes leave controls mounted so pending user edits remain observable.
const loaded = ref(false)
const groupsLoading = ref(false)
const groups = ref<AdminGroup[]>([])
const models = ref<string[]>([])
const form = reactive<QualityProbeSettings>({
  enabled: false,
  interval_minutes: 30,
  models: ['gpt-6-astra'],
  cooldown_minutes: 5,
  max_attempts: 3,
  notify_email: '295783453@qq.com',
  group_ids: [],
  schedule_enabled: true,
  schedule_start: '08:00',
  schedule_end: '00:00',
  unschedule_on_degraded: true,
})
const snapshot = ref('')

function normalizeClock(value: string | undefined, fallback: string) {
  const raw = (value || '').trim()
  if (/^\d{2}:\d{2}$/.test(raw)) {
    return raw
  }
  // Native time controls can include seconds. Persist HH:MM because the server schedule parser uses minute precision.
  if (/^\d{2}:\d{2}:\d{2}$/.test(raw)) {
    return raw.slice(0, 5)
  }
  return fallback
}

function serialize(value: QualityProbeSettings) {
  return JSON.stringify({
    ...value,
    schedule_start: normalizeClock(value.schedule_start, '08:00'),
    schedule_end: normalizeClock(value.schedule_end, '00:00'),
    group_ids: [...(value.group_ids || [])].sort((a, b) => a - b),
    models: [...(value.models || [])],
  })
}

// modelRequestToken identifies the candidate lookup allowed to publish its result;
// restoring server settings also advances it to invalidate the abandoned draft.
let modelRequestToken = 0

// loadModels refreshes candidates and optionally filters the current model choices.
// Group changes use the default filtering behavior; server reloads pass false so
// temporarily unavailable models remain configured. Every lookup advances the
// request token before awaiting the API, and only the latest lookup may publish
// candidates or filter choices. A failed current lookup clears suggestions while
// retaining selected models, allowing other settings to be saved during catalog
// outages.
async function loadModels(filterSelection = true) {
  const token = ++modelRequestToken
  try {
    const candidates = await adminAPI.settings.getQualityProbeModels([...form.group_ids])
    // A later lookup or server reload owns the draft, so an older completion
    // cannot replace candidates or remove selections restored by that operation.
    if (token !== modelRequestToken) {
      return
    }
    models.value = candidates
    if (filterSelection) {
      form.models = form.models.filter((model) => candidates.includes(model))
    }
  } catch {
    // Keep configured selections when discovery fails because the server settings
    // remain valid independently of the currently available candidate list.
    if (token === modelRequestToken) {
      models.value = []
    }
  }
}

const dirty = computed(() => loaded.value && serialize(form) !== snapshot.value)

// load replaces the draft and its clean snapshot from persisted probe settings.
// Mounting, refresh, and the page discard action share this operation. Catalogs
// provide editing choices, while saved model IDs remain intact during discovery.
// Restoring the server draft invalidates earlier model requests before group
// discovery can yield, preventing abandoned selections from changing that draft.
//
// The snapshot is recorded before catalog requests so edits during a later reload
// remain dirty. On the first load, controls stay behind the skeleton until discovery
// finishes, preventing an unfinished initialization from accepting user edits.
async function load() {
  loading.value = true
  try {
    const data = await adminAPI.settings.getQualityProbeSettings()
    // Invalidate pending group lookups before replacing model selections so a
    // response from the abandoned draft cannot prune the restored configuration.
    modelRequestToken++
    Object.assign(form, {
      ...data,
      schedule_enabled: data.schedule_enabled !== false,
      schedule_start: normalizeClock(data.schedule_start, '08:00'),
      schedule_end: normalizeClock(data.schedule_end, '00:00'),
      unschedule_on_degraded: data.unschedule_on_degraded !== false,
      group_ids: [...(data.group_ids || [])],
      models: [...(data.models || (data.model ? [data.model] : []))],
    })
    // Establish the server baseline before awaiting catalogs so edits during an
    // already mounted refresh remain distinguishable when those requests finish.
    snapshot.value = serialize(form)
    groupsLoading.value = true
    try {
      groups.value = await adminAPI.groups.getAllIncludingInactive()
    } catch {
      // A missing group catalog leaves the saved scope available for persistence;
      // the empty list lets the remaining settings controls finish loading.
      groups.value = []
    } finally {
      groupsLoading.value = false
    }
    await loadModels(false)
    loaded.value = true
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
      schedule_start: normalizeClock(form.schedule_start, '08:00'),
      schedule_end: normalizeClock(form.schedule_end, '00:00'),
      group_ids: [...form.group_ids],
      models: [...form.models],
    })
    Object.assign(form, {
      ...data,
      schedule_enabled: data.schedule_enabled !== false,
      schedule_start: normalizeClock(data.schedule_start, '08:00'),
      schedule_end: normalizeClock(data.schedule_end, '00:00'),
      unschedule_on_degraded: data.unschedule_on_degraded !== false,
      group_ids: [...(data.group_ids || [])],
      models: [...(data.models || (data.model ? [data.model] : []))],
    })
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
