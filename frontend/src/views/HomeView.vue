<!-- HomeView.vue renders custom public home content or the built-in marketplace landing page. The built-in page loads public model groups and provider visuals without requesting site sales, usage, token, or user totals. -->
<template>
  <GoogleOneTap :enabled="googleOneTapEligible" :client-id="googleOneTapClientID" />

  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- The built-in landing page appears when custom home content is absent. -->
  <div
    v-else
    class="ba-theme-shell relative flex min-h-screen flex-col overflow-hidden pt-[var(--header-h)] text-gray-950 dark:text-white"
  >
    <div class="ba-theme-backdrop pointer-events-none fixed inset-0"></div>

    <!-- The public page uses the shared fixed header and reserves its layout height. -->
    <AppHeader public-page />

    <main
      v-content-reveal="motionRoute?.path"
      class="relative z-10 flex-1 px-4 pb-20 pt-16 sm:px-6 lg:px-8"
    >
      <section class="mx-auto max-w-5xl text-center">
        <h1
          class="mx-auto max-w-4xl text-4xl font-bold leading-[1.05] tracking-tight text-gray-950 dark:text-white sm:text-5xl md:text-6xl lg:text-7xl"
        >
          {{ homeHeroTitle }}
        </h1>
        <p class="mx-auto mt-6 max-w-2xl text-lg leading-8 text-gray-600 dark:text-dark-300">
          {{ homeHeroSubtitle }}
        </p>

        <div class="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row">
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-[44px] min-w-[180px] items-center justify-center gap-2 rounded-control bg-primary-600 px-6 py-3 text-sm font-semibold text-white shadow-none transition hover:bg-primary-700"
          >
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
            <Icon name="arrowRight" size="sm" :stroke-width="2" />
          </router-link>
          <router-link
            to="/models"
            class="inline-flex min-h-[44px] min-w-[180px] items-center justify-center gap-2 rounded-control border border-gray-200 bg-white px-6 py-3 text-sm font-semibold text-gray-900 shadow-sm transition hover:border-black/20 hover:text-primary-600 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-100 dark:hover:border-primary-500"
          >
            {{ t('home.exploreMarketplace') }}
            <span class="relative flex h-5 w-5 items-center justify-center overflow-hidden">
              <MotionTransition name="home-marketplace-icon" mode="out-in">
                <ProviderIcon
                  v-if="homeMarketplaceButtonBrand"
                  :key="homeMarketplaceButtonBrand"
                  :brand="homeMarketplaceButtonBrand"
                  size="18px"
                />
                <Icon
                  v-else
                  key="marketplace-fallback"
                  name="sparkles"
                  size="sm"
                  class="text-primary-500"
                />
              </MotionTransition>
            </span>
          </router-link>
        </div>
      </section>

      <!-- Provider icon marquee -->
      <section class="mx-auto mt-14 max-w-5xl" aria-hidden="true">
        <div class="home-marquee relative overflow-hidden">
          <div class="home-marquee-track flex w-max items-center gap-8">
            <span
              v-for="(brand, index) in homeMarqueeBrands"
              :key="`${brand}-${index}`"
              class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full border border-gray-200/80 bg-white text-gray-700 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-100"
            >
              <ProviderIcon :brand="brand" size="17px" />
            </span>
          </div>
        </div>
      </section>

      <section class="mx-auto mt-20 grid max-w-7xl gap-5 sm:grid-cols-2 xl:grid-cols-4">
        <article
          class="group overflow-hidden rounded-surface border border-gray-200 bg-white transition-colors duration-fast hover:border-black/20 focus-within:border-black/20 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-600"
        >
          <div
            class="relative h-44 overflow-hidden border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-950"
          >
            <div class="absolute inset-0">
              <span
                v-for="(icon, index) in homeProviderCloudIcons"
                :key="`${icon.brand}-${index}`"
                class="absolute flex h-7 w-7 items-center justify-center rounded-full border border-gray-100 bg-white/95 text-gray-700 shadow-[0_5px_16px_rgba(0,0,0,0.13)] ring-1 ring-black/[0.02] dark:border-dark-700 dark:bg-dark-900 dark:text-dark-100 dark:ring-white/[0.04]"
                :style="{
                  left: icon.left,
                  top: icon.top,
                  opacity: icon.opacity,
                  transform: `translate(-50%, -50%) scale(${icon.scale})`,
                }"
              >
                <ProviderIcon :brand="icon.brand" size="14px" />
              </span>
            </div>
            <span
              class="pointer-events-none absolute inset-x-0 bottom-0 h-8 bg-gradient-to-t from-white via-white/35 to-transparent dark:from-dark-950 dark:via-dark-950/35"
            ></span>
          </div>
          <div class="p-5">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.features.unifiedGateway') }}
            </h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ t('home.features.unifiedGatewayDesc') }}
            </p>
            <router-link
              to="/models"
              class="mt-4 inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-300"
            >
              {{ t('home.features.browseAll') }}
              <Icon name="arrowRight" size="xs" />
            </router-link>
          </div>
        </article>

        <article
          class="group overflow-hidden rounded-surface border border-gray-200 bg-white transition-colors duration-fast hover:border-black/20 focus-within:border-black/20 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-600"
        >
          <div
            class="relative flex h-44 items-center justify-center overflow-hidden border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-950"
          >
            <div class="relative h-full w-full">
              <div
                class="absolute left-1/2 top-7 z-10 max-w-[82%] -translate-x-1/2 truncate rounded-control bg-gray-100 px-3.5 py-1.5 text-xs font-medium text-gray-800 shadow-sm dark:bg-dark-900 dark:text-dark-100"
              >
                {{ homeRouteLabel }}
              </div>
              <svg
                class="absolute left-1/2 top-12 h-24 w-[220px] -translate-x-1/2 text-gray-300 dark:text-dark-700"
                viewBox="0 0 220 110"
                fill="none"
                aria-hidden="true"
              >
                <path
                  d="M110 0V30"
                  stroke="currentColor"
                  stroke-width="1.35"
                  stroke-linecap="round"
                />
                <path
                  d="M110 30C110 60 28 52 28 84M110 30C110 55 110 64 110 84M110 30C110 60 192 52 192 84"
                  stroke="currentColor"
                  stroke-width="1.35"
                  stroke-linecap="round"
                />
              </svg>
              <div
                class="absolute bottom-6 left-1/2 flex w-[190px] -translate-x-1/2 justify-between"
              >
                <span
                  v-for="brand in homeRouteProviderBrands"
                  :key="brand"
                  class="flex h-9 w-9 items-center justify-center rounded-control border border-gray-100 bg-white text-gray-700 shadow-[0_5px_16px_rgba(0,0,0,0.13)] dark:border-dark-700 dark:bg-dark-900 dark:text-dark-100"
                >
                  <ProviderIcon :brand="brand" size="17px" />
                </span>
              </div>
            </div>
          </div>
          <div class="p-5">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.features.multiProvider') }}
            </h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ t('home.features.multiProviderDesc') }}
            </p>
            <router-link
              :to="isAuthenticated ? dashboardPath : '/login'"
              class="mt-4 inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-300"
            >
              {{ t('home.features.learnMore') }}
              <Icon name="arrowRight" size="xs" />
            </router-link>
          </div>
        </article>

        <article
          class="group overflow-hidden rounded-surface border border-gray-200 bg-white transition-colors duration-fast hover:border-black/20 focus-within:border-black/20 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-600"
        >
          <div
            class="flex h-44 items-center justify-center border-b border-gray-200 bg-gray-50 p-6 dark:border-dark-800 dark:bg-dark-950"
          >
            <div
              class="w-full max-w-[200px] rounded-surface border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900"
            >
              <div
                class="mb-4 flex items-center justify-between text-xs text-gray-500 dark:text-dark-400"
              >
                <span>{{ t('home.features.usageChart') }}</span>
                <Icon name="chart" size="sm" />
              </div>
              <div class="space-y-3">
                <div class="h-2 w-11/12 rounded-full bg-sky-300"></div>
                <div class="h-2 w-2/3 rounded-full bg-amber-300"></div>
                <div class="h-2 w-5/6 rounded-full bg-emerald-300"></div>
                <div class="h-2 w-1/2 rounded-full bg-violet-300"></div>
              </div>
            </div>
          </div>
          <div class="p-5">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.features.balanceQuota') }}
            </h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ t('home.features.balanceQuotaDesc') }}
            </p>
            <router-link
              :to="isAuthenticated ? dashboardPath : '/login'"
              class="mt-4 inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-300"
            >
              {{ t('home.features.viewUsage') }}
              <Icon name="arrowRight" size="xs" />
            </router-link>
          </div>
        </article>

        <article
          class="group overflow-hidden rounded-surface border border-gray-200 bg-white transition-colors duration-fast hover:border-black/20 focus-within:border-black/20 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-600"
        >
          <div
            class="flex h-44 items-center justify-center border-b border-gray-200 bg-gray-50 dark:border-dark-800 dark:bg-dark-950"
          >
            <div
              class="relative flex h-24 w-24 items-center justify-center rounded-full border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
            >
              <Icon name="shield" size="xl" class="text-gray-400 dark:text-dark-300" />
              <span
                class="absolute -right-1 -top-1 flex h-9 w-9 items-center justify-center rounded-full bg-emerald-100 text-emerald-600 dark:bg-emerald-500/15 dark:text-emerald-300"
              >
                <Icon name="check" size="md" :stroke-width="2" :animate-on-hover="false" />
              </span>
            </div>
          </div>
          <div class="p-5">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.features.dataPolicies') }}
            </h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
              {{ t('home.features.dataPoliciesDesc') }}
            </p>
            <a
              v-if="docUrl"
              :href="docUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="mt-4 inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-300"
            >
              {{ t('home.docs') }}
              <Icon name="externalLink" size="xs" />
            </a>
          </div>
        </article>
      </section>

      <section class="mx-auto mt-20 max-w-7xl">
        <div class="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <router-link
              to="/models"
              class="inline-flex items-center gap-2 text-2xl font-bold text-gray-950 hover:text-primary-600 dark:text-white dark:hover:text-primary-300"
            >
              {{ t('home.providers.title') }}
              <Icon name="chevronRight" size="md" :animate-on-hover="false" />
            </router-link>
            <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
              {{ formatMarketplaceStat(totalModelCount) }} {{ t('marketplace.modelsStat') }}
              ·
              {{ formatMarketplaceStat(supportedProviders.length) }}
              {{ t('home.stats.providerTypes') }}
            </p>
          </div>
          <router-link
            to="/models"
            class="text-sm font-medium text-gray-500 transition hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-300"
          >
            {{ t('home.viewAll') }}
            <Icon name="arrowRight" size="xs" class="inline-block" />
          </router-link>
        </div>

        <div class="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          <div
            v-if="homeMarketplaceLoading"
            class="rounded-surface border border-gray-200 bg-white px-5 py-4 text-center text-sm text-gray-500 dark:border-dark-800 dark:bg-dark-900 dark:text-dark-400 sm:col-span-2 lg:col-span-3"
          >
            {{ t('common.loading') }}
          </div>

          <div
            v-else-if="supportedProviders.length === 0"
            class="rounded-surface border border-gray-200 bg-white px-5 py-4 text-center text-sm text-gray-500 dark:border-dark-800 dark:bg-dark-900 dark:text-dark-400 sm:col-span-2 lg:col-span-3"
          >
            {{ homeMarketplaceError ? t('home.providers.unavailable') : t('home.providers.empty') }}
          </div>

          <!-- Configured featured models render as individual public marketplace cards. -->
          <template v-else-if="featuredModels.length > 0">
            <article
              v-for="featured in featuredModels"
              :key="featured.model.id"
              class="rounded-surface border border-gray-200 bg-white p-6 transition-colors duration-fast hover:border-black/20 focus-within:border-black/20 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-600"
            >
              <div class="flex items-start gap-4">
                <!-- Featured cards reuse the marketplace model icon treatment. -->
                <span
                  class="flex h-12 w-12 shrink-0 items-center justify-center rounded-surface border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-950"
                >
                  <ModelIcon :model="featured.model.id" size="28px" />
                </span>
                <div class="min-w-0 flex-1">
                  <h3 class="truncate text-lg font-semibold text-gray-950 dark:text-white">
                    {{ featured.model.display_name || featured.model.id }}
                  </h3>
                  <p class="truncate text-sm text-gray-500 dark:text-dark-400">
                    {{
                      t('home.featured.byProvider', {
                        provider: homeProviderCategory(featured.group).label,
                      })
                    }}
                  </p>
                </div>
              </div>
              <!-- The discount footer appears only when the group has a valid lower official-price ratio. -->
              <div
                v-if="featured.discountOff"
                class="mt-5 border-t border-gray-200 pt-5 dark:border-dark-800"
              >
                <p class="text-sm font-semibold tabular-nums text-gray-950 dark:text-white">
                  {{ featured.discountOff }}
                </p>
              </div>
            </article>
          </template>

          <template v-else>
            <article
              v-for="provider in supportedProviders.slice(0, 6)"
              :key="provider.key"
              class="rounded-surface border border-gray-200 bg-white p-6 transition-colors duration-fast hover:border-black/20 focus-within:border-black/20 dark:border-dark-800 dark:bg-dark-900 dark:hover:border-dark-600"
            >
              <div class="flex items-start gap-4">
                <span
                  class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-950"
                  :class="providerIconWrapClass(provider)"
                >
                  <ProviderIcon :brand="provider.iconBrand" size="22px" />
                </span>
                <div class="min-w-0 flex-1">
                  <h3 class="truncate text-lg font-semibold text-gray-950 dark:text-white">
                    {{ provider.label }}
                  </h3>
                  <p class="text-sm text-gray-500 dark:text-dark-400">
                    {{ provider.groupCount }} {{ t('home.providers.groups') }}
                  </p>
                </div>
              </div>
              <div class="mt-5 border-t border-gray-200 pt-5 dark:border-dark-800">
                <div class="flex items-end justify-between gap-4">
                  <div>
                    <p class="text-sm text-gray-500 dark:text-dark-400">{{
                      t('home.providers.modelCount')
                    }}</p>
                    <p class="mt-1 text-lg font-semibold text-gray-950 dark:text-white">
                      {{ provider.modelCount }}
                    </p>
                  </div>
                  <p
                    v-if="provider.officialPriceRatio"
                    class="max-w-[180px] text-right text-sm font-semibold text-emerald-600 dark:text-emerald-300"
                  >
                    {{ formatOfficialPriceRatio(provider.officialPriceRatio) }}
                  </p>
                  <p v-else class="text-sm font-medium text-primary-600 dark:text-primary-300">
                    {{ t('home.providers.supported') }}
                  </p>
                </div>
              </div>
            </article>
          </template>
        </div>
      </section>

      <section class="mx-auto mt-16 max-w-7xl px-2 sm:px-0">
        <div class="grid gap-8 md:grid-cols-3">
          <article v-for="step in homeSteps" :key="step.key" class="flex min-h-[190px] flex-col">
            <div class="flex items-center gap-3">
              <span
                class="flex h-8 w-8 items-center justify-center rounded-full bg-primary-50 text-base font-semibold text-primary-600 dark:bg-primary-500/10 dark:text-primary-300"
              >
                {{ step.index }}
              </span>
              <h2 class="text-lg font-semibold tracking-tight text-gray-950 dark:text-white">{{
                step.title
              }}</h2>
            </div>
            <p class="mt-4 max-w-sm text-sm leading-6 text-gray-600 dark:text-dark-300">{{
              step.description
            }}</p>

            <div v-if="step.key === 'signup'" class="mt-8">
              <div class="flex items-center gap-3 text-primary-500">
                <Icon name="user" size="md" :stroke-width="1.8" />
                <div class="space-y-1.5">
                  <div class="h-1.5 w-7 rounded-full bg-primary-100 dark:bg-primary-400/20"></div>
                  <div class="h-1.5 w-20 rounded-full bg-primary-100 dark:bg-primary-400/20"></div>
                </div>
              </div>
              <div class="mt-4 grid max-w-[156px] grid-cols-3 gap-3">
                <span
                  class="flex h-10 w-10 items-center justify-center rounded-control bg-white/90 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:ring-dark-800"
                >
                  <ProviderIcon brand="Google" size="20px" />
                </span>
                <span
                  class="flex h-10 w-10 items-center justify-center rounded-control bg-white/90 text-gray-800 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:text-gray-100 dark:ring-dark-800"
                >
                  <GitHubMark class="h-5 w-5" />
                </span>
                <span
                  class="flex h-10 w-10 items-center justify-center rounded-control bg-white/90 text-primary-500 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:ring-dark-800"
                >
                  <Icon name="mail" size="md" :stroke-width="1.8" />
                </span>
              </div>
            </div>

            <div v-else-if="step.key === 'browse'" class="mt-auto max-w-[270px] pt-6">
              <div class="flex items-center gap-3 text-primary-500">
                <Icon name="grid" size="md" :stroke-width="1.8" />
                <div class="grid flex-1 grid-cols-4 gap-2">
                  <div class="h-1 rounded-full bg-primary-100 dark:bg-primary-400/20"></div>
                  <div class="h-1 rounded-full bg-primary-100 dark:bg-primary-400/20"></div>
                  <div class="h-1 rounded-full bg-primary-100 dark:bg-primary-400/20"></div>
                  <div class="h-1 rounded-full bg-primary-100 dark:bg-primary-400/20"></div>
                </div>
              </div>
              <div class="mt-4 space-y-2">
                <div
                  class="flex items-center gap-2 rounded-control bg-white/90 px-3 py-2 text-gray-700 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:text-dark-200 dark:ring-dark-800"
                >
                  <span class="w-14 text-xs font-medium">Claude</span>
                  <span
                    class="h-2 flex-1 rounded-full bg-primary-100 dark:bg-primary-400/20"
                  ></span>
                  <span class="h-2 w-12 rounded-full bg-primary-100 dark:bg-primary-400/20"></span>
                </div>
                <div
                  class="flex items-center gap-2 rounded-control bg-white/90 px-3 py-2 text-gray-700 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:text-dark-200 dark:ring-dark-800"
                >
                  <span class="w-14 text-xs font-medium">GPT</span>
                  <span
                    class="h-2 flex-1 rounded-full bg-primary-100 dark:bg-primary-400/20"
                  ></span>
                  <span class="h-2 w-12 rounded-full bg-primary-100 dark:bg-primary-400/20"></span>
                </div>
              </div>
            </div>

            <div v-else class="mt-8 max-w-[270px]">
              <div class="flex items-center gap-3 text-primary-500">
                <Icon name="key" size="md" :stroke-width="1.8" />
                <div
                  class="flex-1 rounded-control bg-white/90 px-3 py-2 font-mono text-xs text-gray-600 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:text-dark-300 dark:ring-dark-800"
                >
                  TOKENFLUX_API_KEY
                </div>
              </div>
              <div
                class="mt-3 rounded-control bg-white/90 px-3 py-2 font-mono text-sm tracking-[0.2em] text-gray-950 shadow-sm ring-1 ring-gray-100 dark:bg-dark-950 dark:text-white dark:ring-dark-800"
              >
                ••••••••••••••••
              </div>
            </div>
          </article>
        </div>
      </section>

      <!-- CTA -->
      <section class="mx-auto mt-24 max-w-3xl text-center">
        <h2 class="text-3xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-4xl">
          {{ t('home.cta.title') }}
        </h2>
        <p class="mx-auto mt-4 max-w-xl text-base leading-7 text-gray-600 dark:text-dark-300">
          {{ t('home.cta.description') }}
        </p>
        <div class="mt-8">
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-[44px] min-w-[180px] items-center justify-center gap-2 rounded-control bg-primary-600 px-8 py-3 text-sm font-semibold text-white transition hover:bg-primary-700"
          >
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.cta.button') }}
            <Icon name="arrowRight" size="sm" :stroke-width="2" />
          </router-link>
        </div>
      </section>
    </main>

    <footer
      class="relative z-10 border-t border-gray-200 bg-white/90 px-6 py-12 backdrop-blur dark:border-dark-800 dark:bg-dark-950/90"
    >
      <div class="mx-auto max-w-7xl">
        <div
          class="grid grid-cols-2 gap-x-8 gap-y-10 sm:grid-cols-3 lg:flex lg:justify-between lg:gap-8"
        >
          <!-- Brand -->
          <div class="col-span-2 sm:col-span-3 lg:col-auto lg:max-w-[240px] lg:shrink-0">
            <div class="flex items-center gap-2.5">
              <span class="h-8 w-8 shrink-0 overflow-hidden rounded-control shadow-sm">
                <img
                  :src="siteLogo || '/logo.svg'"
                  alt="Logo"
                  class="h-full w-full object-contain"
                />
              </span>
              <span class="text-sm font-semibold text-gray-950 dark:text-white">{{
                siteName
              }}</span>
            </div>
            <p class="mt-4 text-sm text-gray-500 dark:text-dark-400">
              &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
            </p>
            <p
              v-for="(line, index) in footerTextLines"
              :key="index"
              class="mt-1 text-xs text-gray-400 dark:text-dark-500"
            >
              {{ line }}
            </p>
          </div>

          <!-- Link columns -->
          <div v-for="column in footerColumns" :key="column.title" class="lg:min-w-[140px]">
            <h3 class="text-sm font-semibold text-gray-950 dark:text-white">{{ column.title }}</h3>
            <ul class="mt-4 space-y-2.5">
              <li v-for="link in column.links" :key="link.label">
                <router-link
                  v-if="link.url.startsWith('/')"
                  :to="link.url"
                  class="text-sm text-gray-500 transition hover:text-gray-950 dark:text-dark-400 dark:hover:text-white"
                >
                  {{ link.label }}
                </router-link>
                <a
                  v-else
                  :href="link.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-sm text-gray-500 transition hover:text-gray-950 dark:text-dark-400 dark:hover:text-white"
                >
                  {{ link.label }}
                </a>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { useLocaleRefresh } from '@/composables/useLocaleRefresh'
