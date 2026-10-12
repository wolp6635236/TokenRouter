package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	billingpg "github.com/TokenFlux/TokenRouter/internal/billing/postgres"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
)

func newDashboardAggregationRepositoryWithSQL(q sqlExecutor) *AggregationStore {
	return NewAggregationStoreWithSQL(q, timezone.NewCalendar(time.Local), func(ctx context.Context, t time.Time) error { return billingpg.ArchiveUsageDedup(ctx, q, t) })
}

func newSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, m, e := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, e)
	t.Cleanup(func() { _ = db.Close() })
	return db, m
}
