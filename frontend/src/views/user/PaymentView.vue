<template>
  <AppLayout>
    <!-- 页签与标题同行放在页头右侧；支付中和订阅确认时隐藏。 -->
    <template v-if="!loading && tabs.length > 1 && paymentPhase === 'select' && !selectedPlan" #page-heading-actions>
      <div v-segmented role="tablist" class="segmented gap-1">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.key"
          class="segmented-item flex h-8 items-center justify-center px-4 text-sm"
          :class="{ 'segmented-item-active': activeTab === tab.key }"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>
    </template>
    <!-- 不加 mx-auto：app-main 是 flex 列容器，auto 边距会让内容收缩到内容宽度并与页头错位。 -->
    <div
      class="purchase-viewport relative w-full"
      :style="{ '--purchase-direction': activeTab === 'subscription' ? 1 : -1 }"
    >
      <MotionTransition name="purchase-slide" :css="!loading">
        <div :key="activeTab" class="w-full space-y-4">
          <!-- 首次取数：按结算双栏的位置显示骨架。 -->
          <div
            v-if="loading"
            role="status"
            aria-busy="true"
            :aria-label="t('common.loading')"
            class="space-y-4"
          >
            <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22.5rem] xl:items-start">
              <div class="space-y-4">
                <div class="card space-y-4 p-4 sm:p-6">
                  <Skeleton width="6rem" height="1rem" />
                  <div class="grid grid-cols-3 gap-2 sm:grid-cols-5">
                    <Skeleton v-for="n in 10" :key="n" height="2.25rem" />
                  </div>
                  <Skeleton height="2.25rem" />
                </div>
                <div class="card space-y-4 p-4 sm:p-6">
                  <Skeleton width="6rem" height="1rem" />
                  <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
                    <Skeleton v-for="n in 3" :key="n" height="3.5rem" />
                  </div>
                </div>
              </div>
              <div class="card space-y-3 p-4 sm:p-6">
                <Skeleton width="5rem" height="0.75rem" />
                <Skeleton width="9rem" height="2rem" />
                <Skeleton height="1rem" class="mt-6" />
                <Skeleton height="1rem" />
                <Skeleton height="2.25rem" class="mt-6" />
              </div>
            </div>
          </div>
          <template v-else>
            <!-- 支付中（充值与订阅共用） -->
            <PaymentStatusPanel
              v-if="paymentPhase === 'paying'"
              :order-id="paymentState.orderId"
              :amount="paymentState.amount"
              :pay-amount="paymentState.payAmount"
              :qr-code="paymentState.qrCode"
              :expires-at="paymentState.expiresAt"
              :payment-type="paymentState.paymentType"
              :out-trade-no="paymentState.outTradeNo"
              :pay-url="paymentState.payUrl"
              :order-type="paymentState.orderType"
              :currency="paymentState.currency || selectedCurrency"
              :mobile-alipay-deep-link="paymentState.alipayMobilePrecreateDeepLink"
              @done="onPaymentDone"
              @success="onPaymentSuccess"
              @settled="onPaymentSettled"
            />

            <!-- 订阅套餐列表 -->
            <template v-else-if="activeTab === 'subscription' && !selectedPlan">
              <div v-if="checkout.plans.length === 0" class="card empty-state">
                <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-surface bg-gray-100 dark:bg-dark-800">
                  <Icon name="gift" size="lg" class="text-gray-400 dark:text-dark-500" />
                </div>
                <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('payment.noPlans') }}</p>
              </div>
              <section v-else :aria-label="t('payment.selectPlan')">
                <!-- 主区在 lg 仍需让出侧栏宽度，三列放到 xl 才不拥挤。 -->
                <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
                  <SubscriptionPlanCard
                    v-for="plan in checkout.plans"
                    :key="plan.id"
                    :plan="plan"
                    :active-subscriptions="activeSubscriptions"
                    @select="selectPlan"
                  />
                </div>
              </section>

              <!-- 当前订阅：沿用兑换页的用量列表，直接展示各周期用量与剩余时间。 -->
              <section
                v-if="activeSubscriptions.length > 0"
                data-testid="purchase-active-subscriptions"
                class="space-y-4"
              >
                <div class="flex items-center justify-between gap-4">
                  <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('payment.activeSubscription') }}</h2>
                  <router-link
                    to="/subscriptions"
                    class="shrink-0 text-sm text-primary-600 hover:underline dark:text-primary-400"
                  >
                    {{ t('subscriptionProgress.viewAll') }}
                  </router-link>
                </div>
                <SubscriptionUsageList
                  :subscriptions="activeSubscriptions"
                  class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3"
                  item-class="card p-4"
                />
              </section>

              <PaymentHelpNote
                v-if="hasHelpContent"
                :image-url="checkout.help_image_url"
                :html="renderedHelpText"
                @preview="previewImage = $event"
              />
            </template>

            <!-- 充值未开放 -->
            <div v-else-if="activeTab === 'recharge' && enabledMethods.length === 0" class="card empty-state">
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-surface bg-gray-100 dark:bg-dark-800">
                <Icon name="creditCard" size="lg" class="text-gray-400 dark:text-dark-500" />
              </div>
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('payment.notAvailable') }}</p>
            </div>

            <!-- 结算：充值与订阅确认共用。xl 以下按选择、摘要顺序堆叠（lg 主区还要让出侧栏，双栏会挤压支付方式），xl 起右栏摘要吸顶。 -->
            <div v-else class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22.5rem] xl:items-start">
              <!-- 金额（或套餐）、支付方式、账单信息各占一张卡片，分组更清楚。 -->
              <div class="min-w-0 space-y-4">
                <!-- 订阅确认：套餐摘要 -->
                <section v-if="isSubscriptionCheckout && selectedPlan" class="card p-4 sm:p-6">
                  <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('payment.confirmSubscription') }}</p>
                  <h2 class="mt-1 break-words text-lg font-semibold text-gray-900 dark:text-white">{{ selectedPlan.name }}</h2>
                  <div class="mt-3 flex flex-wrap items-baseline gap-x-2">
                    <span class="text-3xl font-semibold tracking-tight tabular-nums text-gray-900 dark:text-white">
                      {{ formatSelectedSubscriptionPaymentAmount(selectedPlan.price) }}
                    </span>
                    <span class="text-sm text-gray-500 dark:text-dark-400">/ {{ planValiditySuffix }}</span>
                    <span v-if="selectedPlan.original_price" class="text-sm text-gray-400 line-through dark:text-dark-500">
                      {{ formatSelectedSubscriptionPaymentAmount(selectedPlan.original_price) }}
                    </span>
                  </div>
                  <p v-if="selectedPlan.description" class="mt-2 text-sm leading-relaxed text-gray-500 dark:text-dark-400">
                    {{ selectedPlan.description }}
                  </p>
                  <dl class="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3">
                    <div v-if="hasPlanQuota(selectedPlan.daily_limit_usd)">
                      <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('payment.planCard.dailyLimit') }}</dt>
                      <dd class="mt-1 text-base font-semibold tabular-nums text-gray-900 dark:text-dark-100">{{ formatPlanQuota(selectedPlan.daily_limit_usd) }}</dd>
                    </div>
                    <div v-if="hasPlanQuota(selectedPlan.weekly_limit_usd)">
                      <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('payment.planCard.weeklyLimit') }}</dt>
                      <dd class="mt-1 text-base font-semibold tabular-nums text-gray-900 dark:text-dark-100">{{ formatPlanQuota(selectedPlan.weekly_limit_usd) }}</dd>
                    </div>
                    <div v-if="hasPlanQuota(selectedPlan.monthly_limit_usd)">
                      <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('payment.planCard.monthlyLimit') }}</dt>
                      <dd class="mt-1 text-base font-semibold tabular-nums text-gray-900 dark:text-dark-100">{{ formatPlanQuota(selectedPlan.monthly_limit_usd) }}</dd>
                    </div>
                    <div v-if="!hasPlanQuota(selectedPlan.daily_limit_usd) && !hasPlanQuota(selectedPlan.weekly_limit_usd) && !hasPlanQuota(selectedPlan.monthly_limit_usd)">
                      <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('payment.planCard.quota') }}</dt>
                      <dd class="mt-1 text-base font-semibold text-gray-900 dark:text-dark-100">{{ t('payment.planCard.unlimited') }}</dd>
                    </div>
                  </dl>
                </section>

                <!-- 充值：金额 -->
                <section v-else class="card p-4 sm:p-6">
                  <h2 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.amountLabel') }}</h2>
                  <AmountInput
                    v-model="amount"
                    :amounts="[10, 20, 50, 100, 200, 500, 1000, 2000, 5000]"
                    :min="globalMinAmount"
                    :max="globalMaxAmount"
                    :currency="selectedCurrency"
                  />
                  <p v-if="amountError" class="mt-2 text-xs text-amber-600 dark:text-amber-300">{{ amountError }}</p>
                </section>

                <section v-if="enabledMethods.length >= 1" class="card p-4 sm:p-6">
                  <h2 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.paymentMethod') }}</h2>
                  <PaymentMethodSelector
                    :methods="checkoutMethodOptions"
                    :selected="selectedMethod"
                    @select="selectedMethod = $event"
                  />
                </section>

                <!-- Stripe 账单信息 -->
                <section v-if="isStripeSelected" class="card p-4 sm:p-6">
                  <h2 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.billing.title') }}</h2>
                  <div class="grid gap-4 sm:grid-cols-2">
                    <div>
                      <label class="input-label">{{ t('payment.billing.name') }}</label>
                      <input v-model="billingInfo.name" class="input mt-1 w-full" autocomplete="name" />
                    </div>
                    <div>
                      <label class="input-label">{{ t('payment.billing.email') }}</label>
                      <input v-model="billingInfo.email" class="input mt-1 w-full" autocomplete="email" type="email" />
                    </div>
                    <div>
                      <label class="input-label">{{ optionalBillingLabel('country') }}</label>
                      <input v-model="billingInfo.country" class="input mt-1 w-full" autocomplete="country" maxlength="2" />
                    </div>
                    <div>
                      <label class="input-label">{{ optionalBillingLabel('postalCode') }}</label>
                      <input v-model="billingInfo.postal_code" class="input mt-1 w-full" autocomplete="postal-code" />
                    </div>
                    <div class="sm:col-span-2">
                      <label class="input-label">{{ optionalBillingLabel('line1') }}</label>
                      <input v-model="billingInfo.line1" class="input mt-1 w-full" autocomplete="address-line1" />
                    </div>
                    <div>
                      <label class="input-label">{{ optionalBillingLabel('city') }}</label>
                      <input v-model="billingInfo.city" class="input mt-1 w-full" autocomplete="address-level2" />
                    </div>
                    <div>
                      <label class="input-label">{{ optionalBillingLabel('state') }}</label>
                      <input v-model="billingInfo.state" class="input mt-1 w-full" autocomplete="address-level1" />
                    </div>
                  </div>
                </section>
              </div>

              <!-- 吸顶偏移 = 顶栏高度 + 1.5rem 页面内边距。 -->
              <aside class="min-w-0 space-y-4 xl:sticky xl:top-[calc(var(--header-h)+1.5rem)]">
                <section data-testid="checkout-summary" class="card p-4 sm:p-6">
                  <dl v-if="!isSubscriptionCheckout" class="mb-5 border-b border-gray-100 pb-5 dark:border-dark-700">
                    <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('payment.currentBalance') }}</dt>
                    <dd class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">
                      {{ formatBalanceAmount(user?.balance, { fractionDigits: 2 }) }}
                    </dd>
                    <dd class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400">
                      {{ t('payment.rechargeAccount') }}: {{ user?.username || '' }}
                    </dd>
                  </dl>

                  <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('payment.orderSummary') }}</h2>
                  <dl class="mt-3 space-y-2 text-sm">
                    <div class="flex justify-between gap-4">
                      <dt class="text-gray-500 dark:text-dark-400">{{ checkoutBaseLabel }}</dt>
                      <dd class="tabular-nums text-gray-900 dark:text-dark-100">{{ checkoutBaseText }}</dd>
                    </div>
                    <div v-if="checkoutFeeBreakdown.fixedFee > 0" class="flex justify-between gap-4">
                      <dt class="text-gray-500 dark:text-dark-400">{{ t('payment.fixedFee') }}</dt>
                      <dd class="tabular-nums text-gray-900 dark:text-dark-100">{{ formatSelectedPaymentAmount(checkoutFeeBreakdown.fixedFee) }}</dd>
                    </div>
                    <div v-if="checkoutFeeBreakdown.rateFee > 0" class="flex justify-between gap-4">
                      <dt class="text-gray-500 dark:text-dark-400">{{ t('payment.rateFee') }} ({{ checkoutFeeBreakdown.feeRate }}%)</dt>
                      <dd class="tabular-nums text-gray-900 dark:text-dark-100">{{ formatSelectedPaymentAmount(checkoutFeeBreakdown.rateFee) }}</dd>
                    </div>
                    <div v-if="checkoutFeeBreakdown.totalFee > 0" class="flex justify-between gap-4">
                      <dt class="text-gray-500 dark:text-dark-400">{{ t('payment.feeTotal') }}</dt>
                      <dd class="tabular-nums text-gray-900 dark:text-dark-100">{{ formatSelectedPaymentAmount(checkoutFeeBreakdown.totalFee) }}</dd>
                    </div>
                    <div class="flex items-baseline justify-between gap-4 border-t border-gray-100 pt-3 dark:border-dark-700">
                      <dt class="font-medium text-gray-900 dark:text-white">{{ t('payment.actualPay') }}</dt>
                      <dd class="text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(checkoutTotalAmount) }}</dd>
                    </div>
                    <div v-if="!isSubscriptionCheckout && balanceRechargeMultiplier !== 1" class="flex justify-between gap-4">
                      <dt class="text-gray-500 dark:text-dark-400">{{ t('payment.creditedBalance') }}</dt>
                      <dd class="font-medium tabular-nums text-primary-600 dark:text-primary-400">{{ formatBalanceAmount(creditedAmount, { fractionDigits: 2 }) }}</dd>
                    </div>
                  </dl>
                  <p v-if="!isSubscriptionCheckout && balanceRechargeMultiplier !== 1" class="mt-3 text-xs text-gray-500 dark:text-dark-400">
                    {{ t('payment.rechargeRatePreview', { currency: selectedCurrency, amount: balanceRechargeMultiplier.toFixed(2), unitName: balanceUnitName }) }}
                  </p>

                  <div class="mt-6 space-y-2">
                    <button
                      type="button"
                      :class="['btn w-full', paymentButtonClass]"
                      :disabled="!checkoutCanSubmit || submitting"
                      @click="handleCheckoutSubmit"
                    >
                      <template v-if="submitting">
                        <Icon name="loader" size="sm" :animate-on-hover="false" class="animate-spin" />
                        {{ t('common.processing') }}
                      </template>
                      <template v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(checkoutTotalAmount) }}</template>
                    </button>
                    <button
                      v-if="isSubscriptionCheckout"
                      type="button"
                      class="btn btn-secondary w-full"
                      @click="selectedPlan = null"
                    >
                      {{ t('common.cancel') }}
                    </button>
                  </div>
                </section>

                <PaymentHelpNote
                  v-if="!isSubscriptionCheckout && hasHelpContent"
                  :image-url="checkout.help_image_url"
                  :html="renderedHelpText"
                  @preview="previewImage = $event"
                />
              </aside>
            </div>
          </template>
        </div>
      </MotionTransition>
    </div>
    <!-- 续费套餐选择弹窗 -->
    <BaseDialog
      :show="showRenewalModal"
      :title="t('payment.selectPlan')"
      width="normal"
      close-on-click-outside
      @close="closeRenewalModal"
    >
      <div class="space-y-4">
        <SubscriptionPlanCard v-for="plan in renewalPlans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlanFromModal" />
      </div>
    </BaseDialog>
    <ConfirmDialog
      :show="duplicatePlanDialogPlan !== null"
      :title="t('payment.duplicatePlan.title')"
      :message="duplicatePlanDialogMessage"
      :confirm-text="t('payment.duplicatePlan.continue')"
      :cancel-text="t('common.cancel')"
      @confirm="confirmDuplicatePlanPurchase"
      @cancel="closeDuplicatePlanDialog"
    />
    <!-- 帮助图片预览 -->
    <Teleport to="body">
      <MotionTransition name="modal">
        <div v-if="previewImage" class="fixed inset-0 z-modal-nested flex items-center justify-center bg-[var(--overlay-bg-strong)] backdrop-blur-sm" @click="previewImage = ''">
          <img :src="previewImage" alt="" class="max-h-[85vh] max-w-[90vw] rounded-surface object-contain shadow-2xl" />
        </div>
      </MotionTransition>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { useLocaleRefresh } from '@/composables/useLocaleRefresh'
