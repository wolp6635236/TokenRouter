// modelReferences.ts validates single-hop model references in editor drafts.
// Each concrete target is checked against every reference source, including
// wildcard rules, before submission. The server repeats validation for other
// clients and persisted configuration writes.

// ModelReference describes a source-pattern list and its optional target identity.
interface ModelReference {
  models: string[] // Source model patterns.
  reference_model?: string // Concrete target identity.
}

// validateModelReferences reports an invalid target or a reference-to-reference match.
// Callers supply reference-mode rules only, including drafts whose target is
// still empty. Route selectors do not exempt a target from single-hop checking.
export function validateModelReferences(rules: ModelReference[]): boolean {
  const sources = rules.flatMap((rule) => rule.models.map((model) => model.trim().toLowerCase()))
  return rules.some((rule) => {
    const target = rule.reference_model?.trim().toLowerCase() ?? ''
    if (!target || target.includes('*')) return true
    const short = target
      .replace(/^\/+/, '')
      .replace(/^(?:.*\/)?publishers\/google\/models\//, '')
      .replace(/^models\//, '')
    return [target, short].some((name) =>
      sources.some((source) =>
        source.endsWith('*') ? name.startsWith(source.slice(0, -1)) : name === source,
      ),
    )
  })
}