import { vContentReveal } from '@/directives/contentReveal'
import { useRoute as useMotionRoute } from 'vue-router'
const motionRoute = useMotionRoute()

import MotionTransition from '@/components/common/MotionTransition.vue'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import GitHubMark from '@/components/auth/GitHubMark.vue'
import GoogleOneTap from '@/components/auth/GoogleOneTap.vue'
import AppHeader from '@/components/layout/AppHeader.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { getMarketplaceModels } from '@/api/marketplace'
import type { MarketplaceGroup, MarketplaceModel } from '@/api/marketplace.types'
import { sanitizeUrl } from '@/utils/url'
import { hasAcceptedLoginAgreement } from '@/utils/loginAgreement'
import { isGoogleOneTapEligible, isGoogleOneTapOriginSupported } from '@/utils/googleIdentity'
import {
  providerBrandDisplayName,
  providerBrandFilterKey,
  resolveProviderBrand,
  resolveProviderBrandKey,
} from '@/utils/providerBrand'

type HomeStepIcon = 'userPlus' | 'grid' | 'key'

interface HomeProviderCategory {
  key: string
  label: string
  iconBrand: string
}

interface HomeProviderSummary extends HomeProviderCategory {
  modelCount: number
  groupCount: number
  officialPriceRatio?: number
  sortOrder: number
  firstIndex: number
}

