<template>
  <aside
    class="sidebar"
    :class="[
      sidebarCollapsed ? 'w-[var(--sidebar-w-collapsed)]' : 'w-[var(--sidebar-w)]',
      { '-translate-x-full lg:translate-x-0': !mobileOpen }
    ]"
  >
    <!-- Navigation -->
    <nav ref="sidebarNavRef" class="sidebar-nav scrollbar-hide">
      <!-- Admin View: Admin menu first, then personal menu -->
      <template v-if="isAdmin">
        <!-- Admin Section -->
        <div class="sidebar-section">
          <template v-for="item in adminNavItems" :key="item.path">
            <!-- Collapsible group (has children) -->
            <template v-if="item.children?.length">
              <button
                type="button"
                class="sidebar-link mb-1 w-full"
                :class="{
                  'sidebar-link-active': isGroupActive(item) && !isGroupExpanded(item),
                  'sidebar-link-collapsed': sidebarCollapsed
                }"
                :title="sidebarCollapsed ? item.label : undefined"
                :aria-expanded="!sidebarCollapsed && isGroupExpanded(item)"
                :aria-controls="`sidebar-group-${item.path}`"
                @click="sidebarCollapsed ? undefined : toggleGroup(item)"
              >
                <Icon :name="item.icon ?? 'home'" size="md" class="flex-shrink-0" />
                <span
                  class="sidebar-label sidebar-label-flex"
                  :class="{ 'sidebar-label-collapsed': sidebarCollapsed }"
                  :aria-hidden="sidebarCollapsed ? 'true' : 'false'"
                >
                  <span class="min-w-0 truncate">{{ item.label }}</span>
                  <Icon
                    name="chevronDown"
                    size="sm"
                    :animate-on-hover="false"
                    class="h-4 w-4 flex-shrink-0 transition-transform duration-normal"
                    :class="isGroupExpanded(item) ? 'rotate-180' : ''"
                  />
                </span>
              </button>
              <!-- Children -->
              <Collapse :id="`sidebar-group-${item.path}`" :open="!sidebarCollapsed && isGroupExpanded(item)" unmount-on-hide>
                <div class="mb-1 ml-4 border-l border-primary-900/10 pl-2 dark:border-dark-600">
                  <router-link
                    v-for="child in item.children"
                    :key="child.path"
                    :to="child.path"
                    class="sidebar-link mb-0.5 py-1.5 text-sm"
                    :class="{ 'sidebar-link-active': route.path === child.path }"
                    @click="handleMenuItemClick(child.path)"
                  >
                    <Icon :name="child.icon ?? 'home'" size="sm" class="h-4 w-4 flex-shrink-0" />
                    <span>{{ child.label }}</span>
                  </router-link>
                </div>
              </Collapse>
            </template>
            <!-- Normal item (no children) -->
            <router-link
              v-else
              :to="item.path"
              class="sidebar-link mb-1"
              :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
              :title="sidebarCollapsed ? item.label : undefined"
              :id="
                item.path === '/admin/providers'
                  ? 'sidebar-channel-manage'
                  : item.path === '/admin/groups'
                    ? 'sidebar-group-manage'
                    : item.path === '/admin/redeem'
                      ? 'sidebar-wallet'
                      : undefined
              "
              @click="handleMenuItemClick(item.path)"
            >
              <span v-if="item.iconSvg" class="flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
              <Icon v-else :name="item.icon ?? 'home'" size="md" class="flex-shrink-0" />
              <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
            </router-link>
          </template>
        </div>

        <!-- 管理员的个人功能区 -->
        <div class="sidebar-section">
          <div class="sidebar-section-title" :class="{ 'sidebar-section-title-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
            <span class="sidebar-section-title-text" :class="{ 'sidebar-section-title-text-collapsed': sidebarCollapsed }">
              {{ t('nav.myAccount') }}
            </span>
          </div>

          <router-link
            v-for="item in personalNavItems"
            :key="item.path"
            :to="item.path"
            class="sidebar-link mb-1"
            :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
            :title="sidebarCollapsed ? item.label : undefined"
            :data-tour="item.path === '/keys' ? 'sidebar-my-keys' : item.path === '/usage' ? 'sidebar-usage' : undefined"
            @click="handleMenuItemClick(item.path)"
          >
            <span v-if="item.iconSvg" class="flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
            <Icon v-else :name="item.icon ?? 'home'" size="md" class="flex-shrink-0" />
            <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
          </router-link>
        </div>
      </template>

      <!-- Regular User View -->
      <template v-else-if="!appStore.backendModeEnabled">
        <div class="sidebar-section">
          <router-link
            v-for="item in userNavItems"
            :key="item.path"
            :to="item.path"
            class="sidebar-link mb-1"
            :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
            :title="sidebarCollapsed ? item.label : undefined"
            :data-tour="item.path === '/keys' ? 'sidebar-my-keys' : item.path === '/usage' ? 'sidebar-usage' : undefined"
            @click="handleMenuItemClick(item.path)"
          >
            <span v-if="item.iconSvg" class="flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
            <Icon v-else :name="item.icon ?? 'home'" size="md" class="flex-shrink-0" />
            <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
          </router-link>
        </div>
      </template>
    </nav>

  </aside>

  <!-- Mobile Overlay -->
  <MotionTransition name="fade">
    <div
      v-if="mobileOpen"
      class="mobile-overlay fixed inset-x-0 bottom-0 top-[var(--header-h)] z-sidebar-overlay bg-black/50 lg:hidden"
      @click="closeMobile"
    ></div>
  </MotionTransition>