import { vSegmented } from '@/directives/segmented'
import MotionTransition from '@/components/common/MotionTransition.vue'
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAuthStore } from '@/stores/auth'
import { usePaymentStore } from '@/stores/payment'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'
import type { BillingInfo, SubscriptionPlan, CheckoutInfoResponse, CreateOrderResult, OrderType } from '@/types/payment'
import type { UserSubscription } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import AmountInput from '@/components/payment/AmountInput.vue'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'
import { METHOD_ORDER, getPaymentPopupFeatures, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/components/payment/providerConfig'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  buildCreateOrderPayload,
  clearPaymentRecoverySnapshot,
  decidePaymentLaunch,
  getVisibleMethods,
  normalizeVisibleMethod,
  readPaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'
import PaymentHelpNote from '@/components/payment/PaymentHelpNote.vue'
import SubscriptionUsageList from '@/components/common/SubscriptionUsageList.vue'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import { useBalanceDisplay } from '@/composables/useBalanceDisplay'
import { DEFAULT_PAYMENT_CURRENCY, formatPaymentAmount, normalizePaymentCurrency, paymentCurrencyFractionDigits } from '@/components/payment/currency'
import { planValiditySuffix as validitySuffixOf } from '@/components/payment/validity'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import { buildPaymentErrorToastMessage, describePaymentScenarioError } from './paymentUx'
import { hasWechatResumeQuery, parseWechatResumeRoute, stripWechatResumeQuery } from './paymentWechatResume'

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const paymentStore = usePaymentStore()
const subscriptionStore = useSubscriptionStore()
const appStore = useAppStore()
const { balanceUnitName, formatBalanceAmount } = useBalanceDisplay()

marked.setOptions({
  breaks: true,
  gfm: true,
})

const user = computed(() => authStore.user)
const activeSubscriptions = computed(() => subscriptionStore.activeSubscriptions)

const loading = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const errorHintMessage = ref('')
const activeTab = ref<'recharge' | 'subscription'>('recharge')
const amount = ref<number | null>(null)
const selectedMethod = ref('')
const selectedPlan = ref<SubscriptionPlan | null>(null)
const previewImage = ref('')
const duplicatePlanDialogPlan = ref<SubscriptionPlan | null>(null)
const duplicatePlanDialogConfirm = ref<(() => void) | null>(null)
const duplicatePlanAcknowledgedId = ref<number | null>(null)
const lastAutoBillingName = ref('')
const billingInfo = reactive({
  name: '',
  email: '',
  country: '',
  line1: '',
  city: '',
  state: '',
  postal_code: '',
})

const paymentPhase = ref<'select' | 'paying'>('select')

interface CreateOrderOptions {
  openid?: string
  wechatResumeToken?: string
  paymentType?: string
  isResume?: boolean
  mobileQrFallbackAttempted?: boolean
}

interface WeixinJSBridgeLike {
  invoke(
    action: string,
    payload: Record<string, unknown>,
    callback: (result: Record<string, unknown>) => void,
  ): void
}

function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    alipayMobilePrecreateDeepLink: false,
    createdAt: 0,
  }
}

