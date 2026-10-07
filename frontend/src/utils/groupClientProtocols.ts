import type { ProtocolID } from '@/types'
import { protocolCatalog } from '@/api/admin/protocolCapabilities'

// 分组共用入口目录，提供商原生能力在选号时检查。
function orderedProtocols(protocols: Iterable<ProtocolID>): ProtocolID[] {
  const selected = new Set(protocols)
  return (protocolCatalog.value?.protocols ?? []).filter(protocol => !protocol.upstream_only && selected.has(protocol.id)).map(protocol => protocol.id)
}

export function supportedGroupClientProtocols(): ProtocolID[] {
  return orderedProtocols(protocolCatalog.value?.groups[0]?.protocols ?? [])
}

export function defaultGroupClientProtocols(): ProtocolID[] {
  return orderedProtocols(protocolCatalog.value?.groups[0]?.defaults ?? [])
}

export function effectiveGroupClientProtocols(protocols: readonly ProtocolID[] | null | undefined): ProtocolID[] {
  if (!protocolCatalog.value) return [...(protocols ?? [])]
  const supported = new Set(protocolCatalog.value.groups[0]?.protocols ?? [])
  return orderedProtocols((protocols ?? []).filter(protocol => supported.has(protocol)))
}

export function setGroupClientProtocol(protocols: readonly ProtocolID[], protocol: ProtocolID, enabled: boolean): ProtocolID[] {
  const next = new Set(protocols)
  if (enabled && supportedGroupClientProtocols().includes(protocol)) next.add(protocol)
  else next.delete(protocol)
  return effectiveGroupClientProtocols([...next])
}

// 历史分组可能残留目录收缩前的转换源（如调整为仅上游的协议），提交前按目录剔除，
// 目标列表同步去重并过滤非法边；显式空数组保留“仅原生”语义。
export function sanitizeGroupProtocolFallbacks(
  fallbacks: Partial<Record<ProtocolID, ProtocolID[]>> | null | undefined,
): Partial<Record<ProtocolID, ProtocolID[]>> {
  const next: Partial<Record<ProtocolID, ProtocolID[]>> = { ...(fallbacks ?? {}) }
  const profile = protocolCatalog.value?.groups[0]
  if (!profile) return next
  const sources = new Set(profile.protocols)
  for (const source of Object.keys(next) as ProtocolID[]) {
    if (!sources.has(source)) {
      delete next[source]
      continue
    }
    const allowed = new Set(profile.fallback_targets[source] ?? [])
    next[source] = (next[source] ?? []).filter(
      (target, index, list) => allowed.has(target) && list.indexOf(target) === index,
    )
  }
  return next
}