interface HomeStep {
  key: string
  index: number
  title: string
  description: string
  icon: HomeStepIcon
}

interface HomeProviderCloudIcon {
  brand: string
  left: string
  top: string
  opacity: number
  scale: number
}

// HomeFeaturedModel combines one public model with its owning group and optional discount.
interface HomeFeaturedModel {
  model: MarketplaceModel
  group: MarketplaceGroup
  // discountOff is the translated group discount or null when no valid discount exists.
  discountOff: string | null
}

const { t, locale } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Public site settings come from the injected or cached public configuration.
const siteName = computed(() => appStore.siteName || 'TokenRouter')
const siteLogo = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
    allowRelative: true,
    allowDataUrl: true,
  }),
)
const docUrl = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''),
)
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const numberLocale = computed(() => locale.value)
const homeHeroTitle = computed(() =>
  appStore.cachedPublicSettings?.site_text_overrides?.includes('site_title')
    ? appStore.cachedPublicSettings.site_title || ''
    : t('home.heroTitle'),
)
const homeHeroSubtitle = computed(() =>
  appStore.cachedPublicSettings?.site_text_overrides?.includes('site_subtitle')
    ? appStore.cachedPublicSettings.site_subtitle || ''
    : t('home.heroDescription'),
)

// Custom home content supports an HTTP(S) iframe or administrator-provided HTML.
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))

