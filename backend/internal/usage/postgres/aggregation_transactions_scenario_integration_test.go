//go:build integration

package postgres

// 本场景覆盖 usage_cleanup_repo.go、dashboard_aggregation_repo.go
// 和 usage_analytics_aggregation_repo.go，并检查审计清理的事务回滚。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/audit"
	auditpg "github.com/TokenFlux/TokenRouter/internal/audit/postgres"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

type pausedState struct {
	*AggregationStore
	reads            atomic.Int32
	entered, release chan struct{}
}

type canceledCleanup struct {
	usage.UsageCleanupRepository
	taskID, operator int64
	observed         chan struct{}
	once             sync.Once
}

type repairStore struct {
	*AggregationStore
	done chan error
}

func TestAuditClearTraceFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	repo := auditpg.NewAuditLogRepository(integrationDB)
	s := audit.NewAuditLogService(repo, nil)
	defer s.Stop()
	_, err := integrationDB.ExecContext(ctx, "TRUNCATE audit_logs")
	require.NoError(t, err)
	require.NoError(t, repo.Insert(ctx, &audit.AuditLog{Action: "test-original", CreatedAt: time.Now()}))
	_, err = integrationDB.ExecContext(ctx, `CREATE FUNCTION test_reject_clear_trace() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='admin.audit_log.clear' THEN RAISE EXCEPTION 'test trace failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_reject_clear_trace BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION test_reject_clear_trace()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, e := integrationDB.ExecContext(ctx, `DROP TRIGGER test_reject_clear_trace ON audit_logs; DROP FUNCTION test_reject_clear_trace(); TRUNCATE audit_logs`)
		require.NoError(t, e)
	})
	_, err = s.ClearAll(ctx, &audit.AuditLog{})
	require.Error(t, err)
	count, err := repo.Count(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	_, err = integrationDB.ExecContext(ctx, "DROP TRIGGER test_reject_clear_trace ON audit_logs")
	require.NoError(t, err)
	// 清理成功后保存一条审计记录，删除数量取自同一事务。
	deleted, err := s.ClearAll(ctx, &audit.AuditLog{})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	var action string
	var deletedRows int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT action,(extra->>'deleted_rows')::bigint FROM audit_logs").Scan(&action, &deletedRows))
	require.Equal(t, audit.AuditActionAuditLogClear, action)
	require.Equal(t, int64(1), deletedRows)
	// 重新创建触发器，供测试结束时的清理函数删除。
	_, err = integrationDB.ExecContext(ctx, `CREATE TRIGGER test_reject_clear_trace BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION test_reject_clear_trace()`)
	require.NoError(t, err)
}

func TestManualBackfillPreservesConcurrentState(t *testing.T) {
	ctx := context.Background()
	base := NewAggregationStoreWithSQL(integrationDB, timezone.NewCalendar(time.Local))
	saved, err := base.GetUsageAnalyticsAggregationState(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.SaveUsageAnalyticsAggregationState(ctx, saved)) })
	old := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	next := old.Add(time.Hour)
	require.NoError(t, base.SaveUsageAnalyticsAggregationState(ctx, &usage.UsageAnalyticsAggregationState{LiveWatermark: old, Phase: "idle"}))
	r := &pausedState{AggregationStore: base, entered: make(chan struct{}), release: make(chan struct{})}
	s := usage.NewDashboardAggregationService(r, nil, &usage.Options{DashboardAgg: usage.DashboardAggregationConfig{BackfillEnabled: true}})
	s.SetPreAggregationSettings(nil)
	defer s.Stop()
	done := make(chan error, 1)
	go func() { done <- s.TriggerBackfill(old.Add(-time.Hour), old) }()
	<-r.entered
	_, err = base.ApplyUsageAnalyticsState(ctx, usage.AnalyticsStateChange{Kind: usage.AnalyticsLiveSuccess, State: usage.UsageAnalyticsAggregationState{LiveWatermark: next, Phase: "idle"}})
	require.NoError(t, err)
	close(r.release)
	require.NoError(t, <-done)
	got, err := base.GetUsageAnalyticsAggregationState(ctx)
	require.NoError(t, err)
	require.True(t, got.LiveWatermark.Equal(next))
	require.NotNil(t, got.ManualBackfillStart)
	oldTarget, oldCursor := got.ManualBackfillStart, got.ManualBackfillCursor
	newTarget, newCursor := old.Add(-4*time.Hour), next
	_, err = base.ApplyUsageAnalyticsState(ctx, usage.AnalyticsStateChange{Kind: usage.AnalyticsManualRequest, State: usage.UsageAnalyticsAggregationState{ManualBackfillStart: &newTarget, ManualBackfillCursor: &newCursor}})
	require.NoError(t, err)
	// 已过期工作器不能清除刚提交的新手工目标。
	_, err = base.ApplyUsageAnalyticsState(ctx, usage.AnalyticsStateChange{Kind: usage.AnalyticsManualProgress, State: usage.UsageAnalyticsAggregationState{Phase: "idle"}, ExpectedManualStart: oldTarget, ExpectedManualCursor: oldCursor})
	require.ErrorIs(t, err, usage.ErrAnalyticsRequestSuperseded)
	got, err = base.GetUsageAnalyticsAggregationState(ctx)
	require.NoError(t, err)
	require.True(t, got.ManualBackfillStart.Equal(newTarget))
	require.True(t, got.LiveWatermark.Equal(next))
	// 逆序写入时，实时快照更新实时字段，手工请求保持当前值。
	_, err = base.ApplyUsageAnalyticsState(ctx, usage.AnalyticsStateChange{Kind: usage.AnalyticsLiveSuccess, State: usage.UsageAnalyticsAggregationState{LiveWatermark: next.Add(time.Hour), Phase: "idle"}})
	require.NoError(t, err)
	got, err = base.GetUsageAnalyticsAggregationState(ctx)
	require.NoError(t, err)
	require.True(t, got.ManualBackfillStart.Equal(newTarget))
	require.True(t, got.ManualBackfillCursor.Equal(newCursor))
}

func TestCanceledPartialCleanupRepairsCommittedData(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	u := mustCreateUser(t, client, &identity.User{Email: "test-cancel@test.local", Balance: 7})
	key := mustCreateApiKey(t, client, &apikey.APIKey{UserID: u.ID, Key: "sk-test-cancel", Name: "k"})
	provider := mustCreateProvider(t, client, &providercore.Record{Name: "test-cancel"})
	repo := NewUsageLogRepositoryWithSQL(client, integrationDB, timezone.NewCalendar(time.Local))
	defer repo.StopUsageBatchers()
	now := time.Now().UTC().Add(-72 * time.Hour)
	for range 2 {
		_, e := repo.Create(ctx, &usage.UsageLog{UserID: u.ID, APIKeyID: key.ID, ProviderID: provider.ID, Model: "lifecycle-test", TotalCost: 1, ActualCost: 1, CreatedAt: now})
		require.NoError(t, e)
	}
	base := NewAggregationStoreWithSQL(integrationDB, timezone.NewCalendar(time.Local))
	require.NoError(t, base.AggregateUsageAnalyticsRange(ctx, now.Add(-time.Hour), now.Add(time.Hour)))
	ar := &repairStore{AggregationStore: base, done: make(chan error, 1)}
	agg := usage.NewDashboardAggregationService(ar, nil, nil)
	agg.SetPreAggregationSettings(nil)
	agg.Start()
	defer agg.Stop()
	cr := &canceledCleanup{UsageCleanupRepository: NewUsageCleanupRepository(client, integrationDB), operator: u.ID, observed: make(chan struct{})}
	cleanup := usage.NewUsageCleanupService(cr, nil, agg, &usage.Options{UsageCleanup: usage.UsageCleanupConfig{Enabled: true, BatchSize: 1}})
	task, e := cleanup.CreateTask(ctx, usage.UsageCleanupFilters{StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour), UserID: &u.ID}, u.ID)
	require.NoError(t, e)
	select {
	case <-cr.observed:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel not observed")
	}
	cleanup.Stop()
	select {
	case e := <-ar.done:
		require.NoError(t, e)
	case <-time.After(5 * time.Second):
		t.Fatal("aggregate repair did not finish independently of canceled cleanup")
	}
	var raw, total int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE user_id=$1", u.ID).Scan(&raw))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_requests),0) FROM usage_analytics_hourly WHERE user_id=$1", u.ID).Scan(&total))
	require.Equal(t, int64(1), raw)
	require.Equal(t, raw, total)
	status, e := cr.UsageCleanupRepository.GetTaskStatus(ctx, task.ID)
	require.NoError(t, e)
	require.Equal(t, usage.UsageCleanupStatusCanceled, status)
	balance, e := client.User.Get(ctx, u.ID)
	require.NoError(t, e)
	require.Equal(t, float64(7), balance.Balance, "deleting analytics must not refund money")
}

func (r *pausedState) GetUsageAnalyticsAggregationState(ctx context.Context) (*usage.UsageAnalyticsAggregationState, error) {
	v, e := r.AggregationStore.GetUsageAnalyticsAggregationState(ctx)
	if r.reads.Add(1) == 1 {
		close(r.entered)
		<-r.release
	}
	return v, e
}

func (r *canceledCleanup) CreateTask(ctx context.Context, t *usage.UsageCleanupTask) error {
	e := r.UsageCleanupRepository.CreateTask(ctx, t)
	r.taskID = t.ID
	return e
}

func (r *canceledCleanup) DeleteUsageLogsBatch(ctx context.Context, f usage.UsageCleanupFilters, n int) (int64, error) {
	d, e := r.UsageCleanupRepository.DeleteUsageLogsBatch(ctx, f, n)
	if e == nil && d > 0 {
		_, e = r.CancelTask(ctx, r.taskID, r.operator)
	}
	return d, e
}

func (r *canceledCleanup) GetTaskStatus(ctx context.Context, id int64) (string, error) {
	v, e := r.UsageCleanupRepository.GetTaskStatus(ctx, id)
	if v == usage.UsageCleanupStatusCanceled {
		r.once.Do(func() { close(r.observed) })
	}
	return v, e
}

func (r *repairStore) RecomputeUsageAnalyticsRange(ctx context.Context, start, end time.Time) error {
	e := r.AggregationStore.RecomputeUsageAnalyticsRange(ctx, start, end)
	r.done <- e
	return e
}