</template>

<script setup lang="ts">
import MotionTransition from '@/components/common/MotionTransition.vue'
import Collapse from '@/components/common/Collapse.vue'

import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { IconName } from '@/components/icons/registry'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore, useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { sanitizeSvg } from '@/utils/sanitize'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'

interface NavItem {
  path: string
  label: string
  icon: IconName | null
  iconSvg?: string
  // featureFlag 返回 false 时隐藏菜单项，用于按公开设置控制可选入口。
  featureFlag?: () => boolean
  children?: NavItem[]
}

const { t } = useI18n()

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const adminSettingsStore = useAdminSettingsStore()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)

// 移动端抽屉的收起挂在导航完成事件上，取代原来 150ms 的定时器猜测：
// 路由一变，抽屉里那份导航清单就过时了，所以不限于菜单点击引发的导航。
watch(
  () => route.fullPath,
  () => {
    if (mobileOpen.value) appStore.setMobileOpen(false)
  }
)
const isAdmin = computed(() => authStore.isAdmin)
const sidebarNavRef = ref<HTMLElement | null>(null)

// 未手动操作的分组随当前路由展开；手动选择优先，允许收起当前分组。
const groupExpandOverrides = ref<Map<string, boolean>>(new Map())

// 批量图片入口还需要用户 API Key 和分组权限同时满足。
const flagBatchImageAccess = () => canUseBatchImage.value
// 创作台入口由功能开关控制（默认开），可用模型以页面内目录为准。
const flagCreativeStudioAccess = () => appStore.cachedPublicSettings?.creative_enabled !== false
const flagTeamAccess = () => appStore.cachedPublicSettings?.team_enabled !== false
const flagUsageRankingAccess = () => appStore.cachedPublicSettings?.usage_ranking_enabled !== false