const googleOneTapClientID = computed(
  () => appStore.cachedPublicSettings?.google_oauth_client_id || '',
)
const googleOneTapEligible = computed(() => {
  const settings = appStore.cachedPublicSettings
  if (!settings) return false
  const agreementEnabled = settings.login_agreement_enabled === true
  return isGoogleOneTapEligible({
    publicSettingsLoaded: appStore.publicSettingsLoaded,
    isAuthenticated: isAuthenticated.value,
    oneTapEnabled: settings.google_one_tap_enabled === true,
    clientID: googleOneTapClientID.value,
    backendModeEnabled: settings.backend_mode_enabled,
    tencentCaptchaEnabled: settings.tencent_captcha_enabled === true,
    aliyunCaptchaEnabled: settings.aliyun_captcha_enabled === true,
    loginAgreementEnabled: agreementEnabled,
    loginAgreementAccepted:
      !agreementEnabled || hasAcceptedLoginAgreement(settings.login_agreement_revision || ''),
    originSupported: isGoogleOneTapOriginSupported(),
  })
})

const currentYear = computed(() => new Date().getFullYear())

// Footer content combines configured link groups with a built-in quick-links fallback.
const footerTextLines = computed<string[]>(() => {
  const raw = appStore.cachedPublicSettings?.footer_text || ''
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
})

