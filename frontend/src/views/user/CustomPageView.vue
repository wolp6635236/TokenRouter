<template>
  <AppLayout fit-viewport="all">
    <div class="custom-page-layout">
      <div class="card flex-1 min-h-0 overflow-hidden">
        <ContentSkeleton v-if="loading" variant="article" :rows="4" class="h-full overflow-hidden p-6" />

        <div
          v-else-if="!menuItem"
          class="flex h-full items-center justify-center p-10 text-center"
        >
          <EmptyState :title="t('customPage.notFoundTitle')" :description="t('customPage.notFoundDesc')">
            <template #icon>
              <Icon name="link" class="empty-state-icon h-10 w-10" />
            </template>
          </EmptyState>
        </div>

        <div v-else-if="isMarkdownMode" class="flex h-full overflow-hidden">
          <MotionTransition persisted name="toc">
            <aside v-show="tocVisible" class="toc-sidebar">
              <div class="toc-header">
                <span class="toc-title">{{ t('customPage.toc') }}</span>
                <button class="toc-close-btn" type="button" :title="t('customPage.hideToc')" @click="tocVisible = false">
                  <Icon name="chevronLeft" size="sm" :animate-on-hover="false" />
                </button>
              </div>
              <nav class="toc-nav">
                <a
                  v-for="item in tocItems"
                  :key="item.id"
                  :href="'#' + item.id"
                  class="toc-item"
                  :class="[`toc-level-${item.level}`, { 'toc-active': activeHeadingId === item.id }]"
                  @click.prevent="scrollToHeading(item.id)"
                >
                  {{ item.text }}
                </a>
              </nav>
            </aside>
          </MotionTransition>

          <button
            v-show="!tocVisible && tocItems.length > 0"
            type="button"
            class="toc-toggle-btn"
            :title="t('customPage.showToc')"
            @click="tocVisible = true"
          >
            <Icon name="menu" size="sm" />
            <span class="ml-1 text-xs">{{ t('customPage.toc') }}</span>
          </button>

          <div
            ref="markdownContainer"
            class="markdown-page-content flex-1 h-full overflow-auto p-6 md:p-10"
            v-html="renderedHtml"
            @scroll="onContentScroll"
          ></div>
        </div>

        <div v-else-if="!isValidUrl" class="flex h-full items-center justify-center p-10 text-center">
          <EmptyState :title="t('customPage.notConfiguredTitle')" :description="t('customPage.notConfiguredDesc')">
            <template #icon>
              <Icon name="link" class="empty-state-icon h-10 w-10" />
            </template>
          </EmptyState>
        </div>

        <div v-else class="custom-embed-shell">
          <a
            :href="embeddedUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-secondary btn-sm custom-open-fab"
          >
            <Icon name="externalLink" size="sm" class="mr-1.5" :stroke-width="2" />
            {{ t('customPage.openInNewTab') }}
          </a>
          <iframe
            :src="embeddedUrl"
            class="custom-embed-frame"
            allowfullscreen
          ></iframe>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import ContentSkeleton from '@/components/common/ContentSkeleton.vue'
import MotionTransition from '@/components/common/MotionTransition.vue'

import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { buildApiUrl } from '@/api/client'
import { COPY_FEEDBACK_MS } from '@/constants/ui'
import { buildEmbeddedUrl, detectTheme } from '@/utils/embedded-url'
import { useMediaQuery } from '@vueuse/core'
import { MEDIA_MIN_MD, MEDIA_MAX_SM } from '@/constants/layout'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

interface TocItem {
  id: string
  text: string
  level: number
}

const { t, locale } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()

const loading = ref(false)
const pageTheme = ref<'light' | 'dark'>('light')
const renderedHtml = ref('')
const markdownContainer = ref<HTMLElement | null>(null)
const tocItems = ref<TocItem[]>([])
// 目录初始可见性按 md 断点取一次性快照;之后由用户手动开关,不随 resize 自动改。
const tocVisible = ref(useMediaQuery(MEDIA_MIN_MD).value)
const activeHeadingId = ref('')
let themeObserver: MutationObserver | null = null
let scrollRafId = 0

const menuItemId = computed(() => route.params.id as string)

const menuItem = computed(() => {
  const id = menuItemId.value
  // 优先读取公共配置，普通用户只会拿到 user 可见菜单。
  const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
  const found = publicItems.find((item) => item.id === id) ?? null
  if (found) return found
  // 管理员页面允许从完整后台配置中读取 admin-only 菜单。
  if (authStore.isAdmin) {
    return adminSettingsStore.customMenuItems.find((item) => item.id === id) ?? null
  }
  return null
})