// 普通用户导航项。
const userNavItems = computed((): NavItem[] => {
  const items: NavItem[] = [
    { path: '/dashboard', label: t('nav.dashboard'), icon: 'dashboard' as const },
    { path: '/models', label: t('nav.modelMarketplace'), icon: 'modelMarketplace' as const },
    { path: '/usage-ranking', label: t('nav.usageRanking'), icon: 'ranking' as const, featureFlag: flagUsageRankingAccess },
    { path: '/keys', label: t('nav.apiKeys'), icon: 'key' as const },
    { path: '/team', label: t('nav.team'), icon: 'users' as const, featureFlag: flagTeamAccess },
    { path: '/batch-image', label: t('nav.batchImage'), icon: 'batchImage' as const, featureFlag: flagBatchImageAccess },
    { path: '/creative', label: t('nav.creative'), icon: 'creative' as const, featureFlag: flagCreativeStudioAccess },
    { path: '/usage', label: t('nav.usage'), icon: 'chart' as const },
    { path: '/subscriptions', label: t('nav.mySubscriptions'), icon: 'creditCard' as const },
    ...(appStore.cachedPublicSettings?.payment_enabled
      ? [
          {
            path: '/purchase',
            label: t('nav.buySubscription'),
            icon: 'recharge' as const,
          },
        ]
      : []),
    ...(appStore.cachedPublicSettings?.payment_enabled
      ? [
          {
            path: '/orders',
            label: t('nav.myOrders'),
            icon: 'orderList' as const,
          },
        ]
      : []),
    { path: '/redeem', label: t('nav.redeem'), icon: 'gift' as const },
    ...(appStore.cachedPublicSettings?.affiliate_enabled === true
      ? [
          {
            path: '/affiliate',
            label: t('nav.affiliate'),
            icon: 'affiliate' as const,
          },
        ]
      : []),
    { path: '/profile', label: t('nav.profile'), icon: 'user' as const },
    ...customMenuItemsForUser.value.map((item): NavItem => ({
      path: `/custom/${item.id}`,
      label: item.label,
      icon: null,
      iconSvg: item.icon_svg,
    })),
  ]
  const visibleItems = items.filter(item => item.featureFlag?.() !== false)
  return visibleItems
})

// 管理员“我的提供商”分组使用的个人导航项
const personalNavItems = computed((): NavItem[] => {
  const items: NavItem[] = [
    { path: '/dashboard', label: t('nav.dashboard'), icon: 'dashboard' as const },
    { path: '/models', label: t('nav.modelMarketplace'), icon: 'modelMarketplace' as const },
    { path: '/usage-ranking', label: t('nav.usageRanking'), icon: 'ranking' as const, featureFlag: flagUsageRankingAccess },
    { path: '/keys', label: t('nav.apiKeys'), icon: 'key' as const },
    { path: '/team', label: t('nav.team'), icon: 'users' as const, featureFlag: flagTeamAccess },
    { path: '/batch-image', label: t('nav.batchImage'), icon: 'batchImage' as const, featureFlag: flagBatchImageAccess },
    { path: '/creative', label: t('nav.creative'), icon: 'creative' as const, featureFlag: flagCreativeStudioAccess },
    { path: '/usage', label: t('nav.usage'), icon: 'chart' as const },
    { path: '/subscriptions', label: t('nav.mySubscriptions'), icon: 'creditCard' as const },
    ...(appStore.cachedPublicSettings?.payment_enabled
      ? [
          {
            path: '/purchase',
            label: t('nav.buySubscription'),
            icon: 'recharge' as const,
          },
        ]
      : []),
    ...(appStore.cachedPublicSettings?.payment_enabled
      ? [
          {
            path: '/orders',
            label: t('nav.myOrders'),
            icon: 'orderList' as const,
          },
        ]
      : []),
    { path: '/redeem', label: t('nav.redeem'), icon: 'gift' as const },
    ...(appStore.cachedPublicSettings?.affiliate_enabled === true
      ? [
          {
            path: '/affiliate',
            label: t('nav.affiliate'),
            icon: 'affiliate' as const,
          },
        ]
      : []),
    { path: '/profile', label: t('nav.profile'), icon: 'user' as const },
    ...customMenuItemsForUser.value.map((item): NavItem => ({
      path: `/custom/${item.id}`,
      label: item.label,
      icon: null,
      iconSvg: item.icon_svg,
    })),
  ]
  const visibleItems = items.filter(item => item.featureFlag?.() !== false)
  return visibleItems
})

// Custom menu items filtered by visibility
const customMenuItemsForUser = computed(() => {
  const items = appStore.cachedPublicSettings?.custom_menu_items ?? []
  return items
    .filter((item) => item.visibility === 'user')
    .sort((a, b) => a.sort_order - b.sort_order)
})

