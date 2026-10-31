/**
 * systemOneTest.ts defines decision-test request and result records shared by the form, stream
 * runner, and answer view. Question identifiers associate returned answers with submitted
 * definitions, while usage validity is carried separately by the surrounding completion event
 * and run state.
 */

/**
 * SystemOneTestPayload combines a textual or structured state with keyed decision questions.
 */
export interface SystemOneTestPayload {
  state: string | object // textual or structured decision state
  assets?: Record<string, SystemOneImageAsset> // reference-keyed inline images
  /**
   * Questions are keyed by submitted identifiers, which the answer view uses to restore
   * definition order and descriptions.
   */
  questions: Record<
    string,
    {
      type: 'noul' | 'choice' | 'score' | 'box' // decision answer category discriminator
      instructions: string // question instructions for upstream
      /**
       * Choice uses keyed descriptions, score uses ordered levels, and noul optionally names
       * true and false meanings.
       */
      criteria?: Record<string, string> | string[]
    }
  >
}

/**
 * SystemOneImageAsset carries a PNG or JPEG in the administrator's JSON test request.
 */
export interface SystemOneImageAsset {
  type: 'image' // image asset discriminator
  format: 'png' | 'jpg' // encoded image format
  data_base64: string // standard padded base64
}

/**
 * SystemOneTestPreview retains image names and explicit Box targets for one submitted test.
 * It belongs to browser state and is excluded from the administrator HTTP body.
 */
export interface SystemOneTestPreview {
  images: Record<string, SystemOnePreviewImage> // submitted image names and data URLs
  boxImages: Record<string, string> // question IDs to image references
}

/**
 * SystemOnePreviewImage retains the submitted filename and immutable display data URL.
 */
export interface SystemOnePreviewImage {
  name: string // original image filename
  src: string // submitted image data URL
}

/**
 * SystemOneBox expresses four edges relative to the displayed image's full dimensions.
 */
export interface SystemOneBox {
  top: number // normalized top edge
  bottom: number // normalized bottom edge
  left: number // normalized left edge
  right: number // normalized right edge
}

/**
 * systemOneInputKey identifies the state, questions, and image references of an editable form.
 * Each uploaded file receives a fresh reference, so comparing keys detects image replacement
 * without repeatedly serializing base64 data. Null identifies an invalid draft that cannot be
 * submitted; the result panel can still distinguish it from the last successful submission.
 */
export function systemOneInputKey(payload: SystemOneTestPayload | null | undefined): string | null {
  if (!payload) return null
  return JSON.stringify({
    state: payload.state,
    questions: payload.questions,
    assets: Object.keys(payload.assets ?? {}),
  })
}

/**
 * SystemOneTestResult contains keyed decision answers and optional token accounting reported by
 * the server.
 */
export interface SystemOneTestResult {
  model: string // reported decision model identifier
  answers: Record<
    string,
    {
      type: 'noul' | 'choice' | 'score' | 'box' // decision answer category discriminator
      noul?: number // probability of true answer
      choice?: string // selected choice option identifier
      score?: number // reported continuous score value
      confidence?: number | null // null means upstream withheld confidence
      box?: SystemOneBox // normalized image rectangle
      probabilities?: Record<string, number> // probabilities by option identifier
      /**
       * Optional response descriptions take precedence over submitted score criteria in the
       * answer view when a matching key exists.
       */
      legend?: unknown
    }
  > // decision answers by question identifier
  /**
   * Token counts are displayed only when the enclosing run also marks decisionUsageValid as
   * true.
   */
  usage?: {
    input_tokens: number // input token count
    output_tokens: number // output token count
  }
}
