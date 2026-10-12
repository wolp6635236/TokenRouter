//go:build integration

package promotion_test

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
)

// testStore 创建独立测试数据库和 Ent 客户端。
func testStore(t *testing.T) (*dbent.Client, *sql.DB) {
	t.Helper()
	db := databaseSuite.New(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client, db
}

// testEntClient 为需要提交事务的测试提供独立数据库。
func testEntClient(t *testing.T) *dbent.Client {
	t.Helper()
	client, _ := testStore(t)
	return client
}