function getWeixinJSBridge(): WeixinJSBridgeLike | undefined {
  return (window as Window & { WeixinJSBridge?: WeixinJSBridgeLike }).WeixinJSBridge
}

function waitForWeixinJSBridge(timeoutMs = 4000): Promise<WeixinJSBridgeLike | null> {
  const existing = getWeixinJSBridge()
  if (existing) return Promise.resolve(existing)

  return new Promise((resolve) => {
    let settled = false
    const finish = (bridge: WeixinJSBridgeLike | null) => {
      if (settled) return
      settled = true
      document.removeEventListener('WeixinJSBridgeReady', handleReady)
      document.removeEventListener('onWeixinJSBridgeReady', handleReady)
      window.clearTimeout(timer)
      resolve(bridge)
    }
    const handleReady = () => finish(getWeixinJSBridge() ?? null)
    const timer = window.setTimeout(() => finish(getWeixinJSBridge() ?? null), timeoutMs)
    document.addEventListener('WeixinJSBridgeReady', handleReady, false)
    document.addEventListener('onWeixinJSBridgeReady', handleReady, false)
  })
}

async function invokeWechatJsapiPayment(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  const bridge = await waitForWeixinJSBridge()
  if (!bridge) {
    throw new Error('WECHAT_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve) => {
    bridge.invoke('getBrandWCPayRequest', payload, (result) => resolve(result || {}))
  })
}

