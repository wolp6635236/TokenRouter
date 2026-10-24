import { describe, expect, it } from 'vitest'

// Catalog strings need the compiler-enabled build for translation in this test.
// The application alias points at a runtime build without a message compiler.
import { createI18n } from '../../../node_modules/vue-i18n/dist/vue-i18n.mjs'

import en from '../locales/en'
import zh from '../locales/zh'

// qualityProbeLocales.spec.ts checks the runtime messages used by quality-probe
// settings, provider actions, results, and history. Both catalogs resolve the
// complete interface vocabulary, including server skip reasons and concurrent
// model progress, so missing translations cannot surface as message paths.

// Required message groups mirror the quality-probe consumers, including paths
// assembled dynamically for backend skip reasons and provider result badges.
const requiredMessageGroups = {
  'admin.providers': [
    'qualityProbe',
    'qualityProbeStart',
    'qualityProbeModelsEmpty',
    'qualityProbeModelsLoadFailed',
    'qualityProbeSkipped',
    'qualityProbeFailed',
    'columns.qualityProbe',
  ],
  'admin.providers.qualityProbeList': ['passed', 'degraded', 'upstreamError', 'skipped'],
  'admin.providers.qualityProbeLogs': [
    'title',
    'description',
    'time',
    'provider',
    'trigger',
    'triggerAuto',
    'triggerManual',
    'candy',
    'trace',
    'traceN',
    'verdict',
    'verdictPass',
    'verdictDegraded',
    'verdictUpstreamError',
    'verdictKept',
    'verdictSkipped',
    'model',
    'view',
    'fingerprint',
    'detailTitle',
    'prompt',
    'answer',
    'empty',
    'loadFailed',
  ],
  'admin.providers.qualityProbeDialog': [
    'title',
    'running',
    'runningMany',
    'runningHint',
    'pending',
    'requestFailed',
    'samples',
    'passed',
    'degraded',
    'upstreamError',
    'kept',
    'candy',
    'trace',
    'pass',
    'fail',
    'consecutive',
    'tempUnscheduled',
    'cycleStopped',
    'emailSent',
    'close',
  ],
  'admin.providers.qualityProbeDialog.skipReasons': [
    'disabled',
    'platform',
    'decide',
    'cycle_stopped',
    'not_due',
    'group',
    'account',
    'model',
    'schedule',
  ],
  'admin.settings.qualityProbe': [
    'title',
    'description',
    'enabled',
    'enabledHint',
    'scheduleEnabled',
    'scheduleEnabledHint',
    'scheduleStart',
    'scheduleStartHint',
    'scheduleEnd',
    'scheduleEndHint',
    'unscheduleOnDegraded',
    'unscheduleOnDegradedHint',
    'intervalMinutes',
    'intervalHint',
    'model',
    'modelHint',
    'cooldownMinutes',
    'cooldownHint',
    'maxAttempts',
    'maxAttemptsHint',
    'notifyEmail',
    'notifyEmailHint',
    'groups',
    'groupsHint',
    'groupInactive',
    'loadFailed',
    'saveFailed',
  ],
}

describe('quality-probe runtime translations', () => {
  // Quality-probe pages and result dialogs request these paths from each
  // language. Resolving every path through Vue I18n detects absent entries even
  // when component tests replace translation functions with message-key stubs.
  it.each(['en', 'zh'] as const)('resolves all required messages in %s', (locale) => {
    const i18n = createI18n({
      legacy: false,
      locale,
      fallbackLocale: false,
      messages: { en, zh },
    })

    for (const [prefix, keys] of Object.entries(requiredMessageGroups)) {
      for (const suffix of keys) {
        const key = `${prefix}.${suffix}`
        expect(i18n.global.te(key), key).toBe(true)
        const translated = i18n.global.t(key, {
          name: 'Provider A',
          reason: 'disabled',
          count: 2,
          total: 3,
          n: 1,
        })
        expect(translated, key).not.toBe(key)
        expect(translated.trim(), key).not.toBe('')
      }
    }
  })

  // Concurrent model progress and provider details interpolate numeric and
  // account placeholders. Real catalog formatting keeps both languages readable
  // after multi-model actions begin and history details open.
  it.each(['en', 'zh'] as const)(
    'formats concurrent progress and provider details in %s',
    (locale) => {
      const i18n = createI18n({
        legacy: false,
        locale,
        fallbackLocale: false,
        messages: { en, zh },
      })
      const progress = i18n.global.t('admin.providers.qualityProbeDialog.runningMany', {
        count: 2,
        total: 3,
      })

      expect(progress).toContain('2')
      expect(progress).toContain('3')
      expect(progress).not.toContain('{')
      expect(i18n.global.t('admin.providers.qualityProbeStart', { count: 2 })).toContain('2')
      expect(
        i18n.global.t('admin.providers.qualityProbeLogs.detailTitle', { name: 'Provider A' }),
      ).toContain('Provider A')
    },
  )
})
