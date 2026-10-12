package provider_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const parameterLimitTestDriverName = "tokenrouter_param_limit_test"

var registerParameterLimitTestDriverOnce sync.Once

type parameterLimitDriver struct{}

type parameterLimitConn struct{}

type parameterLimitStmt struct {
	query string
}

type parameterLimitTx struct{}

type parameterLimitRows struct {
	columns []string
}

func TestProvidersToService_LargeActiveProviderSetDoesNotExceedPostgresParameterLimit(t *testing.T) {
	repo := newParameterLimitProviderRepo(t)

	providers := make([]*dbent.Provider, 0, 65536)
	for i := range 65536 {
		providers = append(providers, &dbent.Provider{
			ID:          int64(i + 1),
			Name:        "large-active",
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Credentials: map[string]any{},
			Extra:       map[string]any{},
			Status:      billing.StatusActive,
			Schedulable: true,
		})
	}

	got, err := repo.RecordsFromEntities(context.Background(), providers)
	require.NoError(t, err)
	require.Len(t, got, len(providers))
}

func newParameterLimitProviderRepo(t *testing.T) *providerpostgres.ProviderStore {
	t.Helper()

	registerParameterLimitTestDriverOnce.Do(func() {
		sql.Register(parameterLimitTestDriverName, parameterLimitDriver{})
	})

	db, err := sql.Open(parameterLimitTestDriverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	return newProviderStoreContract(client, nil, nil)
}

func (parameterLimitDriver) Open(string) (driver.Conn, error) {
	return parameterLimitConn{}, nil
}

func (parameterLimitConn) Prepare(query string) (driver.Stmt, error) {
	return parameterLimitStmt{query: query}, nil
}

func (parameterLimitConn) Close() error {
	return nil
}

func (parameterLimitConn) Begin() (driver.Tx, error) {
	return parameterLimitTx{}, nil
}

func (parameterLimitConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return queryWithParameterLimit(query, args)
}

func (s parameterLimitStmt) Close() error {
	return nil
}

func (s parameterLimitStmt) NumInput() int {
	return -1
}

func (s parameterLimitStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), parameterLimitError(len(args))
}

func (s parameterLimitStmt) Query(args []driver.Value) (driver.Rows, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		namedArgs[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return queryWithParameterLimit(s.query, namedArgs)
}

func (parameterLimitTx) Commit() error {
	return nil
}

func (parameterLimitTx) Rollback() error {
	return nil
}

func queryWithParameterLimit(query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := parameterLimitError(len(args)); err != nil {
		return nil, err
	}
	return parameterLimitRows{columns: columnsForParameterLimitQuery(query)}, nil
}

func parameterLimitError(paramCount int) error {
	if paramCount <= 65535 {
		return nil
	}
	return fmt.Errorf("pq: got %d parameters but PostgreSQL only supports 65535 parameters", paramCount)
}

func columnsForParameterLimitQuery(query string) []string {
	if query == "" {
		return nil
	}
	return []string{"provider_id", "group_id", "priority", "created_at"}
}

func (r parameterLimitRows) Columns() []string {
	return r.columns
}

func (parameterLimitRows) Close() error {
	return nil
}

func (parameterLimitRows) Next([]driver.Value) error {
	return io.EOF
}