const paymentState = ref<PaymentRecoverySnapshot>(emptyPaymentState())

function persistRecoverySnapshot(snapshot: PaymentRecoverySnapshot) {
  if (typeof window === 'undefined' || !snapshot.orderId) return
  writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
}

function removeRecoverySnapshot() {
  if (typeof window === 'undefined') return
  clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
}

function resetPayment() {
  paymentPhase.value = 'select'
  paymentState.value = emptyPaymentState()
  removeRecoverySnapshot()
}

async function redirectToPaymentResult(state: PaymentRecoverySnapshot): Promise<void> {
  const query: Record<string, string | undefined> = {}
  if (state.orderId > 0) {
    query.order_id = String(state.orderId)
  }
  if (state.outTradeNo) {
    query.out_trade_no = state.outTradeNo
  }
  if (state.resumeToken) {
    query.resume_token = state.resumeToken
  }
  await router.push({
    path: '/payment/result',
    query,
  })
}

function buildWechatOAuthAuthorizeUrl(
  authorizeUrl: string,
  context: { paymentType: string; orderType: OrderType; planId?: number; orderAmount: number },
): string {
  const normalizedUrl = authorizeUrl.trim()
  if (!normalizedUrl || typeof window === 'undefined') {
    return normalizedUrl
  }

  try {
    const targetUrl = new URL(normalizedUrl, window.location.origin)
    const redirectPath = targetUrl.searchParams.get('redirect') || '/purchase'
    const redirectUrl = new URL(redirectPath, window.location.origin)
    const paymentType = normalizeVisibleMethod(context.paymentType) || context.paymentType.trim() || 'wxpay'

    redirectUrl.searchParams.set('payment_type', paymentType)
    redirectUrl.searchParams.set('order_type', context.orderType)

    if (context.planId) {
      redirectUrl.searchParams.set('plan_id', String(context.planId))
    } else {
      redirectUrl.searchParams.delete('plan_id')
    }

    if (context.orderAmount > 0) {
      redirectUrl.searchParams.set('amount', String(context.orderAmount))
    } else {
      redirectUrl.searchParams.delete('amount')
    }

    targetUrl.searchParams.set('redirect', `${redirectUrl.pathname}${redirectUrl.search}`)
    return targetUrl.toString()
  } catch {
    return normalizedUrl
  }
}

