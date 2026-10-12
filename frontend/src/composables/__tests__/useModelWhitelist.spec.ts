import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/providers', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import {
  buildCombinedModelMappingObject,
  buildModelMappingObject,
  buildPersistedModelRestriction,
  splitQoderPersistedModelRestriction,
  splitModelMappingObject
} from '../useModelWhitelist'

describe('useModelWhitelist', () => {

  it('qoder 默认提供商未触碰模型限制时不会生成限制配置', () => {
    expect(buildPersistedModelRestriction([], [])).toEqual({
      modelMapping: null,
      modelWhitelist: []
    })
  })

  it('qoder legacy 映射会保留为显式映射规则', () => {
    expect(splitQoderPersistedModelRestriction({
      'claude-opus-4-6': 'ultimate',
      auto: 'auto',
      custom: 'ultimate'
    })).toEqual({
      allowedModels: [],
      modelMappings: [
        { from: 'claude-opus-4-6', to: 'ultimate' },
        { from: 'auto', to: 'auto' },
        { from: 'custom', to: 'ultimate' }
      ]
    })
  })

  it('qoder 新格式优先使用 model_whitelist，并保留 legacy raw self mapping', () => {
    expect(splitQoderPersistedModelRestriction({
      ultimate: 'ultimate',
      'claude-opus-4-6': 'ultimate'
    }, ['ultimate'])).toEqual({
      allowedModels: ['ultimate'],
      modelMappings: [
        { from: 'ultimate', to: 'ultimate' },
        { from: 'claude-opus-4-6', to: 'ultimate' }
      ]
    })
  })

  it('combined 模式支持 Grok 4.5 官方别名映射', () => {
    const mapping = buildCombinedModelMappingObject(
      ['grok-4.5'],
      [
        { from: 'grok-latest', to: 'grok-4.5' },
        { from: 'grok-4.5-latest', to: 'grok-4.5' },
        { from: 'grok-build-latest', to: 'grok-4.5' }
      ]
    )

    expect(mapping).toEqual({
      'grok-4.5': 'grok-4.5',
      'grok-latest': 'grok-4.5',
      'grok-4.5-latest': 'grok-4.5',
      'grok-build-latest': 'grok-4.5'
    })
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-3.1-flash-image'], [])
    expect(mapping).toEqual({
      'gemini-3.1-flash-image': 'gemini-3.1-flash-image'
    })
  })

  it('whitelist 模式会保留 GPT-5.3 Spark 的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.3-spark'], [])

    expect(mapping).toEqual({
      'gpt-5.3-spark': 'gpt-5.3-spark'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })

  it('splitModelMappingObject 只把精确自映射当作最终白名单', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.3-codex': 'gpt-5.3-codex-spark',
      'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark',
      'gpt-5.4': 'gpt-5.4',
      'claude-*': 'claude-sonnet-4-5'
    })

    expect(parsed.allowedModels).toEqual(['gpt-5.3-codex-spark', 'gpt-5.4'])
    expect(parsed.modelMappings).toEqual([
      { from: 'gpt-5.3-codex', to: 'gpt-5.3-codex-spark' },
      { from: 'claude-*', to: 'claude-sonnet-4-5' }
    ])
  })

  it('buildCombinedModelMappingObject 会同时保存最终白名单和显式映射', () => {
    const mapping = buildCombinedModelMappingObject(
      ['gpt-5.3-codex-spark', 'gpt-5.4'],
      [{ from: 'gpt-5.3-codex', to: 'gpt-5.3-codex-spark' }]
    )

    expect(mapping).toEqual({
      'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark',
      'gpt-5.4': 'gpt-5.4',
      'gpt-5.3-codex': 'gpt-5.3-codex-spark'
    })
  })

  it('buildPersistedModelRestriction 在空白名单时仍显式返回空数组', () => {
    const persisted = buildPersistedModelRestriction([], [
      { from: 'gpt-5.4', to: 'gpt-5.4' }
    ])

    expect(persisted).toEqual({
      modelMapping: {
        'gpt-5.4': 'gpt-5.4'
      },
      modelWhitelist: []
    })
  })
})
