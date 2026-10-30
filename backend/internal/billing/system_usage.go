package billing

import "encoding/json"

// system_usage.go defines provider-only settlement evidence for internal model
// calls. These commands carry no customer funding identity. A stable attempt
// identifier and immutable evidence let persistence retry without repeating
// provider spending or inventing a customer charge.

// SystemUsageCommand supplies one observed model attempt to provider-only
// settlement. The caller freezes Evidence and monetary inputs before retrying.
type SystemUsageCommand struct {
	RequestID    string // unique upstream attempt identity
	ProviderID   int64  // executing provider identifier
	ProviderType string // recorded provider credential type
	// CostUSD includes the provider multiplier and can describe an observed
	// partial cost. Nil means pricing evidence is insufficient, never free usage.
	CostUSD *float64
	// Evidence remains caller-owned and immutable throughout ApplySystemUsage and
	// every retry of this command, so its fingerprint describes the same observation.
	Evidence json.RawMessage // immutable usage and attribution snapshot
}

// SystemUsageResult reports a committed provider-only settlement. Duplicate
// submissions leave Applied false and do not supply a new quota observation.
type SystemUsageResult struct {
	Applied bool // newly committed system observation
	// QuotaState is present only when positive known cost updates an eligible
	// provider. Unknown, zero-cost, ineligible and duplicate operations leave it nil.
	QuotaState *ProviderQuotaState // committed provider quota counters
}