function onPaymentDone() {
  const wasSubscription = paymentState.value.orderType === 'subscription'
  resetPayment()
  selectedPlan.value = null
  if (wasSubscription) {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
}

async function onPaymentSuccess() {
  const completedPayment = { ...paymentState.value }
  removeRecoverySnapshot()
  authStore.refreshUser()
  if (paymentState.value.orderType === 'subscription') {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
  await redirectToPaymentResult(completedPayment)
}

function onPaymentSettled() {
  removeRecoverySnapshot()
}

// All checkout data from single API call
const checkout = ref<CheckoutInfoResponse>({
  methods: {}, global_min: 0, global_max: 0,
  plans: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, method_fees: {}, help_text: '', help_image_url: '', stripe_publishable_key: '',
})

const tabs = computed(() => {
  const result: { key: 'recharge' | 'subscription'; label: string }[] = []
  if (!checkout.value.balance_disabled) result.push({ key: 'recharge', label: t('payment.tabTopUp') })
  result.push({ key: 'subscription', label: t('payment.tabSubscribe') })
  return result
})

const visibleMethods = computed(() => getVisibleMethods(checkout.value.methods))
const enabledMethods = computed(() => Object.keys(visibleMethods.value))
const validAmount = computed(() => amount.value ?? 0)
const balanceRechargeMultiplier = computed(() => {
  const multiplier = checkout.value.balance_recharge_multiplier
  return multiplier > 0 ? multiplier : 1
})
// 订阅 CNY 换算汇率（1 USD = X CNY）。0 = 未配置，订阅保持 price 直付（与后端 opt-in 条件严格镜像）。
const subscriptionUsdToCnyRate = computed(() => {
  const rate = checkout.value.subscription_usd_to_cny_rate
  return Number.isFinite(rate) && rate > 0 ? rate : 0
})
const creditedAmount = computed(() => Math.round((validAmount.value * balanceRechargeMultiplier.value) * 100) / 100)
const hasHelpContent = computed(() => Boolean(checkout.value.help_text || checkout.value.help_image_url))
const renderedHelpText = computed(() => {
  const content = checkout.value.help_text.trim()
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
})

function formatPlanQuota(value: number | null | undefined): string {
  const amount = Number(value)
  return formatBalanceAmount(value, { fractionDigits: Number.isInteger(amount) ? 0 : 2 })
}

// Check if an amount fits a method's [min, max]. 0 = no limit.
function amountFitsMethod(amt: number, methodType: string): boolean {
  if (amt <= 0) return true
  const ml = visibleMethods.value[methodType]
  if (!ml) return false
  if (ml.single_min > 0 && amt < ml.single_min) return false
  if (ml.single_max > 0 && amt > ml.single_max) return false
  return true
}

// Visible methods decide the amount range shown to users.
const globalMinAmount = computed(() => {
  const limits = Object.values(visibleMethods.value)
  if (limits.length === 0) return 0
  if (limits.some(limit => limit.single_min <= 0)) return 0
  return Math.min(...limits.map(limit => limit.single_min))
})
const globalMaxAmount = computed(() => {
  const limits = Object.values(visibleMethods.value)
  if (limits.length === 0) return 0
  if (limits.some(limit => limit.single_max <= 0)) return 0
  return Math.max(...limits.map(limit => limit.single_max))
})

// Selected method's limits (for validation and error messages)
const selectedLimit = computed(() => visibleMethods.value[selectedMethod.value])
const isStripeSelected = computed(() => (normalizeVisibleMethod(selectedMethod.value) || selectedMethod.value) === 'stripe')
const registeredEmail = computed(() => (user.value?.email || '').trim())
const defaultBillingName = computed(() => (user.value?.username || '').trim() || registeredEmail.value)

function optionalBillingLabel(key: 'country' | 'postalCode' | 'line1' | 'city' | 'state'): string {
  return `${t(`payment.billing.${key}`)}${t('payment.billing.optionalMark')}`
}

// 账单抬头默认使用用户名，未设置用户名时使用邮箱。
function fillBillingNameFromUser() {
  const nextName = defaultBillingName.value
  if (!nextName) return
  if (billingInfo.name.trim() !== '' && billingInfo.name !== lastAutoBillingName.value) return
  billingInfo.name = nextName
  lastAutoBillingName.value = nextName
}

// 默认使用注册邮箱，但不覆盖用户已经填写过的账单邮箱。
function fillBillingEmailFromUser() {
  if (billingInfo.email.trim() !== '') return
  billingInfo.email = registeredEmail.value
}

function buildStripeBillingInfo(): BillingInfo | undefined {
  if (!isStripeSelected.value) return undefined
  fillBillingNameFromUser()
  fillBillingEmailFromUser()
  const address = {
    country: billingInfo.country.trim().toUpperCase(),
    line1: billingInfo.line1.trim(),
    city: billingInfo.city.trim(),
    state: billingInfo.state.trim(),
    postal_code: billingInfo.postal_code.trim(),
  }
  const hasAddress = Object.values(address).some(Boolean)
  return {
    name: billingInfo.name.trim(),
    email: billingInfo.email.trim(),
    address: hasAddress ? address : undefined,
  }
}

function validateStripeBillingInfo(): boolean {
  if (!isStripeSelected.value) return true
  const name = billingInfo.name.trim()
  const email = billingInfo.email.trim()
  if (!name || !email) {
    errorMessage.value = t('payment.errors.billingRequired')
    errorHintMessage.value = ''
    appStore.showError(errorMessage.value)
    return false
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    errorMessage.value = t('payment.errors.billingEmailInvalid')
    errorHintMessage.value = ''
    appStore.showError(errorMessage.value)
    return false
  }
  return true
}

const selectedCurrency = computed(() => normalizePaymentCurrency(selectedLimit.value?.currency))
const localeCode = computed(() => {
  const raw = i18n.locale as unknown
  if (typeof raw === 'string') return raw
  if (raw && typeof raw === 'object' && 'value' in raw) {
    return String((raw as { value?: string }).value || '')
  }
  return undefined
})

function subscriptionPaymentAmountForCurrency(value: number, currency: string): number {
  const rate = subscriptionUsdToCnyRate.value
  if (rate <= 0 || currency !== DEFAULT_PAYMENT_CURRENCY) return roundMoneyForCurrency(value, currency)
  return roundMoneyForCurrency(value * rate, currency)
}
function formatSelectedPaymentAmount(value: number): string {
  return formatPaymentAmount(value, selectedCurrency.value, localeCode.value)
}

function formatSelectedSubscriptionPaymentAmount(value: number): string {
  return formatSelectedPaymentAmount(subscriptionPaymentAmountForCurrency(value, selectedCurrency.value))
}
const methodOptions = computed<PaymentMethodOption[]>(() =>
  enabledMethods.value.map((type) => {
    const ml = visibleMethods.value[type]
    return {
      type,
      fee_fixed: ml?.fee_fixed ?? 0,
      display_name: ml?.display_name,
      fee_rate: ml?.fee_rate ?? 0,
      available: ml?.available !== false && amountFitsMethod(validAmount.value, type),
    }
  })
)

interface FeeBreakdown {
  fixedFee: number
  feeRate: number
  rateFee: number
  totalFee: number
  payAmount: number
}

function currencyScale(currency: string): number {
  // 金额计算按支付币种的小数位缩放，支持 JPY 这类零小数币种和 KWD 这类三位小数币种。
  return 10 ** paymentCurrencyFractionDigits(currency)
}

function roundMoneyForCurrency(value: number, currency: string): number {
  const scale = currencyScale(currency)
  return Math.round((value + Number.EPSILON) * scale) / scale
}

function ceilMoneyForCurrency(value: number, currency: string): number {
  const scale = currencyScale(currency)
  // 比例手续费向上取到最小货币单位，预览采用实际应付金额的精度。
  return Math.ceil(value * scale - 1e-9) / scale
}

function calculateFeeBreakdown(baseAmount: number, methodType: string): FeeBreakdown {
  const methodLimit = visibleMethods.value[methodType]
  const currency = normalizePaymentCurrency(methodLimit?.currency)
  const fixedFee = Math.max(0, Number(methodLimit?.fee_fixed) || 0)
  const feeRate = Math.max(0, Number(methodLimit?.fee_rate) || 0)
  const normalizedAmount = roundMoneyForCurrency(baseAmount, currency)
  const normalizedFixedFee = roundMoneyForCurrency(fixedFee, currency)
  const rateFee = normalizedAmount > 0 && feeRate > 0 ? ceilMoneyForCurrency((normalizedAmount * feeRate) / 100, currency) : 0
  const totalFee = roundMoneyForCurrency(normalizedFixedFee + rateFee, currency)
  return {
    fixedFee: normalizedFixedFee,
    feeRate,
    rateFee,
    totalFee,
    payAmount: roundMoneyForCurrency(normalizedAmount + totalFee, currency),
  }
}

const rechargeFeeBreakdown = computed(() => calculateFeeBreakdown(validAmount.value, selectedMethod.value))
const totalAmount = computed(() => rechargeFeeBreakdown.value.payAmount)

const amountError = computed(() => {
  if (validAmount.value <= 0) return ''
  // No method can handle this amount
  if (!enabledMethods.value.some((m) => amountFitsMethod(validAmount.value, m))) {
    return t('payment.amountNoMethod')
  }
  // Selected method can't handle this amount (but others can)
  const ml = selectedLimit.value
  if (ml) {
    if (ml.single_min > 0 && validAmount.value < ml.single_min) return t('payment.amountTooLow', { min: formatSelectedPaymentAmount(ml.single_min) })
    if (ml.single_max > 0 && validAmount.value > ml.single_max) return t('payment.amountTooHigh', { max: formatSelectedPaymentAmount(ml.single_max) })
  }
  return ''
})

const canSubmit = computed(() =>
  validAmount.value > 0
    && amountFitsMethod(validAmount.value, selectedMethod.value)
    && selectedLimit.value?.available !== false
    && (!isStripeSelected.value || (billingInfo.name.trim() !== '' && billingInfo.email.trim() !== ''))
)

// 订阅方式限额按换算后的网关实扣金额（含手续费）判断。
const subMethodOptions = computed<PaymentMethodOption[]>(() => {
  const planPrice = selectedPlan.value?.price ?? 0
  return enabledMethods.value.map((type) => {
    const ml = visibleMethods.value[type]
    const currency = normalizePaymentCurrency(ml?.currency)
    const baseAmount = subscriptionPaymentAmountForCurrency(planPrice, currency)
    return {
      type,
      fee_fixed: ml?.fee_fixed ?? 0,
      display_name: ml?.display_name,
      fee_rate: ml?.fee_rate ?? 0,
      available: ml?.available !== false && amountFitsMethod(calculateFeeBreakdown(baseAmount, type).payAmount, type),
    }
  })
})

const subscriptionFeeBreakdown = computed(() => {
  const baseAmount = subscriptionPaymentAmountForCurrency(selectedPlan.value?.price ?? 0, selectedCurrency.value)
  return calculateFeeBreakdown(baseAmount, selectedMethod.value)
})
const subTotalAmount = computed(() => subscriptionFeeBreakdown.value.payAmount)

const canSubmitSubscription = computed(() =>
  selectedPlan.value !== null
    && amountFitsMethod(selectedPlan.value.price, selectedMethod.value)
    && selectedLimit.value?.available !== false
    && (!isStripeSelected.value || (billingInfo.name.trim() !== '' && billingInfo.email.trim() !== ''))
)

// 结算区由充值和订阅确认共用，以下计算属性按当前场景切换数据来源。
const isSubscriptionCheckout = computed(() => activeTab.value === 'subscription' && selectedPlan.value !== null)
const checkoutMethodOptions = computed(() => (isSubscriptionCheckout.value ? subMethodOptions.value : methodOptions.value))
const checkoutFeeBreakdown = computed(() => (isSubscriptionCheckout.value ? subscriptionFeeBreakdown.value : rechargeFeeBreakdown.value))
const checkoutTotalAmount = computed(() => (isSubscriptionCheckout.value ? subTotalAmount.value : totalAmount.value))
const checkoutCanSubmit = computed(() => (isSubscriptionCheckout.value ? canSubmitSubscription.value : canSubmit.value))
const checkoutBaseLabel = computed(() => (isSubscriptionCheckout.value ? t('payment.orders.amount') : t('payment.paymentAmount')))
const checkoutBaseText = computed(() => {
  if (isSubscriptionCheckout.value && selectedPlan.value) {
    return formatSelectedSubscriptionPaymentAmount(selectedPlan.value.price)
  }
  return formatSelectedPaymentAmount(validAmount.value)
})

function handleCheckoutSubmit() {
  if (isSubscriptionCheckout.value) {
    void confirmSubscribe()
    return
  }
  void handleSubmitRecharge()
}

// Auto-switch to first available method when current selection can't handle the amount
watch(() => [validAmount.value, selectedMethod.value] as const, ([amt, method]) => {
  if (amt <= 0 || amountFitsMethod(amt, method)) return
  const available = enabledMethods.value.find((m) => amountFitsMethod(amt, m))
  if (available) selectedMethod.value = available
})

watch(defaultBillingName, fillBillingNameFromUser, { immediate: true })
watch(registeredEmail, fillBillingEmailFromUser, { immediate: true })

// Payment button class: follows selected payment method color
const paymentButtonClass = computed(() => {
  const m = selectedMethod.value
  if (!m) return 'btn-primary'
  if (isBuiltInAlipayMethod(m)) return 'btn-alipay'
  if (isBuiltInWxpayMethod(m)) return 'btn-wxpay'
  if (m === 'stripe') return 'btn-stripe'
  if (m === 'airwallex') return 'btn-airwallex'
  return 'btn-primary'
})

// Renewal modal state
const showRenewalModal = ref(false)
const renewGroupId = ref<number | null>(null)

// 空分组列表表示套餐可用于任意分组；同时兼容尚未升级的旧接口返回值。
function isPlanAvailableForGroup(plan: SubscriptionPlan, groupId: number): boolean {
  if (!Number.isSafeInteger(groupId) || groupId <= 0) return false
  if (Array.isArray(plan.group_ids)) {
    return plan.group_ids.length === 0 || plan.group_ids.includes(groupId)
  }
  if (typeof plan.group_id === 'number' && plan.group_id > 0) {
    return plan.group_id === groupId
  }
  return true
}

const renewalPlans = computed(() => {
  const groupId = renewGroupId.value
  if (groupId == null) return []
  return checkout.value.plans.filter(plan => isPlanAvailableForGroup(plan, groupId))
})

const planValiditySuffix = computed(() => {
  if (!selectedPlan.value) return ''
  return validitySuffixOf(selectedPlan.value, t)
})

const duplicatePlanDialogMessage = computed(() => t('payment.duplicatePlan.message', {
  plan: duplicatePlanDialogPlan.value?.name || t('payment.confirmSubscription'),
}))

async function loadActiveSubscriptionsForSelection(): Promise<UserSubscription[]> {
  try {
    const subscriptions = await subscriptionStore.fetchActiveSubscriptions(true)
    return Array.isArray(subscriptions) ? subscriptions : activeSubscriptions.value
  } catch {
    return activeSubscriptions.value
  }
}

async function hasActivePlan(planId: number): Promise<boolean> {
  const subscriptions = await loadActiveSubscriptionsForSelection()
  return subscriptions.some(sub => sub.plan_id === planId && sub.status === 'active')
}

function applySelectedPlan(plan: SubscriptionPlan, acknowledgedDuplicate = false) {
  selectedPlan.value = plan
  duplicatePlanAcknowledgedId.value = acknowledgedDuplicate ? plan.id : null
  errorMessage.value = ''
}

function openDuplicatePlanDialog(plan: SubscriptionPlan, onConfirm: () => void) {
  duplicatePlanDialogPlan.value = plan
  duplicatePlanDialogConfirm.value = onConfirm
}

function closeDuplicatePlanDialog() {
  duplicatePlanDialogPlan.value = null
  duplicatePlanDialogConfirm.value = null
}

function confirmDuplicatePlanPurchase() {
  const onConfirm = duplicatePlanDialogConfirm.value
  closeDuplicatePlanDialog()
  onConfirm?.()
}

async function selectPlan(plan: SubscriptionPlan) {
  // 购买同一个已生效套餐只会顺延有效期，进入确认页前先提示用户。
  if (await hasActivePlan(plan.id)) {
    openDuplicatePlanDialog(plan, () => applySelectedPlan(plan, true))
    return
  }
  applySelectedPlan(plan)
}

function hasPlanQuota(value: number | null | undefined): boolean {
  return value != null && value > 0
}

async function selectPlanFromModal(plan: SubscriptionPlan) {
  // 续费弹窗中选中同一套餐时，也需要展示有效期顺延提醒。
  if (await hasActivePlan(plan.id)) {
    openDuplicatePlanDialog(plan, () => {
      closeRenewalModal()
      applySelectedPlan(plan, true)
    })
    return
  }
  closeRenewalModal()
  applySelectedPlan(plan)
}

function closeRenewalModal() {
  showRenewalModal.value = false
  renewGroupId.value = null
}

async function handleSubmitRecharge() {
  if (!canSubmit.value || submitting.value) return
  if (!validateStripeBillingInfo()) return
  await createOrder(validAmount.value, 'balance')
}

async function confirmSubscribe() {
  if (!selectedPlan.value || submitting.value) return
  if (!validateStripeBillingInfo()) return
  const plan = selectedPlan.value
  // 若订阅状态在选中套餐后才刷新出来，提交订单前再兜底提醒一次。
  if (duplicatePlanAcknowledgedId.value !== plan.id && await hasActivePlan(plan.id)) {
    openDuplicatePlanDialog(plan, () => {
      duplicatePlanAcknowledgedId.value = plan.id
      void createOrder(plan.price, 'subscription', plan.id)
    })
    return
  }
  await createOrder(plan.price, 'subscription', plan.id)
}

async function createOrder(orderAmount: number, orderType: OrderType, planId?: number, options: CreateOrderOptions = {}) {
  submitting.value = true
  errorMessage.value = ''
  errorHintMessage.value = ''
  const requestType = normalizeVisibleMethod(options.paymentType || selectedMethod.value) || options.paymentType || selectedMethod.value
  try {
    const payload = buildCreateOrderPayload({
      amount: orderAmount,
      paymentType: requestType,
      orderType,
      planId,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      billingInfo: buildStripeBillingInfo(),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && normalizeVisibleMethod(requestType) === 'alipay'),
      mobilePrecreateDeepLink: checkout.value.alipay_mobile_precreate_deep_link === true,
    })
    if (options.openid) {
      payload.openid = options.openid
    }
    if (options.wechatResumeToken) {
      payload.wechat_resume_token = options.wechatResumeToken
    }

    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const openWindow = (url: string) => {
      const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) {
        window.location.href = url
      }
    }
    const visibleMethod = normalizeVisibleMethod(requestType) || requestType
    // 用户点击独立 Stripe 按钮时不指定子方式，让落地页展示完整 Payment Element。
    const stripeMethod = visibleMethod === 'stripe'
      ? ''
      : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    // Stripe Checkout 使用 pay_url 直接跳转；这里仅为旧 Payment Element 响应生成站内路由。
    const stripeRouteUrl = result.client_secret && visibleMethod !== 'airwallex'
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const airwallexRouteUrl = result.client_secret && result.intent_id
      ? router.resolve({
        path: '/payment/airwallex',
        query: {
          order_id: String(result.order_id),
          out_trade_no: result.out_trade_no || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType,
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && visibleMethod === 'alipay'),
      mobilePrecreateDeepLink: checkout.value.alipay_mobile_precreate_deep_link === true,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
      airwallexRouteUrl,
    })

    if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
      window.location.href = buildWechatOAuthAuthorizeUrl(decision.oauth.authorize_url, {
        paymentType: visibleMethod,
        orderType,
        planId,
        orderAmount,
      })
      return
    }

    if (decision.kind === 'unhandled') {
      applyScenarioError({ reason: 'UNHANDLED_PAYMENT_SCENARIO' }, visibleMethod)
      return
    }

    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)

    if (decision.kind === 'stripe_popup') {
      openWindow(decision.paymentState.payUrl)
      return
    }
    if (decision.kind === 'stripe_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'airwallex_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'wechat_jsapi' && decision.jsapi) {
      try {
        const jsapiResult = await invokeWechatJsapiPayment(decision.jsapi as Record<string, unknown>)
        const errMsg = String(jsapiResult.err_msg || '').toLowerCase()
        if (errMsg.includes('cancel')) {
          appStore.showInfo(t('payment.qr.cancelled'))
          resetPayment()
        } else if (errMsg && !errMsg.includes('ok')) {
          resetPayment()
          const fallbackApplied = await attemptMobileQrFallback(
            { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
            {
              orderAmount,
              orderType,
              planId,
              paymentType: visibleMethod,
              attempted: options.mobileQrFallbackAttempted === true,
            },
          )
          if (!fallbackApplied) {
            applyScenarioError({ reason: 'WECHAT_JSAPI_FAILED', message: errMsg }, visibleMethod)
          }
        } else {
          const resultState = { ...decision.paymentState }
          resetPayment()
          await redirectToPaymentResult(resultState)
        }
      } catch (err: unknown) {
        resetPayment()
        const fallbackApplied = await attemptMobileQrFallback(err, {
          orderAmount,
          orderType,
          planId,
          paymentType: visibleMethod,
          attempted: options.mobileQrFallbackAttempted === true,
        })
        if (!fallbackApplied) {
          throw err
        }
      }
      return
    }
    if (decision.kind === 'redirect_waiting' && decision.paymentState.payUrl) {
      if (isMobileDevice()) {
        window.location.href = decision.paymentState.payUrl
        return
      }
      openWindow(decision.paymentState.payUrl)
    }
  } catch (err: unknown) {
    const apiErr = err as Record<string, unknown>
    if (apiErr.reason === 'TOO_MANY_PENDING') {
      const metadata = apiErr.metadata as Record<string, unknown> | undefined
      errorMessage.value = t('payment.errors.tooManyPending', { max: metadata?.max || '' })
      errorHintMessage.value = ''
    } else if (apiErr.reason === 'CANCEL_RATE_LIMITED') {
      errorMessage.value = t('payment.errors.cancelRateLimited')
      errorHintMessage.value = ''
    } else if (await attemptMobileQrFallback(err, {
      orderAmount,
      orderType,
      planId,
      paymentType: requestType,
      attempted: options.mobileQrFallbackAttempted === true,
    })) {
      return
    } else {
      const handled = applyScenarioError(
        err,
        normalizeVisibleMethod(options.paymentType || selectedMethod.value) || selectedMethod.value,
      )
      if (!handled) {
        errorMessage.value = extractI18nErrorMessage(err, t, 'payment.errors', extractApiErrorMessage(err, t('payment.result.failed')))
        errorHintMessage.value = ''
      }
      if (handled) {
        return
      }
    }
    appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  } finally {
    submitting.value = false
  }
}

