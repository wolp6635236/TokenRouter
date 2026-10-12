//go:build integration

package routing_test

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
	identitypostgres "github.com/TokenFlux/TokenRouter/internal/identity/postgres"
	postgresinfra "github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	routingpostgres "github.com/TokenFlux/TokenRouter/internal/routing/postgres"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	schedulerpostgres "github.com/TokenFlux/TokenRouter/internal/scheduler/postgres"
	"github.com/TokenFlux/TokenRouter/internal/testutil/postgrescontainer"
)

// routingDatabase 创建测试数据库和 Ent 客户端，数据库夹具在测试结束时关闭连接。
func routingDatabase(t *testing.T) (*dbent.Client, *sql.DB) {
	t.Helper()
	db := postgrescontainer.New(t)
	return dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db))), db
}

// newGroupStoreFixture 为分组存储配置提供商关联、用户权限删除和调度通知操作。
func newGroupStoreFixture(client *dbent.Client, db postgresinfra.Executor) *routingpostgres.GroupStore {
	return routingpostgres.NewGroupStore(client, db, routingpostgres.GroupStoreOptions{
		Providers: func(exec postgresinfra.Executor) routingpostgres.GroupLinkParticipant {
			return providerpostgres.GroupLinksInTx(exec)
		},
		Users: func(exec postgresinfra.Executor) routingpostgres.GroupAccessParticipant {
			return identitypostgres.GroupAccessDeletionInTx(exec)
		},
		Enqueue: func(ctx context.Context, exec postgresinfra.Executor, id *int64) error {
			return schedulerpostgres.EnqueueSchedulerChange(ctx, exec, scheduler.SchedulerOutboxEventGroupChanged, nil, id, nil)
		},
	})
}