const footerColumns = computed(() => {
  const configured = (appStore.cachedPublicSettings?.footer_links || [])
    .filter((group) => group.title && Array.isArray(group.links) && group.links.length > 0)
    .map((group) => ({
      title: group.title,
      links: group.links.filter((link) => link.label && link.url),
    }))
    .filter((group) => group.links.length > 0)

  // Configured footer groups replace the built-in quick-links column.
  if (configured.length > 0) {
    return configured
  }

  const quickLinks: Array<{ label: string; url: string }> = [
    { label: t('home.nav.models'), url: '/models' },
    { label: t('keyUsage.title'), url: '/key-usage' },
  ]
  if (docUrl.value) {
    quickLinks.push({ label: t('home.docs'), url: docUrl.value })
  }

  return [{ title: t('home.footer.quickLinks'), links: quickLinks }]
})

// Duplicate marquee brands allow the decorative track to loop continuously.
const homeMarqueeBrands = computed(() => {
  const brands = homeProviderVisuals.value.slice(0, 20)
  return [...brands, ...brands]
})

const marketplaceGroups = ref<MarketplaceGroup[]>([])
const homeMarketplaceLoading = ref(true)
const homeMarketplaceError = ref(false)
const homeMarketplaceButtonIconIndex = ref(0)
let homeMarketplaceButtonIconTimer: number | null = null

