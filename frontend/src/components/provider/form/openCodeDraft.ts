import type { ProtocolID } from '@/api/protocols.types'
import { providerModeChoices } from './providerEndpoints'

// openCodeDraft.ts copies OpenCode model protocol tables between provider
// credentials and the form draft. Each provider_mode owns two tables: accepted
// catalog suggestions from model synchronization and manual overrides. A manual
// choice takes precedence, and deleting it restores the accepted catalog protocol
// for that model, so the tables stay separate through the save round trip.
//
// Hydration copies every mode table, so canceling a dialog leaves the provider
// response retained by its parent unchanged. Serialization copies them again and
// writes only the modes OpenCode accepts. Mode, root address, and address slots
// belong to the shared provider form fields.

// OpenCodeProtocolTable maps a provider_mode to exact model IDs and their protocols.
type OpenCodeProtocolTable = Record<string, Record<string, ProtocolID>>

// OpenCodeDraft contains the editable model protocol tables for every mode.
export interface OpenCodeDraft {
  suggestions: OpenCodeProtocolTable // Accepted catalog suggestions per mode.
  protocols: OpenCodeProtocolTable // Manual exact-model overrides per mode.
}

// copyModeTables returns independent copies of the payg and go tables. Missing
// or malformed mode entries become empty tables.
function copyModeTables(source: unknown): OpenCodeProtocolTable {
  const tables = source as Partial<OpenCodeProtocolTable> | undefined
  return Object.fromEntries(
    providerModeChoices('opencode').map(({ value }) => [value, { ...tables?.[value] }]),
  )
}

// openCodeDraft copies saved OpenCode protocol tables or creates empty ones.
export function openCodeDraft(credentials: Record<string, unknown> = {}): OpenCodeDraft {
  return {
    suggestions: copyModeTables(credentials.opencode_catalog_protocols),
    protocols: copyModeTables(credentials.opencode_model_protocols),
  }
}

// openCodeCredentials serializes both protocol tables for provider persistence.
// Calling forms merge the result into their credential payload before saving.
export function openCodeCredentials(draft: OpenCodeDraft): Record<string, unknown> {
  return {
    opencode_catalog_protocols: copyModeTables(draft.suggestions),
    opencode_model_protocols: copyModeTables(draft.protocols),
  }
}
