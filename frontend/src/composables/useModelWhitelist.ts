import { isValidWildcardPattern } from '@/utils/modelMappingRules'

export type QoderSite = 'global' | 'cn'

import { getAntigravityDefaultModelMapping } from '@/api/admin/providers'

let _antigravityDefaultMappingsCache: { from: string; to: string }[] | null = null

// 从后端读取 Antigravity 的专用路由映射。
export async function fetchAntigravityDefaultMappings(): Promise<{ from: string; to: string }[]> {
  if (_antigravityDefaultMappingsCache !== null) {
    return _antigravityDefaultMappingsCache
  }
  try {
    const mapping = await getAntigravityDefaultModelMapping()
    _antigravityDefaultMappingsCache = Object.entries(mapping).map(([from, to]) => ({ from, to }))
  } catch (e) {
    console.warn('[fetchAntigravityDefaultMappings] API failed, using empty fallback', e)
    _antigravityDefaultMappingsCache = []
  }
  return _antigravityDefaultMappingsCache
}

// =====================
// 常用错误码
// =====================

export const commonErrorCodes = [
  { value: 401, label: 'Unauthorized' },
  { value: 403, label: 'Forbidden' },
  { value: 429, label: 'Rate Limit' },
  { value: 500, label: 'Server Error' },
  { value: 502, label: 'Bad Gateway' },
  { value: 503, label: 'Unavailable' },
  { value: 529, label: 'Overloaded' }
]

// =====================
// 辅助函数
// =====================

// =====================
// 构建模型映射对象（用于 API）
// =====================

// 通配符校验与映射编辑器共用同一实现，继续从这里导出以兼容现有调用方。
export { isValidWildcardPattern }

export function buildModelMappingObject(
  mode: 'whitelist' | 'mapping',
  allowedModels: string[],
  modelMappings: { from: string; to: string }[]
): Record<string, string> | null {
  const mapping: Record<string, string> = {}

  if (mode === 'whitelist') {
    for (const model of allowedModels) {
      // whitelist 模式的本意是"精确模型列表"，如果用户输入了通配符（如 claude-*），
      // 写入 model_mapping 会导致 GetMappedModel() 把真实模型映射成 "claude-*"，从而转发失败。
      // 因此这里跳过包含通配符的条目。
      if (!model.includes('*')) {
        mapping[model] = model
      }
    }
  } else {
    for (const m of modelMappings) {
      const from = m.from.trim()
      const to = m.to.trim()
      if (!from || !to) continue
      // 校验通配符格式：* 只能放在末尾
      if (!isValidWildcardPattern(from)) {
        console.warn(`[buildModelMappingObject] 无效的通配符格式，跳过: ${from}`)
        continue
      }
      // to 不允许包含通配符
      if (to.includes('*')) {
        console.warn(`[buildModelMappingObject] 目标模型不能包含通配符，跳过: ${from} -> ${to}`)
        continue
      }
      mapping[from] = to
    }
  }

  return Object.keys(mapping).length > 0 ? mapping : null
}

// splitModelMappingObject 将持久化的 model_mapping 拆成：
// 1. 最终模型白名单：仅精确自映射（from == to）；
// 2. 显式映射规则：通配符映射或 from != to 的精确映射。
export function splitModelMappingObject(
  existingMappings?: Record<string, string>
): { allowedModels: string[]; modelMappings: { from: string; to: string }[] } {
  const nextAllowedModels: string[] = []
  const nextModelMappings: { from: string; to: string }[] = []

  if (existingMappings && typeof existingMappings === 'object') {
    for (const [rawFrom, rawTo] of Object.entries(existingMappings)) {
      const from = rawFrom.trim()
      const to = String(rawTo).trim()
      if (!from || !to) {
        continue
      }

      if (!from.includes('*') && from === to) {
        nextAllowedModels.push(from)
        continue
      }

      nextModelMappings.push({ from, to })
    }
  }

  return {
    allowedModels: Array.from(new Set(nextAllowedModels)),
    modelMappings: nextModelMappings
  }
}

