import { describe, expect, it } from 'vitest'
// Import the compiler-enabled distribution explicitly so this regression exercises
// literal HTML message compilation and sanitization independently of application aliases.
import { createI18n } from '../../../node_modules/vue-i18n/dist/vue-i18n.mjs'

// security.spec.ts checks the dependency's HTML translation sanitizer at the DOM
// boundary. Detached elements expose decoded attribute names without attaching
// nested documents to the page, so tests can detect srcdoc bypasses without
// executing untrusted markup or changing application translation settings.

describe('HTML translation escaping', () => {
  // Inline and interpolated srcdoc content survive ordinary value escaping because
  // browsers decode it again. The sanitizer must neutralize the attribute itself.
  it.each(['inline', 'interpolated'] as const)('neutralizes %s iframe srcdoc', (source) => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      escapeParameter: true,
      warnHtmlMessage: false,
      messages: {
        en: {
          inline: '<iframe srcdoc="<b>nested document</b>"></iframe>',
          interpolated: '<iframe srcdoc="{content}"></iframe>',
        },
      },
    })
    const container = document.createElement('div')
    container.innerHTML = i18n.global.t(source, { content: '<b>nested document</b>' })
    const frame = container.querySelector('iframe')
    expect(frame).not.toBeNull()
    expect(frame!.hasAttribute('srcdoc')).toBe(false)
  })
})
