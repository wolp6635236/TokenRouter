import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { PaymentOrder } from '@/types/payment'
import AdminRefundDialog from '../AdminRefundDialog.vue'

const formatBalanceAmountMock = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/composables/useBalanceDisplay', () => ({
  useBalanceDisplay: () => ({
    balanceUnitSymbol: '点',
    formatBalanceAmount: (...args: any[]) => formatBalanceAmountMock(...args),
  }),
}))

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 1,
    user_id: 10,
    amount: 100,
    pay_amount: 108,
    currency: 'USD',
    fee_rate: 8,
    fee_fixed: 2,
    fee_rate_amount: 6,
    fee_amount: 8,
    payment_type: 'stripe',
    out_trade_no: 'sub2_202606250001',
    status: 'COMPLETED',
    order_type: 'subscription',
    created_at: '2026-06-25T10:00:00Z',
    expires_at: '2026-06-25T10:30:00Z',
    refund_amount: 25,
    ...overrides,
  }
}

describe('admin order currency display', () => {
  it('uses order currency for subscription refund amounts', () => {
    const wrapper = mount(AdminRefundDialog, {
      props: {
        show: true,
        order: orderFactory({
          status: 'PARTIALLY_REFUNDED',
          refund_amount: 20,
        }),
        userBalance: 200,
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$108.00')
    expect(text).toContain('$100.00')
    expect(text).toContain('$20.00')
    expect(text).toContain('$80.00')
  })
})
