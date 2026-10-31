import type { SystemOneImageAsset } from './systemOneTest'

/**
 * systemOneImages.ts reads local decision-test images and checks their transfer boundaries.
 * PNG and JPEG bytes retain their original encoding; browser decoding checks dimensions before
 * the form publishes an asset, and no upload reaches a provider until the administrator starts
 * the test.
 */

/**
 * Image and request limits match TypeSafe's documented upload boundaries. Request limits count
 * UTF-8 JSON bytes, including inline base64, while image limits count the original file bytes.
 */
export const SYSTEM_ONE_IMAGE_LIMIT = 16 * 1024 * 1024
export const SYSTEM_ONE_REQUEST_LIMIT = 32 * 1024 * 1024
const MIN_IMAGE_SIDE = 280
const MAX_IMAGE_SIDE = 1568

/**
 * imageReferenceSequence distinguishes references within this page when no browser crypto API
 * is available. References identify request content and are never authentication credentials.
 */
let imageReferenceSequence = 0

/**
 * SystemOneImageDraft associates an uploaded image with its stable placeholder and local name.
 */
export interface SystemOneImageDraft {
  reference: string // opaque per-image reference
  name: string // original local filename
  src: string // image preview data URL
  width: number // decoded pixel width
  height: number // decoded pixel height
  asset: SystemOneImageAsset // upstream inline image data
}

/**
 * readSystemOneImage reads a PNG or JPEG without resizing or recompressing it.
 * Errors carry decisionTest translation keys. Callers own publication of the returned draft
 * and discard stale reads when their form closes or a newer upload replaces the selection.
 */
export async function readSystemOneImage(file: File): Promise<SystemOneImageDraft> {
  if (!file.size || file.size > SYSTEM_ONE_IMAGE_LIMIT) throw new Error('imageTooLarge')
  const data = await readImageData(file)
  const encoded = data.slice(data.indexOf(',') + 1)
  const signature = atob(encoded.slice(0, 16))
  let format: SystemOneImageAsset['format']
  if (signature.startsWith('\x89PNG\r\n\x1a\n')) format = 'png'
  else if (signature.startsWith('\xff\xd8\xff')) format = 'jpg'
  else throw new Error('invalidImageFormat')
  const src = `data:image/${format === 'jpg' ? 'jpeg' : 'png'};base64,${encoded}`
  const { width, height } = await imageDimensions(src)
  if (
    width < MIN_IMAGE_SIDE ||
    height < MIN_IMAGE_SIDE ||
    width > MAX_IMAGE_SIDE ||
    height > MAX_IMAGE_SIDE
  ) {
    throw new Error('invalidImageDimensions')
  }
  return {
    reference: newImageReference(),
    name: file.name,
    src,
    width,
    height,
    asset: { type: 'image', format, data_base64: encoded },
  }
}

/**
 * newImageReference creates an opaque placeholder for one uploaded image.
 * Secure contexts use randomUUID; browsers without that method use random bytes, which remain
 * available on ordinary HTTP origins. When crypto is absent altogether, a page-local sequence
 * distinguishes references even when uploads share a clock reading. Only uniqueness within the
 * request is required: the placeholder grants no access to an image or account by itself.
 */
function newImageReference(): string {
  const browserCrypto = globalThis.crypto
  if (typeof browserCrypto?.randomUUID === 'function') return `<${browserCrypto.randomUUID()}>`
  if (typeof browserCrypto?.getRandomValues === 'function') {
    // randomUUID requires a secure context, while getRandomValues supports HTTP deployments.
    // Keep a full random identifier so references are unlikely to collide with ordinary text.
    const bytes = browserCrypto.getRandomValues(new Uint8Array(16))
    const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
    return `<image-${hex}>`
  }
  // A process-local counter preserves distinct references without browser crypto, including
  // multiple uploads in one millisecond. These values are content labels rather than secrets.
  imageReferenceSequence++
  return `<image-${Date.now().toString(36)}-${imageReferenceSequence.toString(36)}>`
}

/**
 * readImageData reads one local file as a data URL for transfer and preview.
 * Browser read failures reject with a localized-error key; the reader owns no persistent URL
 * or event listener after its single load or error completion.
 */
function readImageData(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(new Error('imageReadFailed'))
    reader.onabort = () => reject(new Error('imageReadFailed'))
    reader.readAsDataURL(file)
  })
}

/**
 * imageDimensions decodes a local data URL and returns its natural pixel dimensions.
 * Invalid image bytes reject before submission. The temporary image never enters the document
 * and uses no object URL requiring revocation by the form.
 */
function imageDimensions(src: string): Promise<{ width: number; height: number }> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve({ width: image.naturalWidth, height: image.naturalHeight })
    image.onerror = () => reject(new Error('imageReadFailed'))
    image.src = src
  })
}

/**
 * hasImageReference searches description values for a placeholder, excluding all object keys.
 * It mirrors the API's placement rule for string, array, and object descriptions; missing or
 * primitive nonstring descriptions cannot consume an uploaded image.
 */
export function hasImageReference(value: unknown, reference: string): boolean {
  if (typeof value === 'string') return value.includes(reference)
  if (Array.isArray(value)) return value.some((item) => hasImageReference(item, reference))
  if (value && typeof value === 'object') {
    return Object.values(value).some((item) => hasImageReference(item, reference))
  }
  return false
}
