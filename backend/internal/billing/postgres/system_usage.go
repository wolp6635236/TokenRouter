package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// system_usage.go commits internal model cost evidence with provider spending
// and the caller's analytical projection. System work has no customer funding
// identity, so it cannot enter the customer settlement transaction. Each
// upstream attempt instead claims its own permanent ledger identity before
// updating any provider counters or writing its usage row.
//
// The fingerprint covers immutable evidence and the unrounded cost. Replaying
// the same command returns without incrementing counters or recreating a
// deliberately cleaned usage row; conflicting evidence fails. The projection
// uses this transaction's connection, and any error rolls back the claim,
// quota counters, outbox event, and analytical row together.

// ApplySystemUsage records one provider-only operation and its transaction-bound
// projection. The caller supplies frozen evidence and a nonnil projection;
// retries reuse the complete command and never repeat upstream execution.
//
// A duplicate with identical evidence returns Applied=false without invoking project.
// A changed fingerprint fails before quota mutation. Provider thresholds use
// the existing customer-settlement quota calculation and outbox writer, while
// user balances, subscriptions, team counters, and API keys are never read.
func (r *SettlementStore) ApplySystemUsage(ctx context.Context, cmd billing.SystemUsageCommand, project func(context.Context, *sql.Tx) error) (*billing.SystemUsageResult, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("system usage storage unavailable")
	}
	if strings.TrimSpace(cmd.RequestID) == "" || cmd.ProviderID <= 0 || project == nil || !json.Valid(cmd.Evidence) {
		return nil, errors.New("invalid system usage command")
	}
	if cmd.CostUSD != nil && (math.IsNaN(*cmd.CostUSD) || math.IsInf(*cmd.CostUSD, 0) || *cmd.CostUSD < 0) {
		return nil, errors.New("invalid system usage cost")
	}
	encoded, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Every exit before Commit discards the claim and all transaction participants.
	// Rollback after a successful commit is harmless and cannot undo that commit.
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO billing_system_usage_entries (request_id, provider_id, fingerprint, cost_usd, evidence) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (request_id) DO NOTHING`, cmd.RequestID, cmd.ProviderID, fingerprint, cmd.CostUSD, []byte(cmd.Evidence))
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		var existing string
		if err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM billing_system_usage_entries WHERE request_id=$1`, cmd.RequestID).Scan(&existing); err != nil {
			return nil, err
		}
		if existing != fingerprint {
			return nil, fmt.Errorf("system usage evidence conflict for %s", cmd.RequestID)
		}
		return &billing.SystemUsageResult{}, nil
	}
	var quotaState *billing.ProviderQuotaState
	if cmd.CostUSD != nil && *cmd.CostUSD > 0 && (cmd.ProviderType == capability.ProviderTypeAPIKey || cmd.ProviderType == capability.ProviderTypeBedrock) {
		quotaState, err = incrementUsageBillingProviderQuota(ctx, tx, cmd.ProviderID, billing.QuantizeUsageBillingAmount(*cmd.CostUSD), r.providerOutbox)
		if err != nil {
			return nil, err
		}
	}
	if err := project(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &billing.SystemUsageResult{Applied: true, QuotaState: quotaState}, nil
}
