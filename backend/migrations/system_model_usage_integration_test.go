//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	"github.com/TokenFlux/TokenRouter/migrations"
)

// system_model_usage_integration_test.go upgrades historical customer rows with
// migration 304 and checks its write constraints and index eligibility. Minimal
// pre-migration tables isolate heap preservation, source defaults, and generic
// query plans from unrelated application schema changes.

// TestSystemModelUsageMigration preserves old rows without rewriting their heap.
// New writes remain constrained, and generic prepared queries can use both
// partial lookup indexes while source-aware rollup keys preserve old totals.
func TestSystemModelUsageMigration(t *testing.T) {
	ctx := t.Context()
	pg, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23", tcpostgres.WithDatabase("system_usage"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("fixture"), tcpostgres.BasicWaitStrategies())
	// Register cleanup even after partial startup; cancellation of assertions
	// must not leave a Docker database running beyond this migration test.
	if pg != nil {
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, pg.Terminate(cleanupCtx))
		})
	}
	require.NoError(t, err)
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.ExecContext(ctx, `CREATE TABLE usage_logs (
 id bigint, request_id varchar(64), user_id bigint NOT NULL, billing_user_id bigint NOT NULL,
 api_key_id bigint NOT NULL, team_id bigint, subscription_id bigint,
 actual_cost numeric NOT NULL DEFAULT 0, subscription_amount_usd numeric NOT NULL DEFAULT 0,
 balance_amount_usd numeric NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT now());
 INSERT INTO usage_logs(id,request_id,user_id,billing_user_id,api_key_id,actual_cost)
 SELECT id,id::text,1,1,1,0.123 FROM generate_series(1,1000) id;`)
	require.NoError(t, err)
	for _, table := range []string{"usage_analytics_hourly", "usage_analytics_daily"} {
		bucket := "bucket_start"
		if strings.HasSuffix(table, "daily") {
			bucket = "bucket_date"
		}
		_, err = db.ExecContext(ctx, `CREATE TABLE `+table+` (`+bucket+` timestamptz NOT NULL DEFAULT '2026-01-01',
 user_id bigint NOT NULL DEFAULT 1, billing_user_id bigint NOT NULL DEFAULT 1,
 team_id bigint NOT NULL DEFAULT 0, api_key_id bigint NOT NULL DEFAULT 1, group_id bigint NOT NULL DEFAULT 0,
 requested_model text NOT NULL DEFAULT 'test', request_type smallint NOT NULL DEFAULT 0,
 stream boolean NOT NULL DEFAULT false, billing_type smallint NOT NULL DEFAULT 0,
 billing_mode text NOT NULL DEFAULT '', platform text NOT NULL DEFAULT 'openai', inbound_endpoint text NOT NULL DEFAULT '',
 total_requests bigint NOT NULL DEFAULT 7,
 PRIMARY KEY (`+bucket+`,user_id,billing_user_id,team_id,api_key_id,group_id,requested_model,request_type,stream,billing_type,billing_mode,platform,inbound_endpoint));
 INSERT INTO `+table+` DEFAULT VALUES;`)
		require.NoError(t, err)
	}
	var before, after int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_relation_filenode('usage_logs')`).Scan(&before))
	content, err := migrations.FS.ReadFile("304_system_model_usage.sql")
	require.NoError(t, err)
	fs := fstest.MapFS{"304_system_model_usage.sql": &fstest.MapFile{Data: content}}
	require.NoError(t, postgres.ApplyMigrations(ctx, db, fs))
	require.NoError(t, postgres.ApplyMigrations(ctx, db, fs))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT pg_relation_filenode('usage_logs')`).Scan(&after))
	require.Equal(t, before, after)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM usage_logs WHERE request_source='user' AND system_usage IS NULL AND actual_cost=0.123`).Scan(&count))
	require.Equal(t, 1000, count)
	var validated bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT convalidated FROM pg_constraint WHERE conname='usage_logs_source_attribution_check'`).Scan(&validated))
	require.False(t, validated)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_index WHERE indrelid='usage_logs'::regclass AND indpred IS NOT NULL`).Scan(&count))
	require.Equal(t, 3, count)
	_, err = db.ExecContext(ctx, `UPDATE usage_logs SET billing_user_id=NULL WHERE id=1`)
	require.ErrorContains(t, err, "usage_logs_source_attribution_check")
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(request_id) VALUES ('invalid-user')`)
	require.ErrorContains(t, err, "usage_logs_source_attribution_check")
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(request_id,request_source,system_usage,actual_cost) VALUES ('invalid-system','group_probe','{}',1)`)
	require.ErrorContains(t, err, "usage_logs_source_attribution_check")
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(request_id,request_source,system_usage) VALUES ('system','group_probe','{"parent_request_id":"parent"}')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(request_id,request_source,system_usage) VALUES ('system','group_probe','{}')`)
	require.ErrorContains(t, err, "idx_usage_logs_system_request")
	for _, table := range []string{"usage_analytics_hourly", "usage_analytics_daily"} {
		_, err = db.ExecContext(ctx, `INSERT INTO `+table+` (request_source) VALUES ('group_probe')`)
		require.NoError(t, err)
		require.NoError(t, db.QueryRowContext(ctx, `SELECT total_requests FROM `+table+` WHERE request_source='user'`).Scan(&count))
		require.Equal(t, 7, count)
	}
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	// One connection keeps planner settings and prepared statements together.
	// Disabling sequential scans checks eligibility, not production cost estimates.
	_, err = conn.ExecContext(ctx, `SET enable_seqscan=off; SET plan_cache_mode=force_generic_plan;
 PREPARE source_lookup(text) AS SELECT request_id FROM usage_logs WHERE request_source <> 'user' AND request_source=$1 ORDER BY created_at DESC;
 PREPARE parent_lookup(text) AS SELECT request_id FROM usage_logs WHERE system_usage IS NOT NULL AND system_usage->>'parent_request_id'=$1;`)
	require.NoError(t, err)
	for statement, index := range map[string]string{"source_lookup('group_probe')": "idx_usage_logs_source_created", "parent_lookup('parent')": "idx_usage_logs_system_parent"} {
		var plan string
		require.NoError(t, conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE "+statement).Scan(&plan))
		require.Contains(t, plan, index)
	}
}
