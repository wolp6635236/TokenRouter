import { describe, expect, it } from 'vitest'
import en from '../locales/en/admin/providers'
import zh from '../locales/zh/admin/providers'

describe('Responses WS connection descriptions', () => {
  it('explains connection lifetime without internal mode names', () => {
    expect(zh.providers.openai.wsConnectionPooledHint).toContain('会话结束后')
    expect(zh.providers.openai.wsConnectionPerSessionHint).toContain('关闭连接')
    expect(en.providers.openai.wsConnectionPerSessionHint).toContain('closes')
    expect(zh.providers.openai.wsModeDesc).not.toContain('mode_router_v2_enabled')
  })
})