const customMenuItemsForAdmin = computed(() => {
  return adminSettingsStore.customMenuItems
    .filter((item) => item.visibility === 'admin')
    .sort((a, b) => a.sort_order - b.sort_order)
})

// Admin navigation items
const adminNavItems = computed((): NavItem[] => {
  const baseItems: NavItem[] = [
    { path: '/admin/dashboard', label: t('nav.dashboard'), icon: 'dashboard' as const },
    ...(adminSettingsStore.opsMonitoringEnabled
      ? [{ path: '/admin/ops', label: t('nav.ops'), icon: 'chart' as const }]
      : []),
    { path: '/admin/users', label: t('nav.users'), icon: 'users' as const },
    { path: '/admin/teams', label: t('nav.teams'), icon: 'users' as const, featureFlag: flagTeamAccess },
    { path: '/admin/groups', label: t('nav.groups'), icon: 'folder' as const },
    {
      path: '/admin/model-management', label: t('nav.modelManagement'), icon: 'pricing' as const,
      children: [
        { path: '/admin/pricing', label: t('nav.pricing'), icon: 'pricing' as const },
        { path: '/admin/model-attributes', label: t('nav.modelAttributes'), icon: 'cog' as const },
      ],
    },
    {
      path: '/admin/subscriptions',
      label: t('nav.subscriptions'),
      icon: 'creditCard' as const,
      children: [
        { path: '/admin/subscriptions', label: t('nav.userSubscriptions'), icon: 'users' as const },
        // 套餐入口随支付功能开放，用户订阅入口始终保留。
        ...(adminSettingsStore.paymentEnabled
          ? [{ path: '/admin/orders/plans', label: t('nav.paymentPlans'), icon: 'creditCard' as const }]
          : []),
      ],
    },
    { path: '/admin/providers', label: t('nav.providers'), icon: 'globe' as const },
    { path: '/admin/quality-probe', label: t('nav.qualityProbe'), icon: 'beaker' as const },
    { path: '/admin/announcements', label: t('nav.announcements'), icon: 'bell' as const },
    { path: '/admin/proxies', label: t('nav.proxies'), icon: 'server' as const },
    {
      path: '/admin/risk-control',
      label: t('nav.riskControl'),
      icon: 'shieldCheck' as const,
      featureFlag: () => appStore.cachedPublicSettings?.risk_control_enabled === true
    },
    { path: '/admin/redeem', label: t('nav.redeemCodes'), icon: 'ticket' as const },
    { path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: 'gift' as const },
    ...(appStore.cachedPublicSettings?.affiliate_enabled === true
      ? [
          {
            path: '/admin/affiliates',
            label: t('nav.affiliateManagement'),
            icon: 'users' as const,
            children: [
              { path: '/admin/affiliates/invites', label: t('nav.affiliateInviteRecords'), icon: 'users' as const },
              { path: '/admin/affiliates/rebates', label: t('nav.affiliateRebateRecords'), icon: 'order' as const },
              { path: '/admin/affiliates/transfers', label: t('nav.affiliateTransferRecords'), icon: 'creditCard' as const },
            ],
          },
        ]
      : []),
    ...(adminSettingsStore.paymentEnabled
      ? [
          {
            path: '/admin/orders',
            label: t('nav.orderManagement'),
            icon: 'order' as const,
            children: [
              { path: '/admin/orders/dashboard', label: t('nav.paymentDashboard'), icon: 'chart' as const },
              { path: '/admin/orders', label: t('nav.orderManagement'), icon: 'order' as const },
            ],
          },
        ]
      : []),
    { path: '/admin/usage', label: t('nav.usage'), icon: 'chart' as const },
    { path: '/admin/audit-logs', label: t('nav.auditLogs'), icon: 'shieldCheck' as const }
  ]

  const visibleItems = baseItems.filter(item => item.featureFlag?.() !== false)
  visibleItems.push({ path: '/admin/settings', label: t('nav.settings'), icon: 'cog' as const })
  // Add admin custom menu items after settings
  for (const cm of customMenuItemsForAdmin.value) {
    visibleItems.push({ path: `/custom/${cm.id}`, label: cm.label, icon: null, iconSvg: cm.icon_svg })
  }
  return visibleItems
})

