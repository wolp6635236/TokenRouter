<template>
  <div class="space-y-4" data-testid="systemone-test-form" @focusin="rememberField">
    <div class="space-y-2">
      <div class="flex items-center justify-between gap-2">
        <label :for="stateFieldId" class="input-label mb-0">{{
          t('admin.providers.decisionTest.state')
        }}</label>
        <div
          v-segmented
          class="segmented"
          role="radiogroup"
          :aria-label="t('admin.providers.decisionTest.stateFormat')"
        >
          <button
            v-for="format in stateFormats"
            :key="format.value"
            type="button"
            role="radio"
            :aria-checked="stateFormat === format.value"
            :disabled="disabled"
            :class="[
              'segmented-item px-2.5 py-1 text-xs',
              { 'segmented-item-active': stateFormat === format.value },
            ]"
            @click="stateFormat = format.value"
          >
            {{ format.label }}
          </button>
        </div>
      </div>
      <TextArea
        :id="stateFieldId"
        v-model="stateText"
        :hint="stateFormat === 'json' ? t('admin.providers.decisionTest.jsonHint') : undefined"
        :disabled="disabled"
        rows="4"
        data-testid="systemone-state"
      />
    </div>
    <section class="space-y-2" :aria-label="t('admin.providers.decisionTest.images')">
      <div class="flex items-center justify-between gap-2">
        <span class="input-label">{{ t('admin.providers.decisionTest.images') }}</span>
        <label
          class="btn btn-secondary btn-sm cursor-pointer"
          :class="{ 'opacity-50': disabled || uploading }"
        >
          <input
            type="file"
            multiple
            accept="image/png,image/jpeg"
            class="sr-only"
            :disabled="disabled || uploading"
            data-testid="decision-image-upload"
            @change="uploadImages"
          />
          {{
            t(
              uploading
                ? 'admin.providers.decisionTest.readingImages'
                : 'admin.providers.decisionTest.addImages',
            )
          }}
        </label>
      </div>
      <p class="input-hint">{{ t('admin.providers.decisionTest.imageHint') }}</p>
      <TransitionGroup name="motion-list" tag="div" class="relative space-y-2">
        <div
          v-for="image in images"
          :key="image.reference"
          class="space-y-3 rounded-control border border-gray-200 p-3 dark:border-dark-600"
          data-testid="decision-image"
        >
          <!-- Metadata and reference actions use separate rows so the narrow settings column
               does not force buttons to wrap in the space beside the thumbnail. -->
          <div class="flex items-start gap-2">
            <img
              :src="image.src"
              :alt="image.name"
              class="h-16 w-16 shrink-0 rounded-compact object-contain"
            />
            <div class="min-w-0 flex-1 space-y-1 py-1">
              <p class="truncate text-sm text-gray-900 dark:text-dark-50" :title="image.name">
                {{ image.name }}
              </p>
              <p class="text-xs text-gray-500 dark:text-dark-400">
                {{ image.width }} × {{ image.height }}
              </p>
            </div>
            <button
              type="button"
              class="btn-icon-sm shrink-0 text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-gray-400 dark:text-dark-400 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              :disabled="disabled"
              :title="t('admin.providers.decisionTest.removeImage', { name: image.name })"
              :aria-label="t('admin.providers.decisionTest.removeImage', { name: image.name })"
              data-testid="decision-image-remove"
              @click="removeImage(image.reference)"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>
          <div class="flex min-w-0 items-center gap-2">
            <code
              class="min-w-0 flex-1 truncate text-xs text-gray-500 dark:text-dark-400"
              :title="image.reference"
              >{{ image.reference }}</code
            >
            <button
              type="button"
              class="btn-icon-sm shrink-0 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-40 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-dark-100"
              :disabled="disabled"
              :title="
                t(
                  copiedReference === image.reference
                    ? 'admin.providers.decisionTest.referenceCopied'
                    : 'admin.providers.decisionTest.copyReference',
                )
              "
              :aria-label="
                t(
                  copiedReference === image.reference
                    ? 'admin.providers.decisionTest.referenceCopied'
                    : 'admin.providers.decisionTest.copyReference',
                )
              "
              data-testid="decision-image-copy"
              @click="copyReference(image.reference)"
            >
              <Icon
                :name="copiedReference === image.reference ? 'check' : 'copy'"
                size="sm"
                :animate-on-hover="false"
              />
            </button>
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-sm w-full"
            :disabled="disabled || (stateFormat === 'json' && !focusedField)"
            data-testid="decision-image-insert"
            @click="insertReference(image.reference)"
          >
            {{ t('admin.providers.decisionTest.insertReference') }}
          </button>
        </div>
      </TransitionGroup>
      <p v-if="images.length" class="input-hint">{{
        t('admin.providers.decisionTest.experimentalImages')
      }}</p>
      <p v-if="uploadError" class="input-error-text" role="alert">{{ uploadError }}</p>
    </section>
    <RuleListEditor
      :items="questions"
      :title="t('admin.providers.decisionTest.questions')"
      :add-label="t('admin.providers.decisionTest.addQuestion')"
      :item-label="
        (index) => t('admin.providers.decisionTest.questionNumber', { number: index + 1 })
      "
      :disabled="disabled"
      :min="1"
      variant="line"
      test-id="systemone-questions"
      @add="addQuestion"
      @remove="questions.splice($event, 1)"
    >
      <template #row="{ item, index }">
        <div class="space-y-3">
          <Select
            v-model="item.type"
            :options="questionTypes"
            :disabled="disabled"
            :aria-label="t('admin.providers.decisionTest.questionType')"
            :data-testid="`decision-type-${index}`"
          />
          <TextArea
            v-model="item.instructions"
            :label="t('admin.providers.decisionTest.instructions')"
            :disabled="disabled"
            rows="2"
            :data-testid="`decision-instructions-${index}`"
          />
          <div v-if="item.type === 'box'" class="space-y-1">
            <label :for="`${stateFieldId}-box-${index}`" class="input-label">{{
              t('admin.providers.decisionTest.targetImage')
            }}</label>
            <Select
              v-model="item.imageReference"
              :id="`${stateFieldId}-box-${index}`"
              :options="imageOptions"
              :disabled="disabled"
              :aria-label="t('admin.providers.decisionTest.targetImage')"
              :placeholder="t('admin.providers.decisionTest.selectImage')"
              :data-testid="`decision-box-image-${index}`"
            />
          </div>
          <RuleListEditor
            v-if="item.type === 'choice' || item.type === 'score'"
            :items="item.options"
            :title="
              t(
                item.type === 'score'
                  ? 'admin.providers.decisionTest.levels'
                  : 'admin.providers.decisionTest.options',
              )
            "
            :hint="item.type === 'score' ? t('admin.providers.decisionTest.levelsHint') : undefined"
            :disabled="disabled"
            :min="item.type === 'score' ? 2 : 1"
            :max="item.type === 'score' ? 10 : 255"
            :reorderable="item.type === 'score'"
            :item-label="
              item.type === 'score'
                ? (level) => t('admin.providers.decisionTest.levelNumber', { number: level })
                : undefined
            "
            :test-id="`decision-options-${index}`"
            @add="item.options.push({ id: '', description: '' })"
            @remove="item.options.splice($event, 1)"
            @move="(from, to) => moveOption(item, from, to)"
          >
            <template #row="{ item: option, index: optionIndex }">
              <div class="space-y-2">
                <input
                  v-if="item.type === 'choice'"
                  v-model="option.id"
                  class="input"
                  :disabled="disabled"
                  :placeholder="t('admin.providers.decisionTest.optionId')"
                  :aria-label="t('admin.providers.decisionTest.optionId')"
                />
                <label
                  v-if="item.type === 'score'"
                  :for="`${stateFieldId}-option-${index}-${optionIndex}`"
                  class="sr-only"
                >
                  {{ t('admin.providers.decisionTest.levelNumber', { number: optionIndex }) }}
                </label>
                <TextArea
                  :id="`${stateFieldId}-option-${index}-${optionIndex}`"
                  v-model="option.description"
                  :label="
                    item.type === 'score'
                      ? undefined
                      : t('admin.providers.decisionTest.optionDescription')
                  "
                  :disabled="disabled"
                  rows="2"
                />
              </div>
            </template>
          </RuleListEditor>
          <details class="space-y-3">
            <summary class="cursor-pointer text-xs text-gray-500 dark:text-dark-400">
              {{
                t(
                  item.type === 'noul'
                    ? 'admin.providers.decisionTest.questionSettings'
                    : 'admin.providers.decisionTest.questionId',
                )
              }}
            </summary>
            <div class="space-y-1">
              <label class="input-label" :for="`decision-id-${index}`">{{
                t('admin.providers.decisionTest.questionId')
              }}</label>
              <input
                :id="`decision-id-${index}`"
                v-model="item.id"
                class="input"
                :disabled="disabled"
                :data-testid="`decision-id-${index}`"
              />
            </div>
            <template v-if="item.type === 'noul'">
              <TextArea
                v-model="item.trueDescription"
                :label="t('admin.providers.decisionTest.trueDescription')"
                :disabled="disabled"
                rows="2"
              />
              <TextArea
                v-model="item.falseDescription"
                :label="t('admin.providers.decisionTest.falseDescription')"
                :disabled="disabled"
                rows="2"
              />
            </template>
          </details>
        </div>
      </template>
    </RuleListEditor>
    <p v-if="validation.error" class="text-xs text-red-500" role="alert">
      {{ validation.error }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import RuleListEditor from '@/components/common/RuleListEditor.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import Icon from '@/components/icons/Icon.vue'
import { vSegmented } from '@/directives/segmented'
import type { SystemOneTestPayload, SystemOneTestPreview } from './systemOneTest'
import {
  hasImageReference,
  readSystemOneImage,
  SYSTEM_ONE_REQUEST_LIMIT,
  type SystemOneImageDraft,
} from './systemOneImages'

/**
 * SystemOneTestForm.vue edits state and typed questions for decision connection tests and emits
 * a validated request or null. Text and JSON state retain their submitted representation, while
 * question identifiers and option order determine how the response view can explain answers.
 *
 * Choice criteria use unique trimmed option identifiers; score criteria preserve the displayed
 * level order; noul criteria include nonempty true and false descriptions. Validation stops at
 * the first error and withholds the complete payload, so the parent disables single and batch
 * execution for an invalid draft. Adding a question searches unused generated identifiers to
 * keep a deleted question from causing a later generated collision. Image reads publish only
 * while this form remains mounted; submitted previews and Box targets are separate from the
 * wire payload, allowing completed results to retain the image that was actually evaluated.
 * Image names and references keep their full values for tooltips, copying, and submission;
 * single-line summaries leave separate rows for actions in the narrow settings column.
 */

defineProps<{
  disabled: boolean // disable question form controls
}>()
/**
 * emit publishes validation results to the owning test dialog, where null disables execution
 * until the form becomes valid.
 */
const emit = defineEmits<{
  /**
   * Emits the complete valid payload or null whenever validation changes, including the initial
   * form state.
   */
  change: [payload: SystemOneTestPayload | null, preview: SystemOneTestPreview]
}>()
/**
 * Translation, field identity, and state-format controls initialize a localized text-state
 * draft that may be edited as JSON.
 */
const { t } = useI18n()
const stateFieldId = useId()
const stateFormat = ref('text')
const stateText = ref(t('admin.providers.decisionTest.defaultState'))
/**
 * Images and upload feedback belong to the mounted form. The generation invalidates late reads
 * on unmount, and focusedField identifies a text destination without altering structured JSON.
 */
const images = ref<SystemOneImageDraft[]>([])
const uploading = ref(false)
const uploadError = ref('')
const copiedReference = ref('')
const removedReferences = ref<string[]>([])
const focusedField = ref<HTMLTextAreaElement | null>(null)
let uploadGeneration = 0
// Switching the state representation invalidates a remembered state-text destination. Keep
// other text fields eligible, while JSON state remains reserved for manual editing.
watch(stateFormat, () => {
  if (focusedField.value?.id === stateFieldId) focusedField.value = null
})
// Invalidate pending reads before their continuations publish into an unmounted form; a new
// dialog owns its own image list and must not receive those completed drafts.
onUnmounted(() => {
  uploadGeneration++
})
/** imageOptions labels repeated filenames by their stable position in the current upload list. */
const imageOptions = computed(() =>
  images.value.map((image, index) => ({
    value: image.reference,
    label: `${index + 1}. ${image.name}`,
  })),
)
/**
 * State-format and question-type choices determine validation and the representation used when
 * building the emitted decision request payload.
 */
const stateFormats = computed(() => [
  { value: 'text', label: t('admin.providers.decisionTest.text') },
  { value: 'json', label: 'JSON' },
])
const questionTypes = computed(() =>
  ['noul', 'choice', 'score', 'box'].map((value) => ({
    value,
    label: t(`admin.providers.decisionTest.${value}`),
  })),
)
/**
 * QuestionDraft retains editable values for every question kind while the selected type
 * determines submitted criteria.
 */
type QuestionDraft = {
  id: string // editable question or option identifier
  type: 'noul' | 'choice' | 'score' | 'box' // selected decision question category
  imageReference: string // selected Box target reference
  instructions: string // submitted question instruction text
  trueDescription: string // optional true outcome description
  falseDescription: string // optional false outcome description
  /**
   * Editable choice options and score levels share this array, and score submission preserves
   * its current order.
   */
  options: Array<{
    id: string // editable question or option identifier
    description: string // editable option explanation text
  }>
}
/**
 * questions owns editable drafts for the mounted form, starting with one localized noul
 * question and two optional choice or score entries.
 */
const questions = ref<QuestionDraft[]>([
  {
    id: 'available',
    type: 'noul',
    imageReference: '',
    instructions: t('admin.providers.decisionTest.defaultQuestion'),
    trueDescription: '',
    falseDescription: '',
    options: [
      { id: 'a', description: '' },
      { id: 'b', description: '' },
    ],
  },
])

/**
 * addQuestion appends a new noul question with an unused generated identifier. Existing
 * identifiers are checked after deletions or manual edits, and new questions start with two
 * editable option drafts.
 */
function addQuestion() {
  let number = questions.value.length + 1
  while (questions.value.some((item) => item.id === `question_${number}`)) number++
  questions.value.push({
    id: `question_${number}`,
    type: 'noul',
    imageReference: '',
    instructions: '',
    trueDescription: '',
    falseDescription: '',
    options: [
      { id: 'a', description: '' },
      { id: 'b', description: '' },
    ],
  })
}

/**
 * moveOption moves an option within the supplied question draft in place. Callers supply valid
 * positions; score requests use the resulting array order as the meaning of their levels.
 */
function moveOption(question: QuestionDraft, from: number, to: number) {
  const [option] = question.options.splice(from, 1)
  question.options.splice(to, 0, option)
}

/**
 * uploadImages reads one browser selection and keeps each successfully decoded image.
 * A failed file leaves prior images and text intact. Only the current mounted generation may
 * publish results, so closing the dialog cannot mutate its replacement form or enable a stale
 * request. Uploading withholds the payload until all selected files have finished validation.
 * Each accepted file receives its own stable reference even when filenames repeat.
 */
async function uploadImages(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  const generation = ++uploadGeneration
  uploading.value = true
  uploadError.value = ''
  for (const file of files) {
    try {
      const image = await readSystemOneImage(file)
      if (generation !== uploadGeneration) return
      images.value.push(image)
    } catch (error) {
      // Keep previously accepted images when one file fails, allowing correction without
      // rebuilding the entire test state or repeating earlier successful file reads.
      if (generation !== uploadGeneration) return
      uploadError.value = `${file.name}: ${t(`admin.providers.decisionTest.${error instanceof Error ? error.message : 'imageReadFailed'}`)}`
    }
  }
  if (generation === uploadGeneration) uploading.value = false
}

/**
 * rememberField retains the last text field eligible for reference insertion.
 * JSON state is edited manually, so focusing that field clears the insertion destination;
 * buttons and selectors leave the previous text selection available for the insert action.
 */
function rememberField(event: FocusEvent) {
  if (!(event.target instanceof HTMLTextAreaElement)) return
  focusedField.value =
    event.target.id === stateFieldId && stateFormat.value === 'json' ? null : event.target
}

/**
 * insertReference replaces the remembered text selection with an image placeholder.
 * Without an eligible mounted field, text state receives the reference at its end. Structured
 * state remains untouched, and native input dispatch updates the owning Vue field model.
 */
function insertReference(reference: string) {
  const field = focusedField.value
  if (field?.isConnected && !(field.id === stateFieldId && stateFormat.value === 'json')) {
    field.setRangeText(reference, field.selectionStart, field.selectionEnd, 'end')
    field.dispatchEvent(new Event('input', { bubbles: true }))
    field.focus()
  } else if (stateFormat.value === 'text') {
    stateText.value += `\n${reference}`
  }
}

/**
 * copyReference copies a placeholder for manual JSON editing and records local feedback.
 * Clipboard failures remain beside the upload controls; the current test draft is retained and
 * users can still select and copy the visible reference themselves.
 */
async function copyReference(reference: string) {
  try {
    await navigator.clipboard.writeText(reference)
    copiedReference.value = reference
  } catch {
    // Clipboard access can be unavailable in an insecure or restricted browser context.
    // Preserve the visible reference and show a manual-copy recovery message beside it.
    uploadError.value = t('admin.providers.decisionTest.copyFailed')
  }
}

/**
 * removeImage removes an uploaded image and clears any Box targets pointing to it.
 * Text references stay visible for manual correction; completed results use their submitted
 * snapshots and therefore keep their image even after this draft changes.
 */
function removeImage(reference: string) {
  images.value = images.value.filter((image) => image.reference !== reference)
  removedReferences.value.push(reference)
  for (const question of questions.value) {
    if (question.imageReference === reference) question.imageReference = ''
  }
}

/**
 * validation checks state parsing, unique question identifiers, instructions, and type-specific
 * criteria before exposing the complete request to the parent.
 */
const validation = computed<{
  payload: SystemOneTestPayload | null // validated request or null
  error: string // localized validation failure message
}>(() => {
  /**
   * invalid creates the empty payload and translated message for one validation failure.
   */
  const invalid = (key: string) => ({
    payload: null,
    error: t(`admin.providers.decisionTest.${key}`),
  })
  let state: string | object = stateText.value
  if (uploading.value) return { payload: null, error: '' }
  if (stateFormat.value === 'json') {
    try {
      state = JSON.parse(stateText.value)
    } catch {
      // Invalid JSON prevents publishing any payload so the parent cannot submit the textual
      // draft as structured state.
      return invalid('invalidState')
    }
    if (state === null || (typeof state !== 'string' && typeof state !== 'object'))
      // Invalid JSON prevents publishing any payload so the parent cannot submit the textual
      // draft as structured state.
      return invalid('invalidState')
  }
  const entries: Array<[string, SystemOneTestPayload['questions'][string]]> = []
  const ids = new Set<string>()
  for (const item of questions.value) {
    const id = item.id.trim()
    if (!id || ids.has(id)) return invalid('invalidIds')
    ids.add(id)
    if (!item.instructions.trim()) return invalid('missingInstructions')
    const question: SystemOneTestPayload['questions'][string] = {
      type: item.type,
      instructions: item.instructions,
    }
    if (item.type === 'choice') {
      const keys = item.options.map((option) => option.id.trim())
      if (keys.some((key) => !key) || new Set(keys).size !== keys.length)
        return invalid('invalidOptions')
      question.criteria = Object.fromEntries(
        item.options.map((option, index) => [keys[index], option.description]),
      )
    } else if (item.type === 'score') {
      // Score criteria require between two and ten ordered levels.
      if (item.options.length < 2 || item.options.length > 10) return invalid('invalidLevels')
      question.criteria = item.options.map((option) => option.description)
    } else if (item.type === 'noul') {
      const criteria = Object.fromEntries(
        [
          ['true', item.trueDescription],
          ['false', item.falseDescription],
        ].filter(([, value]) => value.trim()),
      )
      if (Object.keys(criteria).length) question.criteria = criteria
    } else {
      if (!images.value.some((image) => image.reference === item.imageReference))
        return invalid('missingBoxImage')
      question.instructions = `${item.instructions}\nImage: ${item.imageReference}`
    }
    entries.push([id, question])
  }
  const payload: SystemOneTestPayload = { state, questions: Object.fromEntries(entries) }
  for (const reference of removedReferences.value) {
    if (
      hasImageReference(state, reference) ||
      Object.values(payload.questions).some(
        (question) =>
          hasImageReference(question.instructions, reference) ||
          hasImageReference(question.criteria, reference),
      )
    )
      return invalid('removedImageReference')
  }
  if (images.value.length) {
    payload.assets = Object.fromEntries(images.value.map((image) => [image.reference, image.asset]))
    for (const image of images.value) {
      const used =
        hasImageReference(state, image.reference) ||
        Object.values(payload.questions).some(
          (question) =>
            hasImageReference(question.instructions, image.reference) ||
            hasImageReference(question.criteria, image.reference),
        )
      if (!used)
        return {
          payload: null,
          error: t('admin.providers.decisionTest.unusedImage', { name: image.name }),
        }
    }
    // Count the model field as part of the wire JSON. The backend repeats the exact check
    // after model mapping; this guard catches large drafts before they leave the browser.
    if (
      new TextEncoder().encode(JSON.stringify({ ...payload, model: 'jev-latest' })).length >
      SYSTEM_ONE_REQUEST_LIMIT
    )
      return invalid('requestTooLarge')
  }
  return { payload, error: '' }
})
/**
 * preview binds each Box question to its selected image independently of upstream JSON fields.
 * The run copies this snapshot on submission, so editing images cannot relabel completed boxes.
 */
const preview = computed<SystemOneTestPreview>(() => ({
  images: Object.fromEntries(
    images.value.map((image) => [image.reference, { name: image.name, src: image.src }]),
  ),
  boxImages: Object.fromEntries(
    questions.value
      .filter((question) => question.type === 'box')
      .map((question) => [question.id.trim(), question.imageReference]),
  ),
}))
watch([validation, preview], ([result, snapshot]) => emit('change', result.payload, snapshot), {
  immediate: true,
})
</script>