interface MobileQrFallbackContext {
  orderAmount: number
  orderType: OrderType
  planId?: number
  paymentType: string
  attempted: boolean
}

function shouldFallbackToDesktopQr(err: unknown, paymentMethod: string, attempted: boolean): boolean {
  if (attempted || !isMobileDevice()) {
    return false
  }

  const normalizedMethod = normalizeVisibleMethod(paymentMethod) || paymentMethod
  const reason = typeof err === 'object' && err && 'reason' in err && typeof err.reason === 'string'
    ? err.reason
    : ''
  const message = err instanceof Error
    ? err.message
    : (typeof err === 'object' && err && 'message' in err && typeof err.message === 'string'
      ? err.message
      : '')
  const normalizedMessage = message.toLowerCase()

  if (normalizedMethod === 'wxpay') {
    return reason === 'WECHAT_H5_NOT_AUTHORIZED'
      || reason === 'WECHAT_PAYMENT_MP_NOT_CONFIGURED'
      || reason === 'WECHAT_JSAPI_FAILED'
      || reason === 'PAYMENT_GATEWAY_ERROR'
      || reason === 'UNHANDLED_PAYMENT_SCENARIO'
      || normalizedMessage.includes('weixinjsbridge is unavailable')
      || normalizedMessage.includes('wechat_jsapi_unavailable')
  }

  if (normalizedMethod === 'alipay') {
    return reason === 'PAYMENT_GATEWAY_ERROR' || reason === 'UNHANDLED_PAYMENT_SCENARIO'
  }

  return false
}

