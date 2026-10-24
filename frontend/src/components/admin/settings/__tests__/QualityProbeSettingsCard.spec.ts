import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QualityProbeSettingsCard from '../QualityProbeSettingsCard.vue'

// QualityProbeSettingsCard.spec.ts verifies that selected group changes refresh
// constrained model candidates and discard models outside the new group scope.
// The component stubs keep this test focused on the settings draft and API calls.

const getQualityProbeSettings = vi.fn()
const getQualityProbeModels = vi.fn()
const getAllIncludingInactive = vi.fn()

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      getQualityProbeSettings: (...args: unknown[]) => getQualityProbeSettings(...args),
      getQualityProbeModels: (...args: unknown[]) => getQualityProbeModels(...args),
      saveQualityProbeSettings: vi.fn(),
    },
    groups: {
      getAllIncludingInactive: (...args: unknown[]) => getAllIncludingInactive(...args),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
}))

vi.mock('@/composables/useSettingsSaveRegistry', () => ({
  useSettingsSaveTarget: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('QualityProbeSettingsCard', () => {
  // The initial selected group provides two models. Switching the selected
  // group reloads candidates and removes models outside the new constrained set.
  it('refreshes models from selected groups and removes models outside the new scope', async () => {
    getQualityProbeSettings.mockResolvedValue({
      enabled: true,
      interval_minutes: 30,
      models: ['gpt-6-astra', 'gpt-5.6-terra'],
      cooldown_minutes: 5,
      max_attempts: 3,
      notify_email: 'test@example.com',
      group_ids: [1],
      schedule_enabled: true,
      schedule_start: '08:00',
      schedule_end: '00:00',
      unschedule_on_degraded: true,
    })
    getAllIncludingInactive.mockResolvedValue([
      { id: 1, name: 'first', rate_multiplier: 1, status: 'active' },
      { id: 2, name: 'second', rate_multiplier: 1, status: 'active' },
    ])
    getQualityProbeModels.mockResolvedValueOnce(['gpt-6-astra', 'gpt-5.6-terra'])
    getQualityProbeModels.mockResolvedValueOnce(['gpt-6-astra', 'gpt-5.6-terra', 'gpt-6-luna'])
    getQualityProbeModels.mockResolvedValueOnce(['gpt-6-luna'])
    const wrapper = mount(QualityProbeSettingsCard, {
      global: {
        stubs: {
          SettingsCard: { template: '<div><slot /><slot name="actions" /></div>' },
          SettingsSection: { template: '<div><slot /></div>' },
          SettingToggleRow: true,
          SettingRow: { template: '<div><slot /></div>' },
          ContentSkeleton: true,
          GroupBadge: true,
          ModelWhitelistSelector: {
            props: ['modelValue', 'models'],
            template:
              '<div data-test="models">{{ modelValue.join(",") }}|{{ models.join(",") }}</div>',
          },
          Icon: true,
        },
      },
    })
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([1])
    const groupTwo = wrapper.findAll('input[type=checkbox]').at(1)
    await groupTwo?.setValue(true)
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([1, 2])
    const groupOne = wrapper.findAll('input[type=checkbox]').at(0)
    await groupOne?.setValue(false)
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([2])
    expect(wrapper.get('[data-test="models"]').text()).toBe('|gpt-6-luna')
  })
})
