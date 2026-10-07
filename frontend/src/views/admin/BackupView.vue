<template>
    <div class="space-y-4">
      <!-- 备份存储配置 -->
      <SettingsCard
        :title="t('admin.backup.storage.title')"
        :description="t('admin.backup.storage.description')"
      >
        <template #actions>
          <button type="button" class="btn btn-secondary btn-sm h-9" :disabled="testingStorage" @click="testStorage">
            {{ testingStorage ? t('common.loading') : t('admin.backup.storage.testConnection') }}
          </button>
        </template>
        <SettingsSection>
          <SettingsSegmented
            v-model="storageForm.type"
            :options="storageTypeOptions"
            :ariaLabel="t('admin.backup.storage.title')"
          />

          <div v-if="storageForm.type === 'local'">
            <label for="backup-local-path" class="input-label">{{ t('admin.backup.storage.localPath') }}</label>
            <input
              id="backup-local-path"
              :value="storageForm.local_path || '-'"
              class="input font-mono text-sm"
              readonly
            />
            <p class="input-hint">{{ t('admin.backup.storage.localHint') }}</p>
          </div>

          <template v-else>
            <p class="input-hint mt-0">
              {{ t('admin.backup.s3.descriptionPrefix') }}
              <button
                type="button"
                class="text-primary-600 underline hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                @click="showR2Guide = true"
              >Cloudflare R2</button>
              {{ t('admin.backup.s3.descriptionSuffix') }}
            </p>
            <div class="grid gap-4 md:grid-cols-2">
              <div>
                <label for="backup-s3-endpoint" class="input-label">{{ t('admin.backup.s3.endpoint') }}</label>
                <input
                  id="backup-s3-endpoint"
                  v-model="s3Form.endpoint"
                  class="input font-mono text-sm"
                  placeholder="https://<account_id>.r2.cloudflarestorage.com"
                />
              </div>
              <div>
                <label for="backup-s3-region" class="input-label">{{ t('admin.backup.s3.region') }}</label>
                <input id="backup-s3-region" v-model="s3Form.region" class="input" placeholder="auto" />
              </div>
              <div>
                <label for="backup-s3-bucket" class="input-label">{{ t('admin.backup.s3.bucket') }}</label>
                <input id="backup-s3-bucket" v-model="s3Form.bucket" class="input" />
              </div>
              <div>
                <label for="backup-s3-prefix" class="input-label">{{ t('admin.backup.s3.prefix') }}</label>
                <input id="backup-s3-prefix" v-model="s3Form.prefix" class="input" placeholder="backups/" />
              </div>
              <div>
                <label for="backup-s3-access-key-id" class="input-label">{{ t('admin.backup.s3.accessKeyId') }}</label>
                <input id="backup-s3-access-key-id" v-model="s3Form.access_key_id" class="input font-mono text-sm" />
              </div>
              <div>
                <label for="backup-s3-secret-access-key" class="input-label">{{ t('admin.backup.s3.secretAccessKey') }}</label>
                <input
                  id="backup-s3-secret-access-key"
                  v-model="s3Form.secret_access_key"
                  type="password"
                  class="input font-mono text-sm"
                  :placeholder="s3SecretConfigured ? t('admin.backup.s3.secretConfigured') : ''"
                />
              </div>
            </div>
            <SettingToggleRow
              id="backup-s3-force-path-style"
              v-model="s3Form.force_path_style"
              :label="t('admin.backup.s3.forcePathStyle')"
            />
            <div>
              <p class="input-label">{{ t('admin.backup.s3.uploadMode') }}</p>
              <SettingsSegmented
                v-model="s3Form.upload_mode"
                :options="uploadModeOptions"
                :ariaLabel="t('admin.backup.s3.uploadMode')"
              />
              <p class="input-hint">{{ t('admin.backup.s3.uploadModeHint') }}</p>
            </div>
          </template>
        </SettingsSection>
      </SettingsCard>

      <!-- 备份内容配置 -->
      <SettingsCard
        :title="t('admin.backup.content.title')"
        :description="t('admin.backup.content.description')"
      >
        <SettingsSection>
          <SettingToggleRow
            v-for="option in contentOptions"
            :id="`backup-content-${option.key}`"
            :key="option.key"
            v-model="contentForm[option.key]"
            :label="option.title"
            :hint="option.description"
          />
          <p class="input-hint">
            {{ t('admin.backup.content.excludedCount', { count: contentExcludedCount }) }}
          </p>
        </SettingsSection>
      </SettingsCard>

      <!-- 定时备份配置 -->
      <SettingsCard
        :title="t('admin.backup.schedule.title')"
        :description="t('admin.backup.schedule.description')"
      >
        <SettingsSection>
          <SettingToggleRow
            id="backup-schedule-enabled"
            v-model="scheduleForm.enabled"
            :label="t('admin.backup.schedule.enabled')"
          />
          <Collapse :open="scheduleForm.enabled">
            <SettingsSubpanel>
              <SettingRow
                id="backup-schedule-cron"
                field
                label-for="backup-schedule-cron"
                :label="t('admin.backup.schedule.cronExpr')"
                :hint="t('admin.backup.schedule.cronHint')"
              >
                <input
                  id="backup-schedule-cron"
                  v-model="scheduleForm.cron_expr"
                  class="input font-mono text-sm"
                  placeholder="0 2 * * *"
                />
              </SettingRow>
              <SettingRow
                id="backup-schedule-retain-days"
                field
                label-for="backup-schedule-retain-days"
                :label="t('admin.backup.schedule.retainDays')"
                :hint="t('admin.backup.schedule.retainDaysHint')"
              >
                <input
                  id="backup-schedule-retain-days"
                  v-model.number="scheduleForm.retain_days"
                  type="number"
                  min="0"
                  class="input"
                />
              </SettingRow>
              <SettingRow
                id="backup-schedule-retain-count"
                field
                label-for="backup-schedule-retain-count"
                :label="t('admin.backup.schedule.retainCount')"
                :hint="t('admin.backup.schedule.retainCountHint')"
              >
                <input
                  id="backup-schedule-retain-count"
                  v-model.number="scheduleForm.retain_count"
                  type="number"
                  min="0"
                  class="input"
                />
              </SettingRow>
            </SettingsSubpanel>
          </Collapse>
        </SettingsSection>
      </SettingsCard>

      <!-- 备份记录 -->
      <SettingsCard
        :title="t('admin.backup.operations.title')"
        :description="t('admin.backup.operations.description')"
      >
        <template #actions>
          <div class="flex flex-wrap items-center gap-2">
            <label for="backup-manual-expire-days" class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.backup.operations.expireDays') }}
            </label>
            <input
              id="backup-manual-expire-days"
              v-model.number="manualExpireDays"
              type="number"
              min="0"
              class="input w-20"
            />
            <button type="button" class="btn btn-primary btn-sm h-9" :disabled="creatingBackup" @click="createBackup">
              {{ creatingBackup ? t('admin.backup.operations.backing') : t('admin.backup.operations.createBackup') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm h-9" :disabled="loadingBackups" @click="loadBackups">
              {{ loadingBackups ? t('common.loading') : t('common.refresh') }}
            </button>
          </div>
        </template>

        <div class="overflow-x-auto">
          <table class="w-full min-w-[800px] text-sm">
            <thead>
              <tr class="border-b border-gray-200 text-left text-xs tracking-wide text-gray-500 dark:border-dark-700 dark:text-dark-400">
                <th class="py-2 pr-4">ID</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.status') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.storage') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.fileName') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.size') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.parts') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.expiresAt') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.triggeredBy') }}</th>
                <th class="py-2 pr-4">{{ t('admin.backup.columns.startedAt') }}</th>
                <th class="py-2">{{ t('admin.backup.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="record in backups" :key="record.id" class="border-b border-gray-100 align-top dark:border-dark-700">
                <td class="py-3 pr-4 font-mono text-xs">{{ record.id }}</td>
                <td class="py-3 pr-4">
                  <span
                    class="rounded-compact px-2 py-0.5 text-xs"
                    :class="statusClass(record.status)"
                  >
                    {{ record.status === 'running' && record.progress
                      ? t(`admin.backup.progress.${record.progress}`)
                      : t(`admin.backup.status.${record.status}`) }}
                  </span>
                </td>
                <td class="py-3 pr-4 text-xs">{{ storageLabel(record) }}</td>
                <td class="py-3 pr-4 text-xs">{{ record.file_name }}</td>
                <td class="py-3 pr-4 text-xs">{{ formatSize(record.size_bytes) }}</td>
                <td class="py-3 pr-4 text-xs">{{ record.parts?.length || 1 }}</td>
                <td class="py-3 pr-4 text-xs">
                  {{ record.expires_at ? formatDate(record.expires_at) : t('admin.backup.neverExpire') }}
                </td>
                <td class="py-3 pr-4 text-xs">
                  {{ record.triggered_by === 'scheduled' ? t('admin.backup.trigger.scheduled') : t('admin.backup.trigger.manual') }}
                </td>
                <td class="py-3 pr-4 text-xs">{{ formatDate(record.started_at) }}</td>
                <td class="py-3 text-xs">
                  <div class="flex flex-wrap gap-1">
                    <button
                      v-if="record.status === 'completed'"
                      type="button"
                      class="btn btn-secondary btn-xs"
                      @click="downloadBackup(record.id)"
                    >
                      {{ t('admin.backup.actions.download') }}
                    </button>
                    <button
                      v-if="record.status === 'completed'"
                      type="button"
                      class="btn btn-secondary btn-xs"
                      :disabled="restoringId === record.id"
                      @click="restoreBackup(record.id)"
                    >
                      {{ restoringId === record.id ? t('common.loading') : t('admin.backup.actions.restore') }}
                    </button>
                    <button
                      type="button"
                      class="btn btn-danger btn-xs"
                      @click="removeBackup(record.id)"
                    >
                      {{ t('common.delete') }}
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="backups.length === 0">
                <td colspan="10" class="py-6 text-center text-sm text-gray-500 dark:text-dark-400">
                  {{ t('admin.backup.empty') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </SettingsCard>
    </div>

    <!-- Cloudflare R2 配置教程弹窗 -->
    <teleport to="body">
      <MotionTransition name="modal">
        <div v-if="showR2Guide" class="fixed inset-0 z-50 flex items-center justify-center p-4" @mousedown.self="showR2Guide = false">
          <div class="fixed inset-0 bg-[var(--overlay-bg)]" @click="showR2Guide = false"></div>
          <div class="relative max-h-[85vh] w-full max-w-2xl overflow-y-auto rounded-surface bg-white p-6 shadow-2xl dark:bg-dark-800 sm:rounded-dialog">
            <button type="button" class="absolute right-4 top-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200" @click="showR2Guide = false">
              <Icon name="x" size="sm" />
            </button>

            <h2 class="mb-4 text-lg font-bold text-gray-900 dark:text-white">{{ t('admin.backup.r2Guide.title') }}</h2>
            <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.backup.r2Guide.intro') }}</p>

            <!-- 步骤 1 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">1</span>
                {{ t('admin.backup.r2Guide.step1.title') }}
              </h3>
              <ol class="ml-8 list-decimal space-y-1 text-sm text-gray-600 dark:text-gray-300">
                <li>{{ t('admin.backup.r2Guide.step1.line1') }}</li>
                <li>{{ t('admin.backup.r2Guide.step1.line2') }}</li>
                <li>{{ t('admin.backup.r2Guide.step1.line3') }}</li>
              </ol>
            </div>

            <!-- 步骤 2 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">2</span>
                {{ t('admin.backup.r2Guide.step2.title') }}
              </h3>
              <ol class="ml-8 list-decimal space-y-1 text-sm text-gray-600 dark:text-gray-300">
                <li>{{ t('admin.backup.r2Guide.step2.line1') }}</li>
                <li>{{ t('admin.backup.r2Guide.step2.line2') }}</li>
                <li>{{ t('admin.backup.r2Guide.step2.line3') }}</li>
                <li>{{ t('admin.backup.r2Guide.step2.line4') }}</li>
              </ol>
              <div class="mt-2 rounded-control bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                {{ t('admin.backup.r2Guide.step2.warning') }}
              </div>
            </div>

            <!-- 步骤 3 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">3</span>
                {{ t('admin.backup.r2Guide.step3.title') }}
              </h3>
              <p class="ml-8 text-sm text-gray-600 dark:text-gray-300">{{ t('admin.backup.r2Guide.step3.desc') }}</p>
              <code class="ml-8 mt-1 block rounded-compact bg-gray-100 px-3 py-2 text-xs text-gray-800 dark:bg-dark-700 dark:text-gray-200">https://&lt;{{ t('admin.backup.r2Guide.step3.accountId') }}&gt;.r2.cloudflarestorage.com</code>
            </div>

            <!-- 步骤 4：填写表单 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">4</span>
                {{ t('admin.backup.r2Guide.step4.title') }}
              </h3>
              <div class="ml-8 overflow-hidden rounded-control border border-gray-200 dark:border-dark-600">
                <table class="w-full text-sm">
                  <tbody>
                    <tr v-for="(row, i) in r2ConfigRows" :key="i" class="border-b border-gray-100 dark:border-dark-700 last:border-0">
                      <td class="whitespace-nowrap bg-gray-50 px-3 py-2 font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-300">{{ row.field }}</td>
                      <td class="px-3 py-2 text-gray-600 dark:text-gray-400"><code class="text-xs">{{ row.value }}</code></td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <!-- 免费额度说明 -->
            <div class="rounded-control bg-green-50 p-3 text-xs text-green-700 dark:bg-green-900/20 dark:text-green-300">
              {{ t('admin.backup.r2Guide.freeTier') }}
            </div>

            <div class="mt-4 text-right">
              <button type="button" class="btn btn-primary btn-sm" @click="showR2Guide = false">{{ t('common.close') }}</button>
            </div>
          </div>
        </div>
      </MotionTransition>
    </teleport>
    <!-- 分卷下载链接 -->
    <teleport to="body">
      <MotionTransition name="modal">
        <div
          v-if="downloadPartsModalOpen"
          class="fixed inset-0 z-50 flex items-center justify-center p-4"
          @mousedown.self="closeDownloadParts"
        >
          <div class="fixed inset-0 bg-[var(--overlay-bg)]" @click="closeDownloadParts"></div>
          <div class="relative max-h-[85vh] w-full max-w-lg overflow-y-auto rounded-surface bg-white p-6 shadow-2xl dark:bg-dark-800 sm:rounded-dialog">
            <button
              type="button"
              class="absolute right-4 top-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
              :aria-label="t('common.close')"
              @click="closeDownloadParts"
            >
              <Icon name="x" size="sm" />
            </button>
            <h2 class="mb-1 text-lg font-bold text-gray-900 dark:text-white">
              {{ t('admin.backup.actions.downloadParts') }}
            </h2>
            <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.backup.actions.downloadPartsHint') }}
            </p>
            <div class="space-y-2">
              <div
                v-for="part in downloadParts"
                :key="part.index"
                class="flex items-center justify-between gap-3 rounded-control border border-gray-200 px-3 py-2 dark:border-dark-600"
              >
                <span class="text-sm text-gray-700 dark:text-gray-300">
                  {{ t('admin.backup.actions.partLabel', { index: part.index }) }}
                  <span class="ml-2 text-xs text-gray-500 dark:text-gray-400">{{ formatSize(part.size_bytes) }}</span>
                </span>
                <a :href="part.url" class="btn btn-secondary btn-xs" rel="noopener">
                  {{ t('admin.backup.actions.download') }}
                </a>
              </div>
            </div>
          </div>
        </div>
      </MotionTransition>
    </teleport>
    <TotpStepUpDialog :controller="backupStepUp" />
</template>

<script setup lang="ts">
import MotionTransition from '@/components/common/MotionTransition.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores'
import type {
  BackupContentConfig,
  BackupDownloadPart,
  BackupS3Config,
  BackupScheduleConfig,
  BackupRecord,
  BackupStorageConfig,
} from '@/api/admin/backup'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useDirtyTracker } from '@/composables/useDirtyTracker'
import { useSettingsSaveTarget } from '@/composables/useSettingsSaveRegistry'
import Collapse from '@/components/common/Collapse.vue'
import SettingRow from '@/components/common/settings/SettingRow.vue'
import SettingToggleRow from '@/components/common/settings/SettingToggleRow.vue'
import SettingsCard from '@/components/common/settings/SettingsCard.vue'
import SettingsSection from '@/components/common/settings/SettingsSection.vue'
import SettingsSegmented, { type SettingsSegmentedOption } from '@/components/common/settings/SettingsSegmented.vue'
import SettingsSubpanel from '@/components/common/settings/SettingsSubpanel.vue'

const { t } = useI18n()
const appStore = useAppStore()

const storageTypeOptions = computed<SettingsSegmentedOption<BackupStorageConfig['type']>[]>(() => [
  { value: 'local', label: t('admin.backup.storage.local') },
  { value: 's3', label: t('admin.backup.storage.remote') },
])
const uploadModeOptions = computed<SettingsSegmentedOption<NonNullable<BackupS3Config['upload_mode']>>[]>(() => [
  { value: 'multipart', label: t('admin.backup.s3.uploadModeMultipart') },
  { value: 'spooled_put', label: t('admin.backup.s3.uploadModeSpooled') },
])
const backupStepUp = useStepUp()

// 敏感操作被 2FA 门控拦截时的统一提示。
function reportStepUpBlocked(error: unknown): boolean {
  if (!isStepUpBlocked(error)) return false
  appStore.showError(
    stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
      ? t('stepUp.adminApiKeyForbidden')
      : t('stepUp.notEnabled')
  )
  return true
}

// 存储配置
const storageForm = ref<BackupStorageConfig>({
  type: 'local',
  local_path: '',
  s3: {
    endpoint: '',
    region: 'auto',
    bucket: '',
    access_key_id: '',
    secret_access_key: '',
    prefix: 'backups/',
    force_path_style: false,
    upload_mode: 'spooled_put',
  },
})
const s3Form = ref<BackupS3Config>({
  endpoint: '',
  region: 'auto',
  bucket: '',
  access_key_id: '',
  secret_access_key: '',
  prefix: 'backups/',
  force_path_style: false,
  upload_mode: 'spooled_put',
})
const s3SecretConfigured = ref(false)
const testingStorage = ref(false)

// 备份内容配置
const contentForm = ref<BackupContentConfig>({
  include_usage_records: false,
	include_ops_logs: false,
	include_audit_logs: false,
	include_runtime_data: false,
	excluded_table_data: [],
})
type BackupContentOptionKey = keyof Pick<BackupContentConfig, 'include_usage_records' | 'include_ops_logs' | 'include_audit_logs' | 'include_runtime_data'>
const contentTablePatternCounts: Record<BackupContentOptionKey, number> = {
  include_usage_records: 10,
  include_ops_logs: 9,
  include_audit_logs: 5,
	include_runtime_data: 6,
}
const contentOptions = computed<Array<{ key: BackupContentOptionKey, title: string, description: string }>>(() => [
  {
    key: 'include_usage_records',
    title: t('admin.backup.content.usageRecords.title'),
    description: t('admin.backup.content.usageRecords.description'),
  },
  {
    key: 'include_ops_logs',
    title: t('admin.backup.content.opsLogs.title'),
    description: t('admin.backup.content.opsLogs.description'),
  },
  {
    key: 'include_audit_logs',
    title: t('admin.backup.content.auditLogs.title'),
    description: t('admin.backup.content.auditLogs.description'),
  },
  {
    key: 'include_runtime_data',
    title: t('admin.backup.content.runtimeData.title'),
    description: t('admin.backup.content.runtimeData.description'),
  },
])
const contentExcludedCount = computed(() => (
  contentOptions.value.reduce((total, option) => (
    contentForm.value[option.key] ? total : total + contentTablePatternCounts[option.key]
  ), 0)
))

// 定时备份配置
const scheduleForm = ref<BackupScheduleConfig>({
  enabled: false,
  cron_expr: '0 2 * * *',
  retain_days: 14,
  retain_count: 10,
})

// 备份记录
const backups = ref<BackupRecord[]>([])
const loadingBackups = ref(false)
const creatingBackup = ref(false)
const restoringId = ref('')
const manualExpireDays = ref(14)
const downloadParts = ref<BackupDownloadPart[]>([])
const downloadPartsModalOpen = ref(false)

// 轮询状态
const pollingTimer = ref<ReturnType<typeof setInterval> | null>(null)
const restoringPollingTimer = ref<ReturnType<typeof setInterval> | null>(null)
const MAX_POLL_COUNT = 900

function updateRecordInList(updated: BackupRecord) {
  const idx = backups.value.findIndex(r => r.id === updated.id)
  if (idx >= 0) {
    backups.value[idx] = updated
  }
}

function startPolling(backupId: string) {
  stopPolling()
  let count = 0
  pollingTimer.value = setInterval(async () => {
    if (count++ >= MAX_POLL_COUNT) {
      stopPolling()
      creatingBackup.value = false
      appStore.showWarning(t('admin.backup.operations.backupRunning'))
      return
    }
    try {
      const record = await adminAPI.backup.getBackup(backupId)
      updateRecordInList(record)
      if (record.status === 'completed' || record.status === 'failed') {
        stopPolling()
        creatingBackup.value = false
        if (record.status === 'completed') {
          appStore.showSuccess(t('admin.backup.operations.backupCreated'))
        } else {
          appStore.showError(record.error_message || t('admin.backup.operations.backupFailed'))
        }
        await loadBackups()
      }
    } catch {
      // 轮询失败时不中断
    }
  }, 2000)
}

function stopPolling() {
  if (pollingTimer.value) {
    clearInterval(pollingTimer.value)
    pollingTimer.value = null
  }
}

function startRestorePolling(backupId: string) {
  stopRestorePolling()
  let count = 0
  restoringPollingTimer.value = setInterval(async () => {
    if (count++ >= MAX_POLL_COUNT) {
      stopRestorePolling()
      restoringId.value = ''
      appStore.showWarning(t('admin.backup.operations.restoreRunning'))
      return
    }
    try {
      const record = await adminAPI.backup.getBackup(backupId)
      updateRecordInList(record)
      if (record.restore_status === 'completed' || record.restore_status === 'failed') {
        stopRestorePolling()
        restoringId.value = ''
        if (record.restore_status === 'completed') {
          appStore.showSuccess(t('admin.backup.actions.restoreSuccess'))
        } else {
          appStore.showError(record.restore_error || t('admin.backup.operations.restoreFailed'))
        }
        await loadBackups()
      }
    } catch {
      // 轮询失败时不中断
    }
  }, 2000)
}

function stopRestorePolling() {
  if (restoringPollingTimer.value) {
    clearInterval(restoringPollingTimer.value)
    restoringPollingTimer.value = null
  }
}

function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling()
    stopRestorePolling()
  } else {
    // 标签页恢复时刷新列表，检查是否仍有活跃操作
    loadBackups().then(() => {
      const running = backups.value.find(r => r.status === 'running')
      if (running) {
        creatingBackup.value = true
        startPolling(running.id)
      }
      const restoring = backups.value.find(r => r.restore_status === 'running')
      if (restoring) {
        restoringId.value = restoring.id
        startRestorePolling(restoring.id)
      }
    })
  }
}

// R2 配置教程
const showR2Guide = ref(false)
const r2ConfigRows = computed(() => [
  { field: t('admin.backup.s3.endpoint'), value: 'https://<account_id>.r2.cloudflarestorage.com' },
  { field: t('admin.backup.s3.region'), value: 'auto' },
  { field: t('admin.backup.s3.bucket'), value: t('admin.backup.r2Guide.step4.bucketValue') },
  { field: t('admin.backup.s3.prefix'), value: 'backups/' },
  { field: 'Access Key ID', value: t('admin.backup.r2Guide.step4.fromStep2') },
  { field: 'Secret Access Key', value: t('admin.backup.r2Guide.step4.fromStep2') },
  { field: t('admin.backup.s3.forcePathStyle'), value: t('admin.backup.r2Guide.step4.unchecked') },
])

function normalizeS3Form(cfg?: Partial<BackupS3Config>): BackupS3Config {
  return {
    endpoint: cfg?.endpoint || '',
    region: cfg?.region || 'auto',
    bucket: cfg?.bucket || '',
    access_key_id: cfg?.access_key_id || '',
    secret_access_key: '',
    prefix: cfg?.prefix || 'backups/',
    force_path_style: Boolean(cfg?.force_path_style),
    upload_mode: cfg?.upload_mode === 'multipart' ? 'multipart' : 'spooled_put',
  }
}

function buildStoragePayload(): BackupStorageConfig {
  // 提交时必须保留用户在密码框中输入的新 Secret，只有服务端回填表单时才清空。
  const s3Payload = normalizeS3Form(s3Form.value)
  s3Payload.secret_access_key = s3Form.value.secret_access_key || ''
  return {
    type: storageForm.value.type,
    local_path: storageForm.value.local_path,
    s3: s3Payload,
  }
}

async function loadStorageConfig() {
  try {
    const cfg = await adminAPI.backup.getStorageConfig()
    storageForm.value = {
      type: cfg.type || 'local',
      local_path: cfg.local_path || '',
      s3: normalizeS3Form(cfg.s3),
    }
    s3Form.value = normalizeS3Form(cfg.s3)
    s3SecretConfigured.value = Boolean(cfg.s3?.access_key_id)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
  await nextTick()
  markClean('storage')
}

// 保存函数返回是否保存成功，由系统设置页的吸底保存条统一调用。
async function saveStorageConfig(): Promise<boolean> {
  try {
    await backupStepUp.run(() => adminAPI.backup.updateStorageConfig(buildStoragePayload()))
    await loadStorageConfig()
    return true
  } catch (error) {
    if (isStepUpCancelled(error)) return false
    if (reportStepUpBlocked(error)) return false
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
    return false
  }
}

async function testStorage() {
  testingStorage.value = true
  try {
    const result = await adminAPI.backup.testStorageConnection(buildStoragePayload())
    if (result.ok) {
      appStore.showSuccess(result.message || t('admin.backup.storage.testSuccess'))
    } else {
      appStore.showError(result.message || t('admin.backup.storage.testFailed'))
    }
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    testingStorage.value = false
  }
}

function normalizeContentConfig(cfg?: Partial<BackupContentConfig>): BackupContentConfig {
  return {
    include_usage_records: Boolean(cfg?.include_usage_records),
    include_ops_logs: Boolean(cfg?.include_ops_logs),
    include_audit_logs: Boolean(cfg?.include_audit_logs),
    include_runtime_data: Boolean(cfg?.include_runtime_data),
    excluded_table_data: cfg?.excluded_table_data || [],
  }
}

async function loadContentConfig() {
  try {
    const cfg = await adminAPI.backup.getContentConfig()
    contentForm.value = normalizeContentConfig(cfg)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
  await nextTick()
  markClean('content')
}

async function saveContentConfig(): Promise<boolean> {
  try {
    const cfg = await adminAPI.backup.updateContentConfig(normalizeContentConfig(contentForm.value))
    contentForm.value = normalizeContentConfig(cfg)
    await nextTick()
    markClean('content')
    return true
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
    return false
  }
}

async function loadSchedule() {
  try {
    const cfg = await adminAPI.backup.getSchedule()
    scheduleForm.value = {
      enabled: cfg.enabled,
      cron_expr: cfg.cron_expr || '0 2 * * *',
      retain_days: cfg.retain_days || 14,
      retain_count: cfg.retain_count || 10,
    }
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
  await nextTick()
  markClean('schedule')
}

async function saveSchedule(): Promise<boolean> {
  try {
    await adminAPI.backup.updateSchedule(scheduleForm.value)
    markClean('schedule')
    return true
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
    return false
  }
}

async function loadBackups() {
  loadingBackups.value = true
  try {
    const result = await adminAPI.backup.listBackups()
    backups.value = result.items || []
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  } finally {
    loadingBackups.value = false
  }
}

async function createBackup() {
  creatingBackup.value = true
  try {
    const record = await backupStepUp.run(() => adminAPI.backup.createBackup({ expire_days: manualExpireDays.value }))
    // 插入到列表顶部
    backups.value.unshift(record)
    startPolling(record.id)
  } catch (error: any) {
    if (isStepUpCancelled(error)) {
      creatingBackup.value = false
      return
    }
    if (reportStepUpBlocked(error)) {
      creatingBackup.value = false
      return
    }
    if (error?.response?.status === 409) {
      appStore.showWarning(t('admin.backup.operations.alreadyInProgress'))
    } else {
      appStore.showError(error?.message || t('errors.networkError'))
    }
    creatingBackup.value = false
  }
}

async function downloadBackup(id: string) {
  try {
    const record = backups.value.find(item => item.id === id)
    if (recordStorageType(record) === 'local' && !record?.parts?.length) {
      const blob = await backupStepUp.run(() => adminAPI.backup.downloadBackupFile(id))
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = record?.file_name || `${id}.sql.gz`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
      return
    }
    const result = await backupStepUp.run(() => adminAPI.backup.getDownloadURL(id))
    if (result.parts && result.parts.length > 0) {
      downloadParts.value = result.parts
      downloadPartsModalOpen.value = true
      return
    }
    if (!result.url) {
      throw new Error(t('admin.backup.actions.downloadFailed'))
    }
    // 预签名 URL 带 attachment disposition，同页 anchor 导航直接触发下载；
    // 不用 window.open：step-up 弹窗 await 会耗尽瞬态用户激活，新标签页会被浏览器拦截。
    const link = document.createElement('a')
    link.href = result.url
    link.rel = 'noopener'
    link.click()
  } catch (error) {
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

function closeDownloadParts() {
  downloadPartsModalOpen.value = false
  downloadParts.value = []
}

async function restoreBackup(id: string) {
  if (!window.confirm(t('admin.backup.actions.restoreConfirm'))) return
  const password = window.prompt(t('admin.backup.actions.restorePasswordPrompt'))
  if (!password) return
  restoringId.value = id
  try {
    const record = await backupStepUp.run(() => adminAPI.backup.restoreBackup(id, password))
    updateRecordInList(record)
    startRestorePolling(id)
  } catch (error: any) {
    restoringId.value = ''
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    // apiClient 拦截器把 HTTP 错误归一化为顶层 { status } 平面对象（无 response 字段）
    if (error?.status === 409 || error?.response?.status === 409) {
      appStore.showWarning(t('admin.backup.operations.restoreRunning'))
    } else {
      appStore.showError(error?.message || t('errors.networkError'))
    }
  }
}

async function removeBackup(id: string) {
  if (!window.confirm(t('admin.backup.actions.deleteConfirm'))) return
  try {
    await adminAPI.backup.deleteBackup(id)
    appStore.showSuccess(t('admin.backup.actions.deleted'))
    await loadBackups()
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('errors.networkError'))
  }
}

function statusClass(status: string): string {
  switch (status) {
    case 'completed':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'running':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-gray-300'
  }
}

function recordStorageType(record?: BackupRecord): 'local' | 's3' {
  if (!record) return 's3'
  if (record.parts?.length) return 's3'
  if (record.storage_type === 'local' || (!record.storage_type && !record.s3_key)) return 'local'
  return 's3'
}

function storageLabel(record: BackupRecord): string {
  return recordStorageType(record) === 'local'
    ? t('admin.backup.storage.local')
    : t('admin.backup.storage.remote')
}

function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatDate(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

// 三块配置各自调用接口保存，分别登记到系统设置页的吸底保存条。
const { isDirty, markClean } = useDirtyTracker({
  storage: () => ({ type: storageForm.value.type, s3: s3Form.value }),
  content: () => contentForm.value,
  schedule: () => scheduleForm.value,
})
useSettingsSaveTarget('backupStorage', { dirty: computed(() => isDirty('storage')), save: saveStorageConfig, discard: loadStorageConfig })
useSettingsSaveTarget('backupContent', { dirty: computed(() => isDirty('content')), save: saveContentConfig, discard: loadContentConfig })
useSettingsSaveTarget('backupSchedule', { dirty: computed(() => isDirty('schedule')), save: saveSchedule, discard: loadSchedule })

onMounted(async () => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await Promise.all([
    loadStorageConfig(),
    loadContentConfig(),
    loadSchedule(),
    loadBackups(),
  ])

  // 如果有正在 running 的备份，恢复轮询
  const runningBackup = backups.value.find(r => r.status === 'running')
  if (runningBackup) {
    creatingBackup.value = true
    startPolling(runningBackup.id)
  }
  const restoringBackup = backups.value.find(r => r.restore_status === 'running')
  if (restoringBackup) {
    restoringId.value = restoringBackup.id
    startRestorePolling(restoringBackup.id)
  }
})

onBeforeUnmount(() => {
  stopPolling()
  stopRestorePolling()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity var(--motion-normal) var(--motion-ease);
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>
