import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { unref } from 'vue'
import { useSettingsSaveTarget } from '@/composables/useSettingsSaveRegistry'
import QualityProbeSettingsCard from '../QualityProbeSettingsCard.vue'

// QualityProbeSettingsCard.spec.ts verifies the independent quality-probe draft
// through rendered controls and its registered save target. Candidate API doubles
// exercise missing catalogs, delayed responses, and group changes while shared
// layout stubs keep assertions focused on selection and persistence behavior.

// API spies isolate server settings and catalog availability while capturing the
// saved payload produced by the component's registered persistence operation.
const getQualityProbeSettings = vi.fn()
const getQualityProbeModels = vi.fn()
const getAllIncludingInactive = vi.fn()
const saveQualityProbeSettings = vi.fn()

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      getQualityProbeSettings: (...args: unknown[]) => getQualityProbeSettings(...args),
      getQualityProbeModels: (...args: unknown[]) => getQualityProbeModels(...args),
      saveQualityProbeSettings: (...args: unknown[]) => saveQualityProbeSettings(...args),
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

// probeSettings supplies the persisted server draft whose model can outlive an
// empty candidate catalog on instances that have no eligible OpenAI groups yet.
const probeSettings = {
  enabled: false,
  interval_minutes: 30,
  models: ['gpt-6-astra'],
  cooldown_minutes: 5,
  max_attempts: 3,
  notify_email: 'test@example.com',
  group_ids: [],
  schedule_enabled: true,
  schedule_start: '08:00',
  schedule_end: '00:00',
  unschedule_on_degraded: true,
}

enableAutoUnmount(afterEach)