const providerVisualFallbacks = [
  'Google',
  'Meta',
  'Gemini',
  'OpenAI',
  'Qwen',
  'DeepSeek',
  'Mistral',
  'Moonshot',
  'Claude',
  'xAI',
  'Antigravity',
  'Zhipu',
  'Cohere',
  'Perplexity',
  'Minimax',
  'Doubao',
  'Baidu',
  'Tencent',
  'Cloudflare',
  'OpenRouter',
]

const providerCloudLayout = [
  { left: '7%', top: '13%', opacity: 0.72, scale: 0.94 },
  { left: '25%', top: '13%', opacity: 0.8, scale: 0.94 },
  { left: '43%', top: '13%', opacity: 0.9, scale: 1 },
  { left: '62%', top: '13%', opacity: 0.8, scale: 0.94 },
  { left: '81%', top: '13%', opacity: 0.72, scale: 0.94 },
  { left: '17%', top: '35%', opacity: 0.86, scale: 0.98 },
  { left: '35%', top: '35%', opacity: 0.92, scale: 1 },
  { left: '53%', top: '35%', opacity: 0.96, scale: 1.04 },
  { left: '72%', top: '35%', opacity: 0.9, scale: 0.98 },
  { left: '91%', top: '35%', opacity: 0.76, scale: 0.94 },
  { left: '7%', top: '57%', opacity: 0.82, scale: 0.94 },
  { left: '25%', top: '57%', opacity: 0.9, scale: 0.98 },
  { left: '43%', top: '57%', opacity: 1, scale: 1.06 },
  { left: '62%', top: '57%', opacity: 0.9, scale: 0.98 },
  { left: '81%', top: '57%', opacity: 0.82, scale: 0.94 },
  { left: '17%', top: '79%', opacity: 0.72, scale: 0.94 },
  { left: '35%', top: '79%', opacity: 0.78, scale: 0.96 },
  { left: '53%', top: '79%', opacity: 0.82, scale: 0.98 },
  { left: '72%', top: '79%', opacity: 0.78, scale: 0.96 },
  { left: '91%', top: '79%', opacity: 0.66, scale: 0.92 },
  { left: '7%', top: '98%', opacity: 0.6, scale: 0.9 },
  { left: '25%', top: '98%', opacity: 0.66, scale: 0.92 },
  { left: '43%', top: '98%', opacity: 0.7, scale: 0.94 },
  { left: '62%', top: '98%', opacity: 0.66, scale: 0.92 },
  { left: '81%', top: '98%', opacity: 0.6, scale: 0.9 },
  { left: '99%', top: '98%', opacity: 0.48, scale: 0.86 },
] as const

