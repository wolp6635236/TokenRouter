//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/settings/preaggregation"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// system_usage_scenario_integration_test.go compares source-filtered usage through raw
// PostgreSQL queries and rebuilt hour/day buckets. Customer and several system
// sources share model and time dimensions, so the fixture detects source merging,
// nullable attribution failures, and hidden unknown-cost requests.

// TestSystemUsageRawAndAnalyticsParity inserts customer, probe, scheduled-test,
// and unknown-cost quota rows. It verifies source filters, provider expense,
// customer visibility, parent-run lookup, and identical rebuilt aggregate totals.
func TestSystemUsageRawAndAnalyticsParity(t *testing.T) {
	client := testEntClient(t)
	calendar := timezone.NewCalendar(time.UTC)
	tx, err := integrationDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	// Derived buckets have no user foreign key. Roll back this scenario's
	// records, watermarks, and buckets before the shared fixture truncates users.
	t.Cleanup(func() { _ = tx.Rollback() })
	repo := NewUsageLogRepositoryWithSQL(client, tx, calendar)
	t.Cleanup(repo.StopUsageBatchers)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &identity.User{Email: suffix + "@system.test"})
	key := mustCreateApiKey(t, client, &apikey.APIKey{UserID: user.ID, Key: suffix, Name: "customer"})
	upstream := mustCreateProvider(t, client, &provider.Record{Name: suffix})
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	end := start.Add(24 * time.Hour)
	parent := uuid.NewString()
	for i, source := range []string{usage.SourceUser, usage.SourceGroupProbe, usage.SourceScheduledTest, usage.SourceQuotaProbe} {
		cost := float64(i+1) / 10
		row := &usage.UsageLog{RequestID: uuid.NewString(), RequestSource: source, ProviderID: upstream.ID, Platform: "openai", Model: suffix, InputTokens: 10, OutputTokens: 2, RateMultiplier: 1, TotalCost: cost, ProviderStatsCost: &cost, CreatedAt: start.Add(time.Hour)}
		if source == usage.SourceUser {
			row.UserID, row.BillingUserID, row.APIKeyID = user.ID, user.ID, key.ID
			row.ActualCost = cost
		} else {
			row.SystemUsage = &usage.SystemUsage{ParentRequestID: parent, Outcome: "success", UsageState: "complete", CostState: "calculated"}
		}
		if source == usage.SourceQuotaProbe {
			row.SystemUsage.CostState, row.SystemUsage.UsageState = "unknown", "missing"
			row.ProviderStatsCost, row.TotalCost = nil, 0
		}
		inserted, err := repo.Create(t.Context(), row)
		require.NoError(t, err)
		require.True(t, inserted)
	}
	filters := UsageLogFilters{Model: suffix, ModelFilterSource: usage.ModelSourceRequested, StartTime: &start, EndTime: &end}
	expected := map[string]struct {
		requests   int64   // matching record count
		cost       float64 // known provider cost USD
		incomplete int64   // missing or partial expense count
	}{"all": {4, 0.6, 1}, usage.SourceUser: {1, 0.1, 0}, "system": {3, 0.5, 1}, usage.SourceGroupProbe: {1, 0.2, 0}}
	for _, aggregated := range []bool{false, true} {
		if aggregated {
			// Publish coverage only after both hour and day buckets are rebuilt;
			// subsequent queries must combine those buckets without losing sources.
			aggregation := NewAggregationStoreWithSQL(tx, calendar)
			require.NoError(t, aggregation.AggregateUsageAnalyticsRange(t.Context(), start, end))
			_, err := tx.ExecContext(t.Context(), `UPDATE usage_analytics_aggregation_state SET live_watermark=$1,coverage_start=$2,source_oldest_at=$2 WHERE id=1`, end, start)
			require.NoError(t, err)
			repo.preAggregation = preaggregation.NewPreAggregationSettingsService(nil, &preaggregation.Options{Usage: preaggregation.UsageOptions{Enabled: true, IntervalSeconds: 60}})
		}
		for source, want := range expected {
			filters.RequestSource = source
			stats, err := repo.GetStatsWithFilters(t.Context(), filters)
			require.NoError(t, err)
			require.Equal(t, want.requests, stats.TotalRequests, source)
			require.Equal(t, want.incomplete, stats.IncompleteCostRequests, source)
			require.NotNil(t, stats.TotalProviderCost)
			require.InDelta(t, want.cost, *stats.TotalProviderCost, 1e-9, source)
		}
		ranking, err := repo.GetUserBreakdownStats(t.Context(), start, end, usage.UserBreakdownDimension{Model: suffix}, 10)
		require.NoError(t, err)
		require.Len(t, ranking, 1)
		require.Equal(t, user.ID, ranking[0].UserID)
		require.EqualValues(t, 1, ranking[0].Requests)
	}
	public := &usage.DashboardPublicStats{}
	require.NoError(t, repo.fillDashboardPublicTokenStatsFromUsageLogs(t.Context(), public, start, end, start))
	require.EqualValues(t, 12, public.TotalTokens)
	require.EqualValues(t, 12, public.TodayTokens)
	// Legacy dashboard buckets keep customer traffic separate from total
	// provider expense. System rows must not create nullable user memberships.
	legacy := NewAggregationStoreWithSQL(tx, calendar)
	require.NoError(t, legacy.AggregateRange(t.Context(), start, end))
	var requests, activeUsers int64
	var providerCost float64
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT total_requests,provider_cost,active_users FROM usage_dashboard_hourly WHERE bucket_start=$1`, start.Add(time.Hour)).Scan(&requests, &providerCost, &activeUsers))
	require.Equal(t, int64(1), requests)
	require.Equal(t, int64(1), activeUsers)
	require.InDelta(t, 0.6, providerCost, 1e-9)
	filters.RequestSource, filters.UserID = "all", user.ID
	rows, _, err := repo.ListWithFilters(t.Context(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, usage.SourceUser, rows[0].RequestSource)
	filters.UserID, filters.RequestID = 0, parent
	rows, _, err = repo.ListWithFilters(t.Context(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Zero(t, row.UserID)
		require.Zero(t, row.APIKeyID)
	}
}
