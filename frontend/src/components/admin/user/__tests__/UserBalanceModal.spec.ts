import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import type { AdminUser } from '@/api/admin/users.types'

// UserBalanceModal.spec.ts checks the contract the administrator balance editor offers to
// the user page. The tests observe the dialog layer forwarded to the shell, the submitted
// account result carried by the success event, and the notification order after the
// endpoint settles.

// apiMocks supplies the balance adjustment response used by the submit path.
const apiMocks = vi.hoisted(() => ({
  updateBalance: vi.fn(), // Balance adjustment spy.
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      updateBalance: apiMocks.updateBalance, // Balance adjustment substitute.
    }, // User endpoint substitutes.
  }, // Administrator API substitute.
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(), // Error notification spy.
    showSuccess: vi.fn(), // Success notification spy.
  }), // Application notification-store substitute.
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null, // No configured balance-unit overrides.
  }), // Store substitute without cached settings.
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key, // Translation key passthrough.
    }), // Translation composable substitute.
  }
})

import UserBalanceModal from '../UserBalanceModal.vue'

// balanceUser is the account receiving the adjustment.
const balanceUser = {
  id: 42, // Fixture record identifier.
  email: 'balance@example.com', // Account email address.
  balance: 7.45, // Current account balance.
} as AdminUser

// DialogShellStub renders the dialog body and exposes the requested modal layer so tests
// can check the forwarded layer without mounting the real teleported shell.
const DialogShellStub = {
  props: ['show', 'zIndex'], // Accepted component props.
  template: '<div v-if="show" data-test="dialog-layer" :data-z-index="zIndex"><slot /></div>',
}

// mountEditor mounts the editor with an inert shell and avatar, applying an optional
// modal layer so tests can check both the shared and nested layers.
const mountEditor = (zIndex?: number) =>
  mount(UserBalanceModal, {
    props: {
      show: true, // Parent-controlled dialog visibility.
      user: balanceUser, // Selected account record.
      operation: 'add', // Balance adjustment direction.
      zIndex, // Requested modal layer; undefined keeps the shared layer.
    },
    global: {
      stubs: {
        BaseDialog: DialogShellStub,
        UserAvatar: true,
      },
    },
  })

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.updateBalance.mockResolvedValue({ ...balanceUser, balance: 17.45 })
})

describe('UserBalanceModal', () => {
  // The row deposit action opens the editor on the shared modal layer. An explicit layer
  // must reach the shell unchanged so a parent dialog can raise the editor above itself.
  it('forwards the requested dialog layer to the shell', () => {
    expect(mountEditor().get('[data-test="dialog-layer"]').attributes('data-z-index')).toBe('50')
    expect(mountEditor(60).get('[data-test="dialog-layer"]').attributes('data-z-index')).toBe('60')
  })

  // A submitted adjustment answers with the updated account. The editor must send that
  // record with the success event before requesting dismissal, so the parent can publish
  // the new balance while the dialog still holds its lifecycle entry.
  it('emits the updated account before requesting dismissal', async () => {
    const events: string[] = []
    const harness = defineComponent({
      components: { UserBalanceModal }, // Editor under test.
      setup() {
        return { events, balanceUser } // Recorded event order and target account.
      },
      template: `
        <UserBalanceModal
          :show="true"
          :user="balanceUser"
          operation="add"
          @success="events.push('success')"
          @close="events.push('close')"
        />
      `,
    })
    const wrapper = mount(harness, {
      global: {
        stubs: {
          BaseDialog: DialogShellStub,
          UserAvatar: true,
        },
      },
    })
    const editor = wrapper.findComponent(UserBalanceModal)

    await wrapper.get('input[type="number"]').setValue('10')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(apiMocks.updateBalance).toHaveBeenCalledWith(42, 10, 'add', '')
    expect(editor.emitted('success')).toEqual([[{ ...balanceUser, balance: 17.45 }]])
    expect(events).toEqual(['success', 'close'])
  })
})