function closeMobile() {
  appStore.setMobileOpen(false)
}

function handleMenuItemClick(itemPath: string) {
  // 点击当前路由不会触发导航（router-link 去重），上面的 route 监听不会命中，这里立即收起。
  if (mobileOpen.value && itemPath === route.path) {
    appStore.setMobileOpen(false)
  }

  // Map paths to tour selectors
  const pathToSelector: Record<string, string> = {
    '/admin/groups': '#sidebar-group-manage',
    '/admin/providers': '#sidebar-channel-manage',
    '/keys': '[data-tour="sidebar-my-keys"]',
    '/usage': '[data-tour="sidebar-usage"]'
  }

  const selector = pathToSelector[itemPath]
  if (selector && onboardingStore.isCurrentStep(selector)) {
    onboardingStore.nextStep(500)
  }
}

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(path + '/')
}

function isGroupActive(item: NavItem): boolean {
  if (!item.children) return false
  return item.children.some(child => route.path === child.path)
}

function isGroupExpanded(item: NavItem): boolean {
  const override = groupExpandOverrides.value.get(item.path)
  if (override !== undefined) return override
  return isGroupActive(item)
}

function toggleGroup(item: NavItem) {
  groupExpandOverrides.value.set(item.path, !isGroupExpanded(item))
}

// Fetch admin settings (for feature-gated nav items like Ops).
watch(
  isAdmin,
  (v) => {
    if (v) {
      adminSettingsStore.fetch()
    }
  },
  { immediate: true }
)

onMounted(() => {
  void refreshBatchImageAccess()
  if (isAdmin.value) {
    adminSettingsStore.fetch()
  }
  // 路由切换重新挂载组件后恢复侧边栏滚动位置。
  if (appStore.sidebarScrollTop > 0 && sidebarNavRef.value) {
    void nextTick(() => {
      if (sidebarNavRef.value) {
        sidebarNavRef.value.scrollTop = appStore.sidebarScrollTop
      }
    })
  }
})

onBeforeUnmount(() => {
  if (sidebarNavRef.value) {
    appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop
  }
})
</script>

<style scoped>
.sidebar-link-collapsed {
  gap: 0;
  padding-left: 0.875rem;
  padding-right: 0.875rem;
}

.sidebar-section-title {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 1.25rem;
  overflow: hidden;
  white-space: nowrap;
}

.sidebar-section-title-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    opacity var(--motion-fast) var(--motion-ease),
    transform var(--motion-fast) var(--motion-ease);
}

.sidebar-section-title::after {
  content: '';
  position: absolute;
  left: 0.75rem;
  right: 0.75rem;
  top: 50%;
  height: 1px;
  background: rgb(229 231 235);
  opacity: 0;
  transform: translateY(-50%);
  transition: opacity var(--motion-fast) var(--motion-ease);
}

.dark .sidebar-section-title::after {
  background: theme('borderColor.dark.700');
}

.sidebar-section-title-text-collapsed {
  opacity: 0;
  transform: translateX(-4px);
}

.sidebar-section-title-collapsed::after {
  opacity: 1;
}

.sidebar-label {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    max-width var(--motion-layout) var(--motion-ease),
    opacity var(--motion-fast) var(--motion-ease),
    transform var(--motion-fast) var(--motion-ease);
  max-width: 12rem;
}

.sidebar-label-flex {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.sidebar-label-collapsed {
  max-width: 0;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}

/* 自定义图标与导航图标同尺寸，保留上传 SVG 自身的颜色。 */
.sidebar-svg-icon {
  width: 1.125rem;
  height: 1.125rem;
  color: currentColor;
}

.sidebar-svg-icon :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
}
</style>