// mountCard renders editable toggles and group checkboxes around the actual draft.
// Each scenario awaits initial promises before interacting; automatic teardown
// releases the component after the registered save operations have completed.
function mountCard() {
  return mount(QualityProbeSettingsCard, {
    global: {
      stubs: {
        SettingsCard: { template: '<div><slot /><slot name="actions" /></div>' },
        SettingsSection: { template: '<div><slot /></div>' },
        SettingToggleRow: {
          props: ['id', 'modelValue'],
          emits: ['update:modelValue'],
          template:
            '<input :id="id" type="checkbox" :checked="modelValue" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
        },
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
}

// registeredTarget returns the draft operations supplied to the page save registry.
function registeredTarget() {
  return vi.mocked(useSettingsSaveTarget).mock.calls[0]![1]
}

describe('QualityProbeSettingsCard', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getQualityProbeSettings.mockResolvedValue(probeSettings)
    getAllIncludingInactive.mockResolvedValue([])
    getQualityProbeModels.mockResolvedValue([])
    saveQualityProbeSettings.mockImplementation(async (payload) => ({ ...payload }))
  })

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
    const wrapper = mountCard()
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([1])
    const groupTwo = wrapper.findAll('input[type=checkbox][value]').at(1)
    await groupTwo?.setValue(true)
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([1, 2])
    const groupOne = wrapper.findAll('input[type=checkbox][value]').at(0)
    await groupOne?.setValue(false)
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([2])
    expect(wrapper.get('[data-test="models"]').text()).toBe('|gpt-6-luna')
  })

  // A new instance can return an empty or unavailable model catalog. Loading and
  // discarding retain its saved model, while deliberate toggle edits remain dirty
  // and save through the page registry with that model intact.
  it.each(['empty', 'failed'] as const)(
    'keeps saved models clean and editable with %s catalog results',
    async (catalog) => {
      if (catalog === 'failed') {
        getQualityProbeModels.mockRejectedValue(new Error('catalog unavailable'))
      }
      const wrapper = mountCard()
      await flushPromises()
      const target = registeredTarget()
      expect(getAllIncludingInactive).toHaveBeenCalledOnce()
      expect(getQualityProbeModels).toHaveBeenCalledWith([])
      expect(wrapper.get('[data-test="models"]').text()).toBe('gpt-6-astra|')
      expect(unref(target.dirty)).toBe(false)

      await wrapper.get('#quality-probe-enabled').setValue(true)
      expect(unref(target.dirty)).toBe(true)
      expect(await target.save()).toBe(true)
      expect(saveQualityProbeSettings).toHaveBeenCalledExactlyOnceWith({
        ...probeSettings,
        enabled: true,
      })
      expect(unref(target.dirty)).toBe(false)

      await wrapper.get('#quality-probe-enabled').setValue(false)
      getQualityProbeSettings.mockResolvedValue({ ...probeSettings, enabled: true })
      expect(unref(target.dirty)).toBe(true)
      await target.discard()
      await flushPromises()
      expect((wrapper.get('#quality-probe-enabled').element as HTMLInputElement).checked).toBe(true)
      expect(wrapper.get('[data-test="models"]').text()).toBe('gpt-6-astra|')
      expect(unref(target.dirty)).toBe(false)
      expect(saveQualityProbeSettings).toHaveBeenCalledOnce()
    },
  )

  // The initial candidate lookup can finish after the settings request. Editable
  // controls remain behind the skeleton until lookup completes, then the loaded
  // draft starts clean and subsequent user edits remain pending.
  it('waits for initial candidates before exposing a clean editable draft', async () => {
    let resolveModels!: (models: string[]) => void
    getQualityProbeModels.mockReturnValueOnce(
      new Promise<string[]>((resolve) => {
        resolveModels = resolve
      }),
    )
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.find('#quality-probe-enabled').exists()).toBe(false)
    expect(unref(registeredTarget().dirty)).toBe(false)

    resolveModels([])
    await flushPromises()
    expect(wrapper.find('#quality-probe-enabled').exists()).toBe(true)
    expect(unref(registeredTarget().dirty)).toBe(false)
    await wrapper.get('#quality-probe-enabled').setValue(true)
    expect(unref(registeredTarget().dirty)).toBe(true)
  })

  // A discard reload can wait for candidates while mounted controls remain
  // editable. An edit made after the server draft arrives stays dirty when
  // discovery finishes and is included in the next save.
  it('retains edits made while a discard candidate refresh is pending', async () => {
    const wrapper = mountCard()
    await flushPromises()
    const target = registeredTarget()
    let resolveModels!: (models: string[]) => void
    getQualityProbeModels.mockReturnValueOnce(
      new Promise<string[]>((resolve) => {
        resolveModels = resolve
      }),
    )
    const discard = target.discard()
    await flushPromises()
    await wrapper.get('#quality-probe-enabled').setValue(true)
    expect(unref(target.dirty)).toBe(true)

    resolveModels([])
    await discard
    await flushPromises()
    expect(unref(target.dirty)).toBe(true)
    expect(await target.save()).toBe(true)
    expect(saveQualityProbeSettings).toHaveBeenCalledExactlyOnceWith({
      ...probeSettings,
      enabled: true,
    })
  })

  // Discard restores saved models before its group catalog finishes loading. A
  // model response from the abandoned group draft must leave that restored
  // selection and its clean save state intact during the wait.
  it('invalidates pending group-model responses when discard restores the server draft', async () => {
    const probeGroups = [
      { id: 1, name: 'first', rate_multiplier: 1, status: 'active' },
      { id: 2, name: 'second', rate_multiplier: 1, status: 'active' },
    ]
    getQualityProbeSettings.mockResolvedValue({ ...probeSettings, group_ids: [1] })
    getAllIncludingInactive.mockResolvedValue(probeGroups)
    getQualityProbeModels.mockResolvedValue(['gpt-6-astra'])
    const wrapper = mountCard()
    await flushPromises()

    let resolveOlderModels!: (models: string[]) => void
    getQualityProbeModels.mockReturnValueOnce(
      new Promise<string[]>((resolve) => {
        resolveOlderModels = resolve
      }),
    )
    await wrapper.get('input[value="2"]').setValue(true)
    let resolveGroups!: (groups: typeof probeGroups) => void
    getAllIncludingInactive.mockReturnValueOnce(
      new Promise<typeof probeGroups>((resolve) => {
        resolveGroups = resolve
      }),
    )
    const target = registeredTarget()
    const discard = target.discard()
    await flushPromises()
    expect(unref(target.dirty)).toBe(false)

    resolveOlderModels(['gpt-6-luna'])
    await flushPromises()
    expect(wrapper.get('[data-test="models"]').text()).toBe('gpt-6-astra|gpt-6-astra')
    expect(unref(target.dirty)).toBe(false)

    resolveGroups(probeGroups)
    await discard
    await flushPromises()
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([1])
    expect(wrapper.get('[data-test="models"]').text()).toBe('gpt-6-astra|gpt-6-astra')
    expect(unref(target.dirty)).toBe(false)
  })

  // Two group edits can leave candidate requests in flight together. The latest
  // scope determines both candidates and selected models even when an older
  // response arrives after that scope has already rendered.
  it('ignores an older candidate response after the group scope changes again', async () => {
    getQualityProbeSettings.mockResolvedValue({ ...probeSettings, group_ids: [1] })
    getAllIncludingInactive.mockResolvedValue([
      { id: 1, name: 'first', rate_multiplier: 1, status: 'active' },
      { id: 2, name: 'second', rate_multiplier: 1, status: 'active' },
    ])
    getQualityProbeModels.mockResolvedValueOnce(['gpt-6-astra'])
    const wrapper = mountCard()
    await flushPromises()

    let resolveOlder!: (models: string[]) => void
    getQualityProbeModels.mockReturnValueOnce(
      new Promise<string[]>((resolve) => {
        resolveOlder = resolve
      }),
    )
    getQualityProbeModels.mockResolvedValueOnce(['gpt-6-luna'])
    await wrapper.get('input[value="2"]').setValue(true)
    await wrapper.get('input[value="1"]').setValue(false)
    await flushPromises()
    expect(wrapper.get('[data-test="models"]').text()).toBe('|gpt-6-luna')

    resolveOlder(['gpt-6-astra'])
    await flushPromises()
    expect(wrapper.get('[data-test="models"]').text()).toBe('|gpt-6-luna')
    expect(getQualityProbeModels).toHaveBeenLastCalledWith([2])
  })
})
