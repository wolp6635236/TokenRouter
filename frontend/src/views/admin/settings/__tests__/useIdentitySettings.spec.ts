import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { OAuthLoginProvider } from '@/api/auth'
import { createSettingsForm } from '../settingsState'
import { currentOrigin } from '../settingsDefaults'
import type { useIdentitySettings } from '../useIdentitySettings'

/**
 * useIdentitySettings.spec.ts checks callback suggestions and their copy actions
 * against the browser API entry used by OAuth start requests. Mounted setup
 * supplies the same Vue lifecycle as the settings page, while a clipboard
 * substitute records which URLs administrators would copy into their identity
 * provider configuration.
 */

// Clipboard outcomes are controlled independently of draft edits so tests can
// inspect saved suggestions even when the browser reports a copy failure.
const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetModules()
  vi.stubEnv('VITE_API_BASE_URL', '')
  copyToClipboard.mockReset()
  copyToClipboard.mockResolvedValue(true)
})
afterEach(() => {
  vi.unstubAllEnvs()
})

/**
 * setupIdentitySettings mounts the controller with a real reactive settings draft.
 * Each call installs local translations, and automatic wrapper cleanup stops the
 * controller's watchers after the current test finishes.
 */
async function setupIdentitySettings(
  apiBase: string,
  frontendURL = 'https://console.example/portal',
) {
  vi.stubEnv('VITE_API_BASE_URL', apiBase)
  const { useIdentitySettings } = await import('../useIdentitySettings')
  const form = createSettingsForm()
  form.frontend_url = frontendURL
  form.api_base_url = 'https://api.example/v1'
  let controls!: ReturnType<typeof useIdentitySettings>
  mount(
    defineComponent({
      // setup exposes the controller before mounting completes. Rendering stays
      // empty because these assertions inspect reactive suggestions and copy actions;
      // wrapper cleanup owns the controller's watcher lifetime.
      setup() {
        controls = useIdentitySettings(form)
        return () => null
      },
    }),
    {
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            messages: {},
            missingWarn: false,
            fallbackWarn: false,
          }),
        ],
      },
    },
  )
  return { form, controls }
}

/**
 * callbackSuggestions reads every suggested provider callback from the mounted controller.
 */
function callbackSuggestions(controls: ReturnType<typeof useIdentitySettings>) {
  return {
    linuxdo: controls.linuxdoRedirectUrlSuggestion.value,
    github: controls.githubOAuthRedirectUrlSuggestion.value,
    google: controls.googleOAuthRedirectUrlSuggestion.value,
    wechat: controls.wechatRedirectUrlSuggestion.value,
    oidc: controls.oidcRedirectUrlSuggestion.value,
  }
}

describe('identity callback API entry', () => {
  // API entry settings can be relative, absolute, or include a proxy prefix.
  // Suggestions must use that same entry rather than either configurable site address.
  it.each([
    ['', `${currentOrigin}/api/v1`],
    ['/api/v1', `${currentOrigin}/api/v1`],
    ['api/v1///', `${currentOrigin}/api/v1`],
    ['https://api.example.com/api/v1/', 'https://api.example.com/api/v1'],
    ['/proxy/api/v1/', `${currentOrigin}/proxy/api/v1`],
    ['https://api.example.com/proxy/api/v1/', 'https://api.example.com/proxy/api/v1'],
  ])('uses the OAuth API entry %j', async (apiBase, expectedBase) => {
    const { controls } = await setupIdentitySettings(apiBase)
    const { buildOAuthLoginStartURL } = await import('@/api/auth')
    for (const [provider, suggestion] of Object.entries(callbackSuggestions(controls))) {
      expect(suggestion).toBe(`${expectedBase}/auth/oauth/${provider}/callback`)
      const start = new URL(
        buildOAuthLoginStartURL({ provider: provider as OAuthLoginProvider, params: {} }),
        currentOrigin,
      )
      start.pathname = start.pathname.replace(/\/start$/, '/callback')
      expect(suggestion).toBe(start.toString())
    }
  })

  // Administrators edit frontend and model-client addresses while OAuth continues
  // using its deployment API entry. Neither edit may change suggestions or overwrite explicit callbacks.
  it('keeps callbacks on the API entry after unrelated address edits', async () => {
    const { form, controls } = await setupIdentitySettings('https://api.example.com/api/v1')
    form.github_oauth_redirect_url = 'https://custom.example/github'
    const original = callbackSuggestions(controls)
    form.api_base_url = 'https://changed-models.example/v1'
    form.frontend_url = 'https://updated.example/portal/'
    expect(callbackSuggestions(controls)).toEqual(original)
    expect(form.github_oauth_redirect_url).toBe('https://custom.example/github')
  })

  // Each provider action writes the suggested API callback before copying it.
  // A failed clipboard result leaves the editable value available for manual copying.
  it('sets and copies all five callbacks even when copying fails', async () => {
    const { form, controls } = await setupIdentitySettings('https://api.example.com/api/v1')
    copyToClipboard.mockResolvedValue(false)
    await controls.setAndCopyLinuxdoRedirectUrl()
    await controls.setAndCopyEmailOAuthRedirectUrl('github')
    await controls.setAndCopyEmailOAuthRedirectUrl('google')
    await controls.setAndCopyWeChatRedirectUrl()
    await controls.setAndCopyOIDCRedirectUrl()
    const callbacks = {
      linuxdo: form.linuxdo_connect_redirect_url,
      github: form.github_oauth_redirect_url,
      google: form.google_oauth_redirect_url,
      wechat: form.wechat_connect_redirect_url,
      oidc: form.oidc_connect_redirect_url,
    }
    expect(copyToClipboard).toHaveBeenCalledTimes(5)
    for (const [provider, callback] of Object.entries(callbacks)) {
      expect(callback).toBe(`https://api.example.com/api/v1/auth/oauth/${provider}/callback`)
      expect(copyToClipboard).toHaveBeenCalledWith(callback, expect.any(String))
    }
  })
})
