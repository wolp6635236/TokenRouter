import { afterEach, describe, expect, it, vi } from 'vitest'
import { hasImageReference, readSystemOneImage, SYSTEM_ONE_IMAGE_LIMIT } from '../systemOneImages'

/**
 * systemOneImages.spec.ts checks local file validation before a decision test can be submitted.
 * Browser image decoding is replaced with dimensions under test control, while real FileReader
 * encoding preserves the selected bytes and image signatures determine the declared format.
 */

/**
 * pngHeader supplies opaque PNG-signature bytes for the independent dimension-decoder fixture.
 */
const pngHeader = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0])

/**
 * stubDimensions makes image completion report fixed natural dimensions without network I/O.
 */
function stubDimensions(width: number, height: number) {
  vi.stubGlobal(
    'Image',
    class {
      naturalWidth = width // fixture pixel width
      naturalHeight = height // fixture pixel height
      onload?: () => void // installed load callback
      /**
       * src completes the fixture's decode when the reader assigns its local data URL.
       */
      set src(_value: string) {
        this.onload?.()
      }
    },
  )
}

describe('SystemOne image inputs', () => {
  afterEach(() => vi.unstubAllGlobals())

  // A PNG selected with a misleading browser MIME type keeps its actual bytes and format.
  // No resizing or re-encoding changes the uploaded image while a fresh reference is assigned.
  it('uses file signatures and preserves the selected bytes', async () => {
    stubDimensions(600, 400)
    const image = await readSystemOneImage(
      new File([pngHeader], 'photo.dat', { type: 'application/octet-stream' }),
    )
    expect(image.asset.format).toBe('png')
    expect(atob(image.asset.data_base64).charCodeAt(0)).toBe(137)
    expect(image.src).toMatch(/^data:image\/png;base64,/)
    expect(image.reference).toMatch(/^<[^>]+>$/)
    expect(image.width).toBe(600)
  })

  // HTTP origins can expose getRandomValues without randomUUID. Uploads still retain their
  // original image bytes and receive distinct placeholders from the available random source.
  it('accepts uploads when randomUUID is unavailable on HTTP', async () => {
    stubDimensions(600, 400)
    let sequence = 0
    const getRandomValues = vi.fn((bytes: Uint8Array) => {
      bytes.fill(++sequence)
      return bytes
    })
    vi.stubGlobal('crypto', { getRandomValues })
    const first = await readSystemOneImage(new File([pngHeader], 'first.png'))
    const second = await readSystemOneImage(new File([pngHeader], 'second.png'))
    expect(getRandomValues).toHaveBeenCalledTimes(2)
    expect(first.reference).toMatch(/^<image-[0-9a-f]{32}>$/)
    expect(first.reference).not.toBe(second.reference)
    expect(first.asset.data_base64).toBe(second.asset.data_base64)
  })

  // A browser without either crypto method can still label images within one request.
  // Consecutive uploads stay distinct even when the fallback clock value is identical.
  it('keeps references distinct without any crypto API', async () => {
    stubDimensions(600, 400)
    vi.stubGlobal('crypto', undefined)
    const clock = vi.spyOn(Date, 'now').mockReturnValue(42)
    try {
      const first = await readSystemOneImage(new File([pngHeader], 'first.png'))
      const second = await readSystemOneImage(new File([pngHeader], 'second.png'))
      expect(first.reference).toMatch(/^<image-[0-9a-z]+-[0-9a-z]+>$/)
      expect(first.reference).not.toBe(second.reference)
    } finally {
      // Restore the frozen clock before other image tests run so their asynchronous browser
      // fixtures continue to observe the normal test environment rather than this fallback case.
      clock.mockRestore()
    }
  })

  // Unsupported signatures, oversized files, and out-of-range dimensions fail before a request
  // exists. The two accepted side-length boundaries retain the corresponding image dimensions.
  it('checks format, file size, and dimension boundaries', async () => {
    await expect(readSystemOneImage(new File(['GIF89a'], 'image.gif'))).rejects.toThrow(
      'invalidImageFormat',
    )
    await expect(readSystemOneImage(new File([], 'empty.png'))).rejects.toThrow('imageTooLarge')
    const oversized = new File([pngHeader], 'large.png')
    Object.defineProperty(oversized, 'size', { value: SYSTEM_ONE_IMAGE_LIMIT + 1 })
    await expect(readSystemOneImage(oversized)).rejects.toThrow('imageTooLarge')
    for (const side of [279, 1569]) {
      stubDimensions(side, 400)
      await expect(readSystemOneImage(new File([pngHeader], 'photo.png'))).rejects.toThrow(
        'invalidImageDimensions',
      )
    }
    for (const side of [280, 1568]) {
      stubDimensions(side, side)
      expect((await readSystemOneImage(new File([pngHeader], 'photo.png'))).width).toBe(side)
    }
  })

  // References may appear inside nested string values or longer sentences. Matching an object
  // key alone does not supply the asset to the model and cannot pass form validation.
  it('checks only description values when matching references', () => {
    expect(hasImageReference({ key: ['before <photo> after'] }, '<photo>')).toBe(true)
    expect(hasImageReference({ '<photo>': 'plain text' }, '<photo>')).toBe(false)
    expect(hasImageReference(null, '<photo>')).toBe(false)
  })
})