async function attemptMobileQrFallback(err: unknown, context: MobileQrFallbackContext): Promise<boolean> {
  if (!shouldFallbackToDesktopQr(err, context.paymentType, context.attempted)) {
    return false
  }

  try {
    const visibleMethod = normalizeVisibleMethod(context.paymentType) || context.paymentType
    const payload = buildCreateOrderPayload({
      amount: context.orderAmount,
      paymentType: visibleMethod,
      orderType: context.orderType,
      planId: context.planId,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: false,
      isWechatBrowser: false,
      billingInfo: buildStripeBillingInfo(),
    })
    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const stripeMethod = visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    // 移动端失败后的桌面兜底仍只需要 Payment Element 路由，Checkout pay_url 由决策函数处理。
    const stripeRouteUrl = result.client_secret
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType: context.orderType,
      isMobile: false,
      isWechatBrowser: false,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
    })

    if (decision.kind !== 'qr_waiting' || !decision.paymentState.qrCode) {
      return false
    }

    errorMessage.value = ''
    errorHintMessage.value = ''
    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)
    appStore.showWarning(t('payment.errors.mobilePaymentFallbackToQr'))
    return true
  } catch {
    return false
  }
}

function applyScenarioError(err: unknown, paymentMethod: string): boolean {
  const descriptor = describePaymentScenarioError(err, {
    paymentMethod,
    isMobile: isMobileDevice(),
    isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
  })
  if (!descriptor) {
    errorMessage.value = ''
    errorHintMessage.value = ''
    return false
  }
  errorMessage.value = t(descriptor.messageKey)
  errorHintMessage.value = descriptor.hintKey ? t(descriptor.hintKey) : ''
  appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  return true
}