const totalModelCount = computed(() =>
  marketplaceGroups.value.reduce((total, group) => total + group.models.length, 0),
)

// homeFeaturedModelIds reads administrator-selected public model identifiers from public settings.
const homeFeaturedModelIds = computed<string[]>(() => {
  const configured = appStore.cachedPublicSettings?.home_featured_models
  return Array.isArray(configured) ? configured : []
})

// featuredModels resolves configured IDs in order and skips models absent from public groups.
const featuredModels = computed<HomeFeaturedModel[]>(() => {
  const resolved: HomeFeaturedModel[] = []
  for (const modelId of homeFeaturedModelIds.value) {
    for (const group of marketplaceGroups.value) {
      const model = group.models.find((item) => item.id === modelId)
      if (model) {
        resolved.push({
          model,
          group,
          discountOff: formatFeaturedDiscountOff(group.official_price_ratio),
        })
        break
      }
    }
  }
  return resolved
})

const supportedProviders = computed<HomeProviderSummary[]>(() => {
  const summaries = new Map<string, HomeProviderSummary>()
  const sortedGroups = [...marketplaceGroups.value].sort((left, right) => {
    const sortDiff = (left.sort_order ?? 0) - (right.sort_order ?? 0)
    if (sortDiff !== 0) {
      return sortDiff
    }
    return left.id - right.id
  })

  sortedGroups.forEach((group, index) => {
    const modelCount = group.models.length
    if (modelCount === 0) {
      return
    }

    const category = homeProviderCategory(group)
    const existing = summaries.get(category.key)
    const ratio = validOfficialPriceRatio(group.official_price_ratio)
    if (!existing) {
      summaries.set(category.key, {
        ...category,
        modelCount,
        groupCount: 1,
        officialPriceRatio: ratio ?? undefined,
        sortOrder: group.sort_order ?? 0,
        firstIndex: index,
      })
      return
    }

    existing.modelCount += modelCount
    existing.groupCount += 1
    existing.sortOrder = Math.min(existing.sortOrder, group.sort_order ?? 0)
    existing.firstIndex = Math.min(existing.firstIndex, index)
    if (ratio && (!existing.officialPriceRatio || ratio < existing.officialPriceRatio)) {
      existing.officialPriceRatio = ratio
    }
  })

  return [...summaries.values()].sort((left, right) => {
    const priorityDiff = homeProviderPriority(left.key) - homeProviderPriority(right.key)
    if (priorityDiff !== 0) {
      return priorityDiff
    }
    const sortDiff = left.sortOrder - right.sortOrder
    if (sortDiff !== 0) {
      return sortDiff
    }
    return left.firstIndex - right.firstIndex
  })
})

const homeProviderVisuals = computed(() => {
  const brands = supportedProviders.value.map((provider) => provider.iconBrand)
  return mergeProviderVisualBrands(brands)
})

const homeMarketplaceButtonBrands = computed(() =>
  supportedProviders.value.map((provider) => provider.iconBrand),
)

const homeMarketplaceButtonBrand = computed(() => {
  const brands = homeMarketplaceButtonBrands.value
  if (brands.length === 0) {
    return ''
  }
  return brands[homeMarketplaceButtonIconIndex.value % brands.length]
})

const homeProviderCloudIcons = computed<HomeProviderCloudIcon[]>(() => {
  const brands = homeProviderVisuals.value
  return providerCloudLayout.map((layout, index) => ({
    brand: brands[index % brands.length],
    ...layout,
  }))
})

const homeRouteProviderBrands = computed(() => homeProviderVisuals.value.slice(0, 3))

const homeRouteLabel = computed(() => {
  return 'OpenAI/GPT-5.4'
})

const homeSteps = computed<HomeStep[]>(() => [
  {
    key: 'signup',
    index: 1,
    title: t('home.steps.signup.title'),
    description: t('home.steps.signup.description'),
    icon: 'userPlus',
  },
  {
    key: 'browse',
    index: 2,
    title: t('home.steps.browse.title'),
    description: t('home.steps.browse.description'),
    icon: 'grid',
  },
  {
    key: 'api-key',
    index: 3,
    title: t('home.steps.apiKey.title'),
    description: t('home.steps.apiKey.description'),
    icon: 'key',
  },
])

function homeProviderCategory(group: MarketplaceGroup): HomeProviderCategory {
  const brandSource = group.display_brand?.trim() || group.name.trim()
  const brandKey = resolveProviderBrandKey(brandSource)
  if (brandKey && brandKey !== 'unknown') {
    return homeProviderCategoryFromBrand(brandKey, brandSource)
  }

  const fallbackLabel = brandSource
  return {
    key: providerBrandFilterKey(fallbackLabel),
    label: fallbackLabel,
    iconBrand: fallbackLabel,
  }
}

