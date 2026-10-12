package provider_test

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
	egresspostgres "github.com/TokenFlux/TokenRouter/internal/egress/postgres"
	postgresinfra "github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
	routingpostgres "github.com/TokenFlux/TokenRouter/internal/routing/postgres"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	schedulerpostgres "github.com/TokenFlux/TokenRouter/internal/scheduler/postgres"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"
)

const (
	// deprecatedUpstreamBillingProbeExtraKey 是历史上游计费探测快照键。
	deprecatedUpstreamBillingProbeExtraKey = "upstream_billing_probe"

	// deprecatedUpstreamBillingProbeEnabledExtraKey 是历史上游计费探测开关键。
	deprecatedUpstreamBillingProbeEnabledExtraKey = "upstream_billing_probe_enabled"
)

// captureEntQueryMatcher 记录 Ent 发出的 SQL。
type captureEntQueryMatcher struct {
	actual *string
}

// captureQuerySQL 在执行查询时记录 SQL 和参数。
type captureQuerySQL struct {
	db       *sql.DB
	captured *string
	args     *[]any
}

// rowsAffectedResult 提供固定影响行数的 SQL 执行结果。
type rowsAffectedResult int64

// recordingSQLExecutor 记录写入 SQL，并模拟错误和执行后的回调。
type recordingSQLExecutor struct {
	result      sql.Result
	err         error
	afterExec   func()
	execQueries []string
	execArgs    [][]any
}

// providerEventsFixture 将提供商事件写入调度 outbox 并发布快照。
type providerEventsFixture struct{ publisher scheduler.SnapshotPublisher }

// Match 保存待断言的查询文本。
func (m captureEntQueryMatcher) Match(_, actual string) error {
	if m.actual == nil {
		return fmt.Errorf("query capture target is nil")
	}
	*m.actual = actual
	return nil
}

// newProxyStoreContract 将代理变更与提供商快照清理绑定到同一个事务。
func newProxyStoreContract(client *dbent.Client, exec postgresinfra.Executor) *egresspostgres.ProxyStore {
	return egresspostgres.NewProxyStore(client, exec, egresspostgres.ProxyStoreOptions{
		Providers: func(tx postgresinfra.Executor) egresspostgres.ProxyProviderParticipant {
			return providerpostgres.ProxyChangesInTx(tx)
		},
		Enqueue: func(ctx context.Context, tx postgresinfra.Executor, payload any) error {
			return schedulerpostgres.EnqueueSchedulerChange(ctx, tx, scheduler.SchedulerOutboxEventProviderBulkChanged, nil, nil, payload)
		},
	})
}

// ExecContext 将写操作交给数据库执行。
func (c captureQuerySQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, query, args...)
}

// QueryContext 记录查询文本和参数后执行查询。
func (c captureQuerySQL) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if c.captured != nil {
		*c.captured = query
	}
	if c.args != nil {
		*c.args = append([]any(nil), args...)
	}
	return c.db.QueryContext(ctx, query, args...)
}

// normalizeSQLWhitespace 将连续空白压缩为一个空格。
func normalizeSQLWhitespace(sql string) string {
	return strings.Join(regexp.MustCompile(`\s+`).Split(strings.TrimSpace(sql), -1), " ")
}

// LastInsertId 返回测试占位值。
func (r rowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }

// RowsAffected 返回指定的影响行数。
func (r rowsAffectedResult) RowsAffected() (int64, error) { return int64(r), nil }

// ExecContext 记录写操作并返回配置的结果。
func (e *recordingSQLExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	e.execQueries = append(e.execQueries, query)
	e.execArgs = append(e.execArgs, append([]any(nil), args...))
	if e.err != nil {
		return nil, e.err
	}
	if e.afterExec != nil {
		e.afterExec()
	}
	return e.result, nil
}

// QueryContext 返回无结果错误。
func (e *recordingSQLExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, sql.ErrNoRows
}

// newProviderStoreContract 绑定提供商存储的 outbox 写入器和传入的调度缓存。
func newProviderStoreContract(client *dbent.Client, exec postgresinfra.Executor, cache scheduler.SnapshotPublicationCache) *providerpostgres.ProviderStore {
	store := providerpostgres.NewProviderStore(client, exec, providerpostgres.ProviderStoreOptions{
		Group: func(g *dbent.Group) *accessview.GroupConfig {
			return (*accessview.GroupConfig)(routingpostgres.GroupFromEnt(g))
		},
		OllamaIdentity: provider.IsOllamaCloudUsageProvider,
		Proxy:          egresspostgres.ProxyEntity,
		Events:         providerEventsFixture{},
	})
	store.SetEvents(providerPublicationEvents(store, cache))
	return store
}

// Name 将提供商事件转换为调度 outbox 事件名。
func (providerEventsFixture) Name(event providerpostgres.ProviderEvent) string {
	switch event {
	case providerpostgres.ProviderChanged:
		return scheduler.SchedulerOutboxEventProviderChanged
	case providerpostgres.ProviderGroupsChanged:
		return scheduler.SchedulerOutboxEventProviderGroupsChanged
	case providerpostgres.ProviderLastUsed:
		return scheduler.SchedulerOutboxEventProviderLastUsed
	case providerpostgres.ProviderBulkChanged:
		return scheduler.SchedulerOutboxEventProviderBulkChanged
	default:
		panic("未知提供商事件")
	}
}

// Write 使用传入事务写入调度事件。
func (f providerEventsFixture) Write(ctx context.Context, exec postgresinfra.Executor, event providerpostgres.ProviderEvent, id, group *int64, payload any) error {
	return schedulerpostgres.EnqueueSchedulerChange(ctx, exec, f.Name(event), id, group, payload)
}

// GroupPayload 构造分组事件的数据。
func (providerEventsFixture) GroupPayload(ids []int64) any { return scheduler.GroupPayload(ids) }

// SyncOne 发布指定提供商的快照。
func (f providerEventsFixture) SyncOne(ctx context.Context, id int64) { f.publisher.Publish(ctx, id) }

// SyncMany 批量发布提供商快照。
func (f providerEventsFixture) SyncMany(ctx context.Context, ids []int64) {
	f.publisher.PublishMany(ctx, ids)
}

// Drop 删除提供商快照。
func (f providerEventsFixture) Drop(ctx context.Context, id int64) { f.publisher.Drop(ctx, id) }

// providerPublicationEvents 将提供商存储的 outbox 连接到调度快照发布器。
func providerPublicationEvents(store *providerpostgres.ProviderStore, cache scheduler.SnapshotPublicationCache) providerEventsFixture {
	return providerEventsFixture{publisher: scheduler.SnapshotPublisher{
		Cache: cache,
		Read: func(ctx context.Context, id int64) (scheduler.SnapshotProvider, error) {
			v, err := store.GetByID(ctx, id)
			return codec.WrapRecord(v), err
		},
		ReadMany: func(ctx context.Context, ids []int64) ([]scheduler.SnapshotProvider, error) {
			values, err := store.GetByIDs(ctx, ids)
			if values == nil {
				return nil, err
			}
			out := make([]scheduler.SnapshotProvider, len(values))
			for i, value := range values {
				out[i] = codec.WrapRecord(value)
			}
			return out, err
		},
	}}
}
