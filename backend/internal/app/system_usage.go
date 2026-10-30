package app

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingpostgres "github.com/TokenFlux/TokenRouter/internal/billing/postgres"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
	usagepostgres "github.com/TokenFlux/TokenRouter/internal/usage/postgres"
)

// system_usage.go assembles provider-only model accounting with the existing
// pricing and persistence services. Internal test and quota transports share
// one recorder while ordinary gateway completion keeps its customer funding
// path. The assembly supplies the transaction-bound usage projection without
// introducing a billing dependency on the usage storage implementation.
//
// Each persistence submission copies the priced row and normalizes its request
// category before fingerprinting. SQL insertion can then fill the projection's
// generated ID without changing the evidence used by a later retry. The
// ledger claim, provider spending, scheduler outbox, and projection commit on
// the same connection; a failed projection rolls back all those effects.

// provideSystemUsageTransport binds internal accounting to shared pricing and
// provider storage. The caller owns all dependencies and keeps them available
// until internal request producers have stopped and closed their response bodies.
//
// The projection uses a private row copy because insertion fills its generated
// identity. Evidence is encoded before that mutation, preserving a stable
// fingerprint across retries. Committed provider counters feed quota notices
// only for a newly applied charge, while duplicate settlements and rolled-back
// writes cannot publish another threshold crossing.
func provideSystemUsageTransport(transport httpclient.UpstreamTransport, providers *providerpostgres.ProviderStore, calculator *billing.Calculator, resolver *billing.PriceResolver, settlement *billingpostgres.SettlementStore, client *ent.Client, calendar timezone.Calendar, notifications *billing.BalanceNotifyService, tasks *lifecycle.Tasks) *provideradapter.SystemUsageTransport {
	recorder := &usage.SystemRecorder{Calculator: calculator, Resolver: resolver, RequiresExplicitPricing: func(platform, model string) bool {
		value := provider.Record{Platform: platform}
		return value.RequiresExplicitClaudePricing() && completion.IsClaudeModelAlias(model)
	}}
	recorder.Save = func(ctx context.Context, row *usage.UsageLog, providerType string) error {
		snapshot := *row
		snapshot.ID = 0
		snapshot.RequestType = snapshot.EffectiveRequestType()
		evidence, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		var amount *float64
		if snapshot.SystemUsage.CostState != "unknown" && snapshot.ProviderStatsCost != nil {
			value := *snapshot.ProviderStatsCost
			if snapshot.ProviderRateMultiplier != nil {
				value *= *snapshot.ProviderRateMultiplier
			}
			amount = &value
		}
		command := billing.SystemUsageCommand{RequestID: snapshot.RequestID, ProviderID: snapshot.ProviderID, ProviderType: providerType, CostUSD: amount, Evidence: evidence}
		// The analytical writer uses the ledger owner's transaction directly.
		// Pool batching would break atomicity by committing the row independently.
		result, err := settlement.ApplySystemUsage(ctx, command, func(ctx context.Context, tx *sql.Tx) error {
			store := usagepostgres.NewUsageLogRepositoryWithSQL(client, tx, calendar)
			_, writeErr := store.Create(ctx, &snapshot)
			return writeErr
		})
		if err == nil && result.Applied && result.QuotaState != nil && amount != nil && notifications != nil {
			// Notifications use committed counters so overlapping requests cannot
			// reconstruct a different threshold crossing from a later provider read.
			if value, readErr := providers.GetByID(ctx, row.ProviderID); readErr == nil {
				notifications.CheckProviderQuotaAfterIncrement(ctx, provideradapter.QuotaNotification(value), billing.QuantizeUsageBillingAmount(*amount), result.QuotaState)
			}
		}
		return err
	}
	result := &provideradapter.SystemUsageTransport{Base: transport, Read: providers.GetByID, Recorder: recorder}
	if tasks != nil {
		result.Background = func(fn func()) bool { return tasks.Go("system-model-usage", fn) }
	}
	return result
}
