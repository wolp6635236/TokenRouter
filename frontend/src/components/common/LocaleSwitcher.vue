<template>
  <div class="relative" ref="dropdownRef">
    <button
      @click="toggleDropdown"
      :disabled="switching"
      :class="triggerClass"
      :title="currentLocale?.name"
    >
      <!-- 顶栏状态变体用语言图标（文/A 字形）与相邻图标按钮对齐，也避开国旗 emoji 在 Windows 上无法渲染的问题。 -->
      <Icon v-if="variant === 'status'" name="languages" size="md" />
      <span v-else class="text-base leading-none">{{ currentLocale?.flag }}</span>
      <span v-if="variant !== 'status'" class="hidden sm:inline">{{ currentLocale?.code.toUpperCase() }}</span>
      <Icon
        v-if="variant !== 'status'"
        name="chevronDown"
        size="xs"
        class="text-gray-400 transition-transform duration-normal"
        :class="{ 'rotate-180': isOpen }"
        :animate-on-hover="false"
      />
    </button>

    <!-- 语言列表用顶栏的内缩菜单样式，当前语言文字转为品牌色并在行尾打勾。 -->
    <MotionTransition name="dropdown-fade">
      <div
        v-if="isOpen"
        class="dropdown right-0 z-50 mt-2 w-44 origin-top-right py-0"
      >
        <div class="menu-section">
          <button
            v-for="locale in availableLocales"
            :key="locale.code"
            type="button"
            :disabled="switching"
            @click="selectLocale(locale.code)"
            class="menu-item whitespace-nowrap"
            :class="{ 'text-primary-700 dark:text-primary-500': locale.code === currentLocaleCode }"
          >
            <span class="text-base leading-none">{{ locale.flag }}</span>
            <span class="min-w-0 truncate">{{ locale.name }}</span>
            <Icon
              v-if="locale.code === currentLocaleCode"
              name="check"
              size="sm"
              class="ml-auto shrink-0"
              :animate-on-hover="false"
            />
          </button>
        </div>
      </div>
    </MotionTransition>
  </div>
</template>

<script setup lang="ts">
import MotionTransition from '@/components/common/MotionTransition.vue'
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { setLocale, availableLocales } from '@/i18n'

const props = withDefaults(defineProps<{
  variant?: 'default' | 'status'
}>(), {
  variant: 'default'
})

const { locale } = useI18n()

const isOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const switching = ref(false)

const currentLocaleCode = computed(() => locale.value)
const currentLocale = computed(() => availableLocales.find((l) => l.code === locale.value))
const variant = computed(() => props.variant)
const triggerClass = computed(() => {
  if (variant.value === 'status') {
    return 'flex h-9 w-9 items-center justify-center rounded-control text-primary-900 transition-colors hover:bg-primary-100 disabled:cursor-not-allowed disabled:opacity-60 dark:text-dark-100 dark:hover:bg-dark-700 dark:hover:text-white'
  }
  return 'flex items-center gap-1.5 rounded-control px-2 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-60 dark:text-gray-300 dark:hover:bg-dark-700'
})

function toggleDropdown() {
  isOpen.value = !isOpen.value
}

async function selectLocale(code: string) {
  if (switching.value || code === currentLocaleCode.value) {
    isOpen.value = false
    return
  }
  switching.value = true
  try {
    await setLocale(code)
    isOpen.value = false
  } finally {
    switching.value = false
  }
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    isOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
</style>
