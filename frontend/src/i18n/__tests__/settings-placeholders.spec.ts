import { describe, expect, it } from 'vitest'

// Import the compiler-enabled distribution explicitly to verify literal JSON
// messages and named placeholders independently of the application's bundler alias.
import { createI18n } from '../../../node_modules/vue-i18n/dist/vue-i18n.mjs'
import enMessages from '../locales/en'
import zhMessages from '../locales/zh'

// settings-placeholders.spec.ts renders the settings JSON example through the
// compiler-enabled Vue I18n build. Literal braces and named placeholders coexist
// in this copy, so the rendered value must remain valid JSON and retain the
// billing-header placeholder.

describe('settings i18n placeholders', () => {
  // The Claude OAuth blocks example contains JSON braces and a billing-header
  // placeholder. Rendering each language produces parseable JSON while retaining
  // both the blocks field and literal placeholder.
  it.each([
    ['zh', zhMessages],
    ['en', enMessages],
  ])('renders Claude OAuth blocks placeholder as literal JSON for %s', (locale, messages) => {
    const i18n = createI18n({
      legacy: false,
      locale,
      messages: {
        [locale]: messages,
      },
    })

    const placeholder = i18n.global.t(
      'admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocksPlaceholder',
    )

    expect(() => JSON.parse(placeholder)).not.toThrow()
    expect(placeholder).toContain('"blocks"')
    expect(placeholder).toContain('{billing_header}')
  })
})