const markdownSlug = computed(() => {
  const item = menuItem.value
  if (!item) return ''
  if (item.page_slug) return item.page_slug
  if (item.url?.startsWith('md:')) return item.url.slice(3)
  return ''
})

const isMarkdownMode = computed(() => !!markdownSlug.value)

const embeddedUrl = computed(() => {
  if (!menuItem.value || isMarkdownMode.value) return ''
  return buildEmbeddedUrl(
    menuItem.value.url,
    authStore.user?.id,
    authStore.token,
    pageTheme.value,
    locale.value,
  )
})

const isValidUrl = computed(() => {
  if (isMarkdownMode.value) return false
  const url = embeddedUrl.value
  return url.startsWith('http://') || url.startsWith('https://')
})

function generateHeadingId(text: string, index: number): string {
  const base = text
    .toLowerCase()
    .replace(/[^\w一-鿿]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return base ? `${base}-${index}` : `heading-${index}`
}

function isRelativeMarkdownAsset(src: string): boolean {
  const trimmed = src.trim()
  if (!trimmed || /^[a-z][a-z0-9+.-]*:/i.test(trimmed) || trimmed.startsWith('//') || trimmed.startsWith('/')) {
    return false
  }
  const [pathPart] = trimmed.split(/([?#].*)/, 2)
  return pathPart
    .split('/')
    .filter((part) => part && part !== '.')
    .every((part) => part !== '..' && !part.includes('\\'))
}

function buildPageImageUrl(slug: string, src: string, language: string): string {
  const trimmed = src.trim()
  const [pathPart, suffix = ''] = trimmed.split(/([?#].*)/, 2)
  const encodedPath = pathPart
    .split('/')
    .filter((part) => part && part !== '.')
    .map((part) => encodeURIComponent(part))
    .join('/')
  const url = buildApiUrl(`/pages/${encodeURIComponent(slug)}/images/${encodedPath}${suffix}`)
  const [base, fragment] = url.split('#', 2)
  return language ? `${base}${base.includes('?') ? '&' : '?'}locale=${encodeURIComponent(language)}${fragment ? '#' + fragment : ''}` : url
}

let markdownGeneration = 0

async function fetchAndRenderMarkdown(slug: string) {
  const generation = ++markdownGeneration
  loading.value = true
  tocItems.value = []
  activeHeadingId.value = ''
  try {
    const resp = await fetch(buildApiUrl(`/pages/${encodeURIComponent(slug)}`), {
      headers: { 'Accept-Language': locale.value, ...(authStore.token ? { Authorization: `Bearer ${authStore.token}` } : {}) },
    })
    if (generation !== markdownGeneration) return
    if (!resp.ok) {
      renderedHtml.value = `<p class="text-red-500">${t('customPage.markdownNotFound')}</p>`
      return
    }
    let raw = await resp.text()
    if (generation !== markdownGeneration) return
    const contentLanguage = resp.headers.get('Content-Language') || ''

    raw = raw.replace(
      /!\[([^\]]*)\]\(([^)]+)\)/g,
      (match, alt, src) => isRelativeMarkdownAsset(src) ? `![${alt}](${buildPageImageUrl(slug, src, contentLanguage)})` : match
    )

    const html = marked.parse(raw) as string
    const sanitized = DOMPurify.sanitize(html, {
      ADD_TAGS: ['iframe'],
      ADD_ATTR: ['allowfullscreen', 'frameborder', 'src'],
    })

    const toc: TocItem[] = []
    let headingIndex = 0
    const withIds = sanitized.replace(
      /<(h[1-4])[^>]*>(.*?)<\/h[1-4]>/gi,
      (_, tag: string, content: string) => {
        const level = Number.parseInt(tag[1], 10)
        const text = content.replace(/<[^>]+>/g, '').trim()
        const id = generateHeadingId(text, headingIndex++)
        toc.push({ id, text, level })
        return `<${tag} id="${id}">${content}</${tag}>`
      }
    )

    renderedHtml.value = withIds
    tocItems.value = toc
  } catch {
    renderedHtml.value = `<p class="text-red-500">${t('customPage.markdownLoadFailed')}</p>`
  } finally {
    if (generation === markdownGeneration) loading.value = false
    await nextTick()
    await nextTick()
    injectCopyButtons()
  }
}

function scrollToHeading(id: string) {
  const container = markdownContainer.value
  if (!container) return
  const el = container.querySelector(`#${CSS.escape(id)}`)
  if (!el) return
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeHeadingId.value = id
  // 小屏(低于 sm)跳转锚点后收起目录;max = min - 1px 约定,与 sm: 样式互斥。
  if (window.matchMedia(MEDIA_MAX_SM).matches) {
    tocVisible.value = false
  }
}

function onContentScroll() {
  if (scrollRafId) return
  scrollRafId = requestAnimationFrame(() => {
    scrollRafId = 0
    const container = markdownContainer.value
    if (!container || tocItems.value.length === 0) return

    const containerRect = container.getBoundingClientRect()
    let current = ''
    for (const item of tocItems.value) {
      const el = container.querySelector(`#${CSS.escape(item.id)}`) as HTMLElement | null
      if (!el) continue
      const elRect = el.getBoundingClientRect()
      if (elRect.top - containerRect.top <= 100) {
        current = item.id
      }
    }
    activeHeadingId.value = current
  })
}

function injectCopyButtons() {
  const container = markdownContainer.value
  if (!container) return

  container.querySelectorAll('pre').forEach((pre) => {
    if (pre.querySelector('.copy-btn')) return
    const btn = document.createElement('button')
    btn.className = 'copy-btn'
    btn.textContent = t('common.copy')
    btn.addEventListener('click', async () => {
      const code = pre.querySelector('code')?.textContent ?? pre.textContent ?? ''
      try {
        await navigator.clipboard.writeText(code)
        btn.textContent = t('common.copied')
        window.setTimeout(() => { btn.textContent = t('common.copy') }, COPY_FEEDBACK_MS)
      } catch {
        btn.textContent = t('common.copyFailed')
        window.setTimeout(() => { btn.textContent = t('common.copy') }, COPY_FEEDBACK_MS)
      }
    })
    ;(pre as HTMLElement).style.position = 'relative'
    pre.appendChild(btn)
  })
}

watch([markdownSlug, locale], ([slug]) => {
  if (slug) {
    fetchAndRenderMarkdown(slug)
  } else {
    renderedHtml.value = ''
    tocItems.value = []
  }
}, { immediate: true })

onMounted(async () => {
  pageTheme.value = detectTheme()

  if (typeof document !== 'undefined') {
    themeObserver = new MutationObserver(() => {
      pageTheme.value = detectTheme()
    })
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
  }

  if (appStore.publicSettingsLoaded) return
  loading.value = true
  try {
    await appStore.fetchPublicSettings()
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  if (scrollRafId) {
    cancelAnimationFrame(scrollRafId)
    scrollRafId = 0
  }
  if (themeObserver) {
    themeObserver.disconnect()
    themeObserver = null
  }
})
</script>

<style scoped>
.custom-page-layout {
  /* AppLayout 锁定视口高度后，flex 链把页头下方的剩余空间分配给 iframe 或 Markdown 滚动区。 */
  @apply flex min-h-0 flex-1 flex-col;
}

.toc-sidebar {
  @apply flex flex-col h-full border-r border-gray-200 dark:border-dark-600 bg-gray-50 dark:bg-dark-800;
  width: min(240px, 30%);
  min-width: 160px;
  max-width: 280px;
  overflow: hidden;
}

@media (max-width: 639px) { /* 639 = BREAKPOINT_SM(640) - 1,断点数值唯一来源在 constants/layout.ts */
  .toc-sidebar {
    position: absolute;
    left: 0;
    top: 0;
    z-index: 20; /* check-ui-allow: 自定义页目录抽屉局部层级 */
    width: 70%;
    max-width: 240px;
    height: 100%;
    box-shadow: 2px 0 8px rgba(0, 0, 0, 0.1);
  }
}

/* 桌面目录通过宽度折叠，窄屏抽屉通过位移显隐，正文宽度保持不变。 */
.toc-enter-active,
.toc-leave-active {
  transition: width var(--motion-layout) var(--motion-ease),
    min-width var(--motion-layout) var(--motion-ease),
    opacity var(--motion-layout) var(--motion-ease);
}
.toc-enter-from,
.toc-leave-to {
  width: 0;
  min-width: 0;
  opacity: 0;
}
/* 窄屏目录的断点取 BREAKPOINT_SM 减 1。 */
@media (max-width: 639px) {
  .toc-enter-active,
  .toc-leave-active {
    transition: transform var(--motion-layout) var(--motion-ease), opacity var(--motion-layout) var(--motion-ease);
  }
  .toc-enter-from,
  .toc-leave-to {
    width: 70%;
    min-width: 160px;
    transform: translateX(-100%);
  }
}
@media (prefers-reduced-motion: reduce) {
  .toc-enter-from,
  .toc-leave-to { transform: none; }
}

.toc-header {
  @apply flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-dark-600;
}

.toc-title {
  @apply text-sm font-semibold text-gray-700 dark:text-dark-200;
}

.toc-close-btn {
  @apply p-1 rounded-compact text-gray-400 hover:text-gray-600 dark:hover:text-dark-200 hover:bg-gray-200 dark:hover:bg-dark-700 transition-colors;
}

.toc-nav {
  @apply flex-1 overflow-y-auto py-2 px-2;
}

.toc-item {
  @apply block px-2 py-1.5 text-sm rounded-compact transition-colors truncate;
  @apply text-gray-600 dark:text-dark-300 hover:text-gray-900 dark:hover:text-white hover:bg-gray-200 dark:hover:bg-dark-700;
}

.toc-item.toc-active {
  @apply text-primary-600 dark:text-primary-500 bg-primary-50 dark:bg-primary-500/8 font-medium;
}

.toc-level-1 { padding-left: 8px; }
.toc-level-2 { padding-left: 20px; }
.toc-level-3 { padding-left: 32px; }
.toc-level-4 { padding-left: 44px; }

.toc-toggle-btn {
  @apply absolute left-2 top-2 z-10 flex items-center px-2 py-1.5 rounded-control text-sm;
  @apply bg-white dark:bg-dark-700 border border-gray-200 dark:border-dark-500;
  @apply text-gray-600 dark:text-dark-300 hover:bg-gray-100 dark:hover:bg-dark-700;
  @apply shadow-sm transition-colors cursor-pointer;
}

.custom-embed-shell {
  @apply relative;
  @apply h-full w-full overflow-hidden rounded-surface;
  @apply bg-gradient-to-b from-gray-50 to-white dark:from-dark-900 dark:to-dark-950;
  @apply p-0;
}

.custom-open-fab {
  @apply absolute right-3 top-3 z-10;
  @apply shadow-sm backdrop-blur supports-[backdrop-filter]:bg-white/80 dark:supports-[backdrop-filter]:bg-dark-800/80;
}

.custom-embed-frame {
  display: block;
  margin: 0;
  width: 100%;
  height: 100%;
  border: 0;
  border-radius: 0;
  box-shadow: none;
  background: transparent;
}
</style>

<style>
.markdown-page-content {
  line-height: 1.7;
  color: inherit;
}
.markdown-page-content h1 { @apply text-3xl font-bold mt-8 mb-4 pb-2 border-b border-gray-200 dark:border-dark-600; }
.markdown-page-content h2 { @apply text-2xl font-bold mt-6 mb-3; }
.markdown-page-content h3 { @apply text-xl font-semibold mt-5 mb-2; }
.markdown-page-content h4 { @apply text-lg font-semibold mt-4 mb-2; }
.markdown-page-content p { @apply mb-4; }
.markdown-page-content ul { @apply list-disc pl-6 mb-4; }
.markdown-page-content ol { @apply list-decimal pl-6 mb-4; }
.markdown-page-content li { @apply mb-1; }
.markdown-page-content a { @apply text-primary-500 hover:text-primary-600 underline; }
.markdown-page-content blockquote { @apply border-l-4 border-gray-300 dark:border-dark-500 pl-4 italic text-gray-600 dark:text-dark-300 my-4; }
.markdown-page-content img { @apply max-w-full h-auto rounded-control my-4; }
.markdown-page-content table { @apply w-full border-collapse my-4; }
.markdown-page-content th { @apply border border-gray-300 dark:border-dark-500 px-3 py-2 bg-gray-50 dark:bg-dark-700 font-semibold text-left; }
.markdown-page-content td { @apply border border-gray-300 dark:border-dark-500 px-3 py-2; }
.markdown-page-content code { @apply bg-gray-100 dark:bg-dark-700 px-1.5 py-0.5 rounded-surface text-sm font-mono; }
.markdown-page-content pre { @apply bg-gray-900 dark:bg-dark-900 text-gray-100 p-4 rounded-surface overflow-x-auto my-4 relative; }
.markdown-page-content pre code { @apply bg-transparent p-0 text-inherit; }
.markdown-page-content hr { @apply my-6 border-gray-200 dark:border-dark-600; }

.copy-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  padding: 4px 10px;
  font-size: 12px;
  border-radius: var(--radius-compact);
  background: rgba(255, 255, 255, 0.15);
  color: #e2e8f0;
  border: 1px solid rgba(255, 255, 255, 0.2);
  cursor: pointer;
  opacity: 0;
  transition: opacity var(--motion-normal), background var(--motion-normal);
  font-family: inherit;
}
.copy-btn:hover { background: rgba(255, 255, 255, 0.25); }
pre:hover .copy-btn { opacity: 1; }
</style>
