//go:build integration

package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerhttp "github.com/TokenFlux/TokenRouter/internal/provider/httpapi"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/usage"
	usagepostgres "github.com/TokenFlux/TokenRouter/internal/usage/postgres"
)

// system_usage_integration_test.go verifies provider-only accounting against
// the production PostgreSQL migrations. The scenarios inspect committed rows,
// rollback, concurrent replay, analytical cleanup, and provider quota counters
// while confirming that no customer or API-key funding records are created.

// TestSystemUsageTransactionAndReplay injects a failed projection, then submits
// the same system attempt concurrently. It verifies atomic rollback, exactly
// one provider increment, nullable attribution, and deduplication after cleanup.
func TestSystemUsageTransactionAndReplay(t *testing.T) {
	f := newDatabaseFixture(t)
	calendar := timezone.NewCalendar(time.UTC)
	store := provideSettlementStore(f.db, calendar)
	providerRow, err := f.client.Provider.Create().SetName("system-probe").SetPlatform(provider.PlatformOpenAI).SetType(provider.ProviderTypeAPIKey).SetCredentials(map[string]any{"api_key": "fixture"}).SetExtra(map[string]any{"quota_limit": 1.0, "quota_used": 0.8, "quota_daily_limit": 1.0, "quota_daily_used": 0.8, "quota_daily_start": time.Now().UTC().Format(time.RFC3339)}).Save(t.Context())
	require.NoError(t, err)
	amount := 0.3
	row := usage.UsageLog{RequestID: uuid.NewString(), RequestSource: usage.SourceGroupProbe, ProviderID: providerRow.ID, Platform: provider.PlatformOpenAI, Model: "fixture", InputTokens: 10, OutputTokens: 3, ProviderStatsCost: &amount, RateMultiplier: 1, CreatedAt: time.Now().UTC(), SystemUsage: &usage.SystemUsage{ParentRequestID: uuid.NewString(), Outcome: "success", UsageState: "complete", CostState: "calculated"}}
	evidence, err := json.Marshal(row)
	require.NoError(t, err)
	command := billing.SystemUsageCommand{RequestID: row.RequestID, ProviderID: row.ProviderID, ProviderType: provider.ProviderTypeAPIKey, CostUSD: &amount, Evidence: evidence}
	_, err = store.ApplySystemUsage(t.Context(), command, func(context.Context, *sql.Tx) error { return errors.New("projection unavailable") })
	require.ErrorContains(t, err, "projection unavailable")
	var ledgerCount int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM billing_system_usage_entries").Scan(&ledgerCount))
	require.Zero(t, ledgerCount)
	var spent float64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT (extra->>'quota_used')::numeric FROM providers WHERE id=$1", providerRow.ID).Scan(&spent))
	require.InDelta(t, 0.8, spent, 1e-9)

	// Concurrent attempts claim the same ledger row before touching provider
	// counters. Each projection gets its own row because insertion fills its ID.
	results := make(chan bool, 8)
	failures := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			applied, applyErr := store.ApplySystemUsage(t.Context(), command, func(ctx context.Context, tx *sql.Tx) error {
				copy := row
				_, writeErr := usagepostgres.NewUsageLogRepositoryWithSQL(f.client, tx, calendar).Create(ctx, &copy)
				return writeErr
			})
			results <- applied != nil && applied.Applied
			failures <- applyErr
		})
	}
	workers.Wait()
	close(results)
	close(failures)
	for applyErr := range failures {
		require.NoError(t, applyErr)
	}
	appliedCount := 0
	for applied := range results {
		if applied {
			appliedCount++
		}
	}
	require.Equal(t, 1, appliedCount)
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT (extra->>'quota_used')::numeric FROM providers WHERE id=$1", providerRow.ID).Scan(&spent))
	require.InDelta(t, 1.1, spent, 1e-9)
	var missingCustomer bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT user_id IS NULL AND billing_user_id IS NULL AND api_key_id IS NULL AND actual_cost=0 FROM usage_logs WHERE request_id=$1", row.RequestID).Scan(&missingCustomer))
	require.True(t, missingCustomer)
	var userActivity, customerBilling int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_user_activity").Scan(&userActivity))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM billing_usage_entries").Scan(&customerBilling))
	require.Zero(t, userActivity)
	require.Zero(t, customerBilling)

	_, err = f.db.ExecContext(t.Context(), "DELETE FROM usage_logs WHERE request_id=$1", row.RequestID)
	require.NoError(t, err)
	applied, err := store.ApplySystemUsage(t.Context(), command, func(context.Context, *sql.Tx) error { return errors.New("cleaned projection must not be recreated") })
	require.NoError(t, err)
	require.False(t, applied.Applied)
	command.Evidence = json.RawMessage(`{"changed":true}`)
	_, err = store.ApplySystemUsage(t.Context(), command, func(context.Context, *sql.Tx) error { return nil })
	require.ErrorContains(t, err, "evidence conflict")
}

// TestProviderTestAssemblyRecordsSystemUsage executes a real assembled manual
// test against an isolated response transport and migrated PostgreSQL. It
// verifies the production bindings retain source and usage without customer funding.
func TestProviderTestAssemblyRecordsSystemUsage(t *testing.T) {
	f := newDatabaseFixture(t)
	calendar := timezone.NewCalendar(time.UTC)
	store := providerpostgres.NewProviderStore(f.client, f.db, providerpostgres.ProviderStoreOptions{})
	base := &nativeProviderTestTransport{body: "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":3}}}\n\n"}
	tasks := lifecycle.NewTasks()
	transport := provideSystemUsageTransport(base, store, billing.NewCalculator(nil, billing.CalculatorOptions{}), nil, provideSettlementStore(f.db, calendar), f.client, calendar, nil, tasks)
	manager := lifecycle.New()
	t.Cleanup(func() { require.NoError(t, manager.Stop(context.Background())) })
	core := provideProviderTests(store, nil, nil, nil, nil, base, &config.Config{}, nil, nil, nil, nil, manager, nil, nil, transport)
	value, err := f.client.Provider.Create().SetName("recorded-test").SetPlatform(provider.PlatformOpenAI).SetType(provider.ProviderTypeAPIKey).SetCredentials(map[string]any{"api_key": "fixture", "base_url": "https://upstream.example"}).Save(t.Context())
	require.NoError(t, err)
	kind := provider.ProviderTestTypeText
	require.NoError(t, core.Test(t.Context(), provider.TestRequest{ProviderID: value.ID, Model: "custom-model", Type: &kind}, providerhttp.NewTestEventSink(httptest.NewRecorder())))
	// Accounting is owned by the application task group after preview completion.
	// Wait for that group before inspecting committed rows or closing its database.
	require.NoError(t, tasks.Stop(t.Context()))
	var source, state string
	var input, output int
	var noCustomer bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT request_source,input_tokens,output_tokens,user_id IS NULL AND api_key_id IS NULL,system_usage->>'cost_state' FROM usage_logs WHERE provider_id=$1`, value.ID).Scan(&source, &input, &output, &noCustomer, &state))
	require.Equal(t, usage.SourceProviderTest, source)
	require.Equal(t, 8, input)
	require.Equal(t, 3, output)
	require.True(t, noCustomer)
	require.Equal(t, "unknown", state)
}