export interface PersistedModelRestriction {
  modelMapping: Record<string, string> | null
  modelWhitelist: string[]
}

// normalizeModelWhitelist 将白名单规范成“精确模型列表”。
// whitelist 输入按具体模型处理，通配符属于 request-side mapping 规则。
export function normalizeModelWhitelist(rawWhitelist?: unknown): string[] {
  if (!Array.isArray(rawWhitelist)) {
    return []
  }

  const normalized: string[] = []
  for (const rawModel of rawWhitelist) {
    const model = String(rawModel).trim()
    if (!model || model.includes('*')) {
      continue
    }
    normalized.push(model)
  }

  return Array.from(new Set(normalized))
}

// splitPersistedModelRestriction 优先读取新的独立 model_whitelist 字段；
// 如果旧数据还把白名单编码在 model_mapping 的自映射里，则继续兼容解析。
export function splitPersistedModelRestriction(
  existingMappings?: Record<string, string>,
  rawWhitelist?: unknown
): { allowedModels: string[]; modelMappings: { from: string; to: string }[] } {
  const parsedMapping = splitModelMappingObject(existingMappings)

  if (Array.isArray(rawWhitelist)) {
    // 独立 model_whitelist 出现后，model_mapping 的每一项都是请求侧映射。
    // 不能再把精确自映射当作旧格式白名单，否则保存后重新打开会丢失这类映射。
    const modelMappings = Object.entries(existingMappings || {}).flatMap(([rawFrom, rawTo]) => {
      const from = rawFrom.trim()
      const to = String(rawTo).trim()
      return from && to ? [{ from, to }] : []
    })

    return {
      allowedModels: normalizeModelWhitelist(rawWhitelist),
      modelMappings
    }
  }

  return parsedMapping
}

// buildPersistedModelRestriction 将白名单与映射分别持久化。
// 普通提供商使用 model_mapping 表示请求侧映射，model_whitelist 表示映射后的最终白名单。
// 注意：即使白名单为空，也要显式返回空数组，作为“无白名单限制”的新格式信号，
// 避免后端回退到 legacy 的“自映射即白名单”兼容分支。
export function buildPersistedModelRestriction(
  allowedModels: string[],
  modelMappings: { from: string; to: string }[]
): PersistedModelRestriction {
  const modelMapping = buildModelMappingObject('mapping', [], modelMappings)
  const modelWhitelist = normalizeModelWhitelist(allowedModels)

  return {
    modelMapping,
    modelWhitelist
  }
}

export function splitQoderPersistedModelRestriction(
  existingMappings?: Record<string, string>,
  rawWhitelist?: unknown
): { allowedModels: string[]; modelMappings: { from: string; to: string }[] } {
  const modelMappings: { from: string; to: string }[] = []

  if (existingMappings && typeof existingMappings === 'object') {
    for (const [rawFrom, rawTo] of Object.entries(existingMappings)) {
      const from = rawFrom.trim()
      const to = String(rawTo).trim()
      if (!from || !to) {
        continue
      }
      modelMappings.push({ from, to })
    }
  }

  return {
    allowedModels: normalizeModelWhitelist(rawWhitelist),
    modelMappings
  }
}

// buildCombinedModelMappingObject 同时持久化“最终模型白名单”和“显式映射规则”。
// 其中白名单以精确自映射表示，映射规则则直接覆盖同来源模型。
export function buildCombinedModelMappingObject(
  allowedModels: string[],
  modelMappings: { from: string; to: string }[]
): Record<string, string> | null {
  const whitelistMapping = buildModelMappingObject('whitelist', allowedModels, [])
  const explicitMapping = buildModelMappingObject('mapping', [], modelMappings)

  if (!whitelistMapping && !explicitMapping) {
    return null
  }

  return {
    ...(whitelistMapping || {}),
    ...(explicitMapping || {})
  }
}
