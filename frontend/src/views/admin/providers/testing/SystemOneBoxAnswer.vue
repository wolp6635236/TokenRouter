<template>
  <div class="space-y-3" data-testid="decision-box-result">
    <figure v-if="image && !imageFailed" class="space-y-2">
      <div
        class="relative w-full overflow-hidden rounded-control"
        data-testid="decision-box-image-frame"
      >
        <img
          :src="image.src"
          :alt="image.name"
          class="block h-auto w-full"
          @load="imageLoaded = true"
          @error="imageFailed = true"
        />
        <div
          v-if="imageLoaded"
          class="pointer-events-none absolute border-2 border-primary-500"
          :style="boxStyle"
          aria-hidden="true"
          data-testid="decision-box-overlay"
        />
      </div>
      <figcaption class="break-all text-xs text-gray-500 dark:text-dark-400">{{
        image.name
      }}</figcaption>
    </figure>
    <p v-else class="text-xs text-gray-500 dark:text-dark-400">{{
      t('admin.providers.decisionTest.imageUnavailable')
    }}</p>
    <dl class="grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
      <div v-for="edge in edges" :key="edge">
        <dt class="text-gray-500 dark:text-dark-400">{{
          t(`admin.providers.decisionTest.${edge}`)
        }}</dt>
        <dd class="mt-1 font-mono tabular-nums text-gray-900 dark:text-dark-50">{{ box[edge] }}</dd>
      </div>
    </dl>
    <p class="text-xs text-gray-500 dark:text-dark-400">{{
      t('admin.providers.decisionTest.normalizedCoordinates')
    }}</p>
    <dl class="text-sm">
      <dt class="text-xs text-gray-500 dark:text-dark-400">{{
        t('admin.providers.decisionTest.confidence')
      }}</dt>
      <dd class="mt-1 font-mono tabular-nums text-gray-900 dark:text-dark-50">
        {{
          confidence == null
            ? t('admin.providers.decisionTest.confidenceUnavailable')
            : `${(confidence * 100).toFixed(2)}%`
        }}
      </dd>
    </dl>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SystemOneBox, SystemOneTestPreview } from './systemOneTest'

/**
 * SystemOneBoxAnswer.vue draws normalized edges over the submitted target image and displays
 * their numeric values. The image determines its wrapper's aspect ratio without object-fit
 * padding, so percentage coordinates use the actual rendered image area at every viewport size.
 *
 * The parent supplies a submission snapshot rather than the editable upload list. Missing or
 * failed images retain coordinates and confidence without guessing another target. Overlay
 * rendering waits for image load, and a changed source resets that state before its next load.
 * Null confidence remains unavailable rather than becoming a zero-confidence observation.
 */

/**
 * props associates a validated upstream box with the browser's submitted image identity.
 */
const props = defineProps<{
  box: SystemOneBox // normalized upstream rectangle
  confidence?: number | null // optional or withheld confidence
  image?: SystemOneTestPreview['images'][string] // explicitly selected target image
}>()
/**
 * Image state prevents stale geometry during source loading; edge order follows the upstream
 * coordinate names, and translation supplies readable labels beside exact numeric values.
 */
const { t } = useI18n()
const imageLoaded = ref(false)
const imageFailed = ref(false)
const edges = ['top', 'bottom', 'left', 'right'] as const
watch(
  () => props.image?.src,
  () => {
    // A new source must establish its own loaded dimensions before the old overlay becomes
    // visible again; failed images keep their coordinate-only presentation instead.
    imageLoaded.value = false
    imageFailed.value = false
  },
)
/**
 * boxStyle converts normalized edges to percentages of the image-sized wrapper.
 * Using the difference between opposing edges preserves rectangle size through responsive
 * scaling without measuring a letterboxed container or rounding the underlying coordinates.
 */
const boxStyle = computed(() => ({
  top: `${props.box.top * 100}%`,
  left: `${props.box.left * 100}%`,
  width: `${(props.box.right - props.box.left) * 100}%`,
  height: `${(props.box.bottom - props.box.top) * 100}%`,
}))
</script>
