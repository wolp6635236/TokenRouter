//go:build integration

package identity_test

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
)

// identityDatabase 身份合同保留真实提交；使用隔离数据库避免共享提供商数据。
func identityDatabase(t *testing.T) (*sql.DB, *dbent.Client) {
	t.Helper()
	db := databaseSuite.New(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return db, client
}