async function resumeWechatPaymentFromQuery() {
  const resume = parseWechatResumeRoute(route.query, checkout.value.plans, validAmount.value)
  if (!resume) {
    return
  }

  selectedMethod.value = resume.paymentType
  if (resume.orderType === 'balance' && resume.orderAmount > 0) {
    amount.value = resume.orderAmount
  }
  if (resume.orderType === 'subscription' && resume.planId) {
    selectedPlan.value = checkout.value.plans.find(plan => plan.id === resume.planId) ?? null
  }

  await router.replace({ path: route.path, query: stripWechatResumeQuery(route.query) })

  if (resume.wechatResumeToken) {
    await createOrder(0, resume.orderType, resume.planId, {
      wechatResumeToken: resume.wechatResumeToken,
      paymentType: resume.paymentType,
      isResume: true,
    })
    return
  }

  if (resume.orderAmount > 0 && resume.openid) {
    await createOrder(resume.orderAmount, resume.orderType, resume.planId, {
      openid: resume.openid,
      paymentType: resume.paymentType,
      isResume: true,
    })
  }
}

onMounted(async () => {
  try {
    const res = await paymentAPI.getCheckoutInfo()
    checkout.value = res.data
    if (enabledMethods.value.length) {
      const order: readonly string[] = METHOD_ORDER
      const sorted = [...enabledMethods.value].sort((a, b) => {
        const ai = order.indexOf(a)
        const bi = order.indexOf(b)
        return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
      })
      selectedMethod.value = sorted[0]
    }
    if (typeof window !== 'undefined') {
      if (hasWechatResumeQuery(route.query)) {
        removeRecoverySnapshot()
      }
      const routeResumeToken = typeof route.query.resume_token === 'string'
        ? route.query.resume_token
        : typeof route.query.wechat_resume_token === 'string'
          ? route.query.wechat_resume_token
          : undefined
      const restored = readPaymentRecoverySnapshot(
        window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
        { resumeToken: routeResumeToken },
      )
      if (restored) {
        paymentState.value = restored
        paymentPhase.value = 'paying'
        const restoredMethod = normalizeVisibleMethod(restored.paymentType)
          || (visibleMethods.value[restored.paymentType] ? restored.paymentType : '')
        if (restoredMethod) {
          selectedMethod.value = restoredMethod
        }
      } else {
        removeRecoverySnapshot()
      }
    }
    await resumeWechatPaymentFromQuery()
    if (checkout.value.balance_disabled) {
      activeTab.value = 'subscription'
    }
    if (route.query.tab === 'subscription') {
      activeTab.value = 'subscription'
      const planId = Number(route.query.plan)
      if (planId > 0) {
        const plan = checkout.value.plans.find(item => item.id === planId)
        if (plan) {
          await selectPlan(plan)
        }
      } else if (route.query.group) {
        const groupId = Number(route.query.group)
        const groupPlans = checkout.value.plans.filter(plan => isPlanAvailableForGroup(plan, groupId))
        if (groupPlans.length === 1) {
          await selectPlan(groupPlans[0])
        } else if (groupPlans.length > 1) {
          renewGroupId.value = groupId
          showRenewalModal.value = true
        }
      }
    }
  } catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { loading.value = false }
  // Fetch active subscriptions (uses cache, non-blocking)
  subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
useLocaleRefresh(async () => {
  const { data } = await paymentAPI.getCheckoutInfo()
  checkout.value = data
  if (selectedPlan.value) selectedPlan.value = data.plans.find(plan => plan.id === selectedPlan.value?.id) || null
})
</script>

<style scoped>
/* 横向翻页期间裁剪内容，静止时恢复摘要吸顶和浮层布局。 */
.purchase-viewport:has(.purchase-slide-enter-active, .purchase-slide-leave-active) {
  overflow: clip;
}

.purchase-slide-enter-active,
.purchase-slide-leave-active {
  transition: transform var(--motion-layout) var(--motion-ease);
}

/* 退出面板绝对定位，与进入面板重叠在同一视窗。 */
.purchase-slide-leave-active {
  position: absolute;
  inset: 0 0 auto;
}

.purchase-slide-enter-from {
  transform: translateX(calc(var(--purchase-direction) * 100%));
}

.purchase-slide-leave-to {
  transform: translateX(calc(var(--purchase-direction) * -100%));
}
</style>