function homeProviderCategoryFromBrand(brandKey: string, source: string): HomeProviderCategory {
  switch (brandKey) {
    case 'anthropic':
      return { key: 'claude', label: t('home.providers.claude'), iconBrand: 'Claude' }
    case 'openai':
      return { key: 'gpt', label: t('home.providers.gpt'), iconBrand: 'OpenAI' }
    case 'google':
      return { key: 'gemini', label: t('home.providers.gemini'), iconBrand: 'Gemini' }
    default: {
      const label = providerBrandDisplayName(source)
      return { key: brandKey || providerBrandFilterKey(source), label, iconBrand: label }
    }
  }
}

function homeProviderPriority(key: string): number {
  const priorities = ['claude', 'gpt', 'deepseek', 'gemini', 'antigravity']
  const index = priorities.indexOf(key)
  return index === -1 ? priorities.length : index
}

function validOfficialPriceRatio(value?: number): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : null
}

function formatOfficialPriceRatio(ratio: number): string {
  const discount = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(ratio * 10)

  return t('marketplace.officialPriceDiscount', { discount })
}

// formatFeaturedDiscountOff returns group savings against the official price.
// Invalid or undiscounted ratios return null so the card omits its footer.
function formatFeaturedDiscountOff(ratio?: number): string | null {
  const valid = validOfficialPriceRatio(ratio)
  if (valid === null || valid >= 1) {
    return null
  }
  const percent = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 1,
  }).format((1 - valid) * 100)
  return t('home.featured.discountOff', { percent })
}

function formatMarketplaceStat(value: number): string {
  if (homeMarketplaceLoading.value) {
    return '...'
  }
  return new Intl.NumberFormat(numberLocale.value).format(value)
}

watch(
  homeMarketplaceButtonBrands,
  (brands) => {
    if (brands.length === 0) {
      homeMarketplaceButtonIconIndex.value = 0
      return
    }
    homeMarketplaceButtonIconIndex.value %= brands.length
  },
  { immediate: true },
)

function mergeProviderVisualBrands(brands: string[]): string[] {
  const seen = new Set<string>()
  const merged: string[] = []

  ;[...brands, ...providerVisualFallbacks].forEach((brand) => {
    const normalizedBrand = brand.trim()
    if (!normalizedBrand || seen.has(normalizedBrand)) {
      return
    }
    seen.add(normalizedBrand)
    merged.push(normalizedBrand)
  })

  return merged
}

function providerIconWrapClass(provider: Pick<HomeProviderSummary, 'key' | 'iconBrand'>): string {
  if (provider.key === 'antigravity') {
    return 'bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-500/15 dark:text-rose-200 dark:ring-rose-400/30'
  }
  return resolveProviderBrand(provider.iconBrand).iconWrapClass
}

async function fetchHomeMarketplace() {
  homeMarketplaceLoading.value = true
  homeMarketplaceError.value = false

  try {
    marketplaceGroups.value = await getMarketplaceModels()
  } catch (error) {
    console.error('Failed to load home marketplace models:', error)
    marketplaceGroups.value = []
    homeMarketplaceError.value = true
  } finally {
    homeMarketplaceLoading.value = false
  }
}

onMounted(async () => {
  authStore.checkAuth()
  homeMarketplaceButtonIconTimer = window.setInterval(() => {
    if (homeMarketplaceButtonBrands.value.length <= 1) {
      return
    }
    homeMarketplaceButtonIconIndex.value += 1
  }, 1800)

  if (!appStore.publicSettingsLoaded) {
    try {
      await appStore.fetchPublicSettings()
    } catch (error) {
      console.error('Failed to load public settings:', error)
    }
  }

  if (!homeContent.value) {
    await fetchHomeMarketplace()
  }
})

onUnmounted(() => {
  if (homeMarketplaceButtonIconTimer) {
    window.clearInterval(homeMarketplaceButtonIconTimer)
    homeMarketplaceButtonIconTimer = null
  }
})
useLocaleRefresh(fetchHomeMarketplace)
</script>

<style scoped>
.home-marketplace-icon-enter-active,
.home-marketplace-icon-leave-active {
  transition:
    opacity var(--motion-layout) var(--motion-ease),
    transform var(--motion-layout) var(--motion-ease);
}

.home-marketplace-icon-enter-from {
  opacity: 0;
  transform: translateY(-70%);
}

.home-marketplace-icon-leave-to {
  opacity: 0;
  transform: translateY(70%);
}

/* The marquee fades at both edges while duplicated provider icons scroll. */
.home-marquee {
  -webkit-mask-image: linear-gradient(to right, transparent, black 12%, black 88%, transparent);
  mask-image: linear-gradient(to right, transparent, black 12%, black 88%, transparent);
}

.home-marquee-track {
  animation: home-marquee-scroll 48s linear infinite;
}

.home-marquee:hover .home-marquee-track {
  animation-play-state: paused;
}

@keyframes home-marquee-scroll {
  0% {
    transform: translateX(0);
  }
  100% {
    transform: translateX(-50%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .home-marquee-track {
    animation: none;
  }
}
</style>
