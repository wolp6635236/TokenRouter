//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// TestResponseModelBatchRoundTrip 使用 PostgreSQL 检查批量 SQL 的列和参数顺序。
func TestResponseModelBatchRoundTrip(t *testing.T) {
	ctx := context.Background()
	// 批量写入会提交到共享数据库，测试结束后由夹具清理数据。
	client := testEntClient(t)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &identity.User{Email: suffix + "@response-model.test"})
	key := mustCreateApiKey(t, client, &apikey.APIKey{UserID: user.ID, Key: suffix, Name: "model"})
	upstream := mustCreateProvider(t, client, &providercore.Record{Name: suffix})
	repo := &Store{}
	keys := []string{}
	prepared := map[string]usageLogInsertPrepared{}
	for _, kind := range []string{"unknown", "same", "different"} {
		log := &usage.UsageLog{UserID: user.ID, APIKeyID: key.ID, ProviderID: upstream.ID, RequestID: suffix + kind, Model: "sent", CreatedAt: time.Now()}
		if kind != "unknown" {
			value := kind
			mismatch := kind == "different"
			log.UpstreamResponseModel = &value
			log.UpstreamModelMismatch = &mismatch
		}
		batchKey := usageLogBatchKey(log.RequestID, log.APIKeyID)
		keys = append(keys, batchKey)
		prepared[batchKey] = prepareUsageLogInsert(log)
	}
	_, states, _, err := repo.batchInsertUsageLogs(integrationDB, keys, prepared)
	require.NoError(t, err)
	for _, key := range keys {
		state := states[key]
		log, err := scanUsageLog(integrationDB.QueryRowContext(ctx, "SELECT "+usageLogSelectColumns+" FROM usage_logs WHERE id=$1", state.ID))
		require.NoError(t, err)
		want := prepared[key].args
		require.Equal(t, want[len(want)-3], nullString(log.UpstreamResponseModel))
		require.Equal(t, want[len(want)-2], log.UpstreamModelMismatch)
	}
	// 尽力批量和单条降级写入都需要传递响应模型的两列。
	for _, path := range []string{"best-effort", "fallback"} {
		model, mismatch := "runtime-version", true
		log := &usage.UsageLog{UserID: user.ID, APIKeyID: key.ID, ProviderID: upstream.ID, RequestID: suffix + path, Model: "sent", CreatedAt: time.Now(), UpstreamResponseModel: &model, UpstreamModelMismatch: &mismatch}
		value := prepareUsageLogInsert(log)
		if path == "best-effort" {
			query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{value})
			_, err = integrationDB.ExecContext(ctx, query, args...)
		} else {
			err = execUsageLogInsertNoResult(ctx, integrationDB, value)
		}
		require.NoError(t, err)
		loaded, err := scanUsageLog(integrationDB.QueryRowContext(ctx, "SELECT "+usageLogSelectColumns+" FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", log.RequestID, key.ID))
		require.NoError(t, err)
		require.Equal(t, log.UpstreamResponseModel, loaded.UpstreamResponseModel)
		require.Equal(t, log.UpstreamModelMismatch, loaded.UpstreamModelMismatch)
	}
	// 重放时使用已保存的响应模型声明。
	_, _, _, err = repo.batchInsertUsageLogs(integrationDB, keys, prepared)
	require.NoError(t, err)
}

// TestUsageLog_SessionIDPersistence 验证 session_id 能完成插入与读取回环，
// 缺失时则保持为 NULL。
func TestUsageLog_SessionIDPersistence(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageLogRepositoryWithSQL(client, integrationDB, timezone.NewCalendar(time.Local))

	user := mustCreateUser(t, client, &identity.User{Email: "session-id-" + uuid.NewString() + "@example.com"})
	apiKey := mustCreateApiKey(t, client, &apikey.APIKey{UserID: user.ID, Key: "sk-session-" + uuid.NewString(), Name: "k"})
	provider := mustCreateProvider(t, client, &providercore.Record{Name: "acc-session-" + uuid.NewString()})

	sessionID := "sess-" + uuid.NewString()

	withSession := &usage.UsageLog{
		UserID:       user.ID,
		APIKeyID:     apiKey.ID,
		ProviderID:   provider.ID,
		RequestID:    uuid.NewString(),
		Model:        "claude-3",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.0,
		ActualCost:   1.0,
		SessionID:    &sessionID,
		CreatedAt:    time.Now().UTC(),
	}
	_, err := repo.Create(ctx, withSession)
	require.NoError(t, err)
	require.NotZero(t, withSession.ID)

	withoutSession := &usage.UsageLog{
		UserID:       user.ID,
		APIKeyID:     apiKey.ID,
		ProviderID:   provider.ID,
		RequestID:    uuid.NewString(),
		Model:        "claude-3",
		InputTokens:  7,
		OutputTokens: 3,
		TotalCost:    0.5,
		ActualCost:   0.5,
		CreatedAt:    time.Now().UTC(),
	}
	_, err = repo.Create(ctx, withoutSession)
	require.NoError(t, err)

	// 插入后读回相同的会话标识。
	got, err := repo.GetByID(ctx, withSession.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SessionID)
	require.Equal(t, sessionID, *got.SessionID)

	// 缺失的会话标识读回 nil（NULL）。
	gotNone, err := repo.GetByID(ctx, withoutSession.ID)
	require.NoError(t, err)
	require.Nil(t, gotNone.SessionID)
}

// BenchmarkUserActivityWrites 使用完整迁移和生产批量写入 SQL 比较活动汇总的开销。
func BenchmarkUserActivityWrites(b *testing.B) {
	benchmarkUserActivityWrites(b, false)
}

// BenchmarkUserActivityConcurrentWrites 比较并发连接写入同用户及多个用户的吞吐。
func BenchmarkUserActivityConcurrentWrites(b *testing.B) {
	benchmarkUserActivityWrites(b, true)
}

// benchmarkUserActivityWrites 在每个子基准结束时恢复触发器并清理夹具。
func benchmarkUserActivityWrites(b *testing.B, concurrent bool) {
	ctx := context.Background()
	client := integrationEntClient
	for _, spread := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("spread=%t/activity=%t", spread, enabled), func(b *testing.B) {
				mode := "DISABLE"
				if enabled {
					mode = "ENABLE"
				}
				_, err := integrationDB.Exec("ALTER TABLE usage_logs " + mode + " TRIGGER usage_activity_insert")
				require.NoError(b, err)
				b.Cleanup(func() {
					_, e := integrationDB.Exec("ALTER TABLE usage_logs ENABLE TRIGGER usage_activity_insert; TRUNCATE users CASCADE")
					require.NoError(b, e)
				})
				p, err := client.Provider.Create().SetName("activity-bench").SetPlatform("openai").SetType("apikey").SetCredentials(map[string]any{}).Save(ctx)
				require.NoError(b, err)
				userIDs, keyIDs := make([]int64, 64), make([]int64, 64)
				for i := range userIDs {
					if i > 0 && !spread {
						userIDs[i], keyIDs[i] = userIDs[0], keyIDs[0]
						continue
					}
					u, e := client.User.Create().SetEmail(fmt.Sprintf("%d@activity-bench.test", i)).SetPasswordHash("hash").Save(ctx)
					require.NoError(b, e)
					k, e := client.APIKey.Create().SetUserID(u.ID).SetKey(fmt.Sprintf("activity-bench-%d", i)).SetName("bench").Save(ctx)
					require.NoError(b, e)
					userIDs[i], keyIDs[i] = u.ID, k.ID
				}
				repo := &Store{}
				sequence := new(atomic.Int64)
				write := func() {
					current := sequence.Add(1)
					keys := make([]string, 64)
					prepared := make(map[string]usageLogInsertPrepared, 64)
					for i := range keys {
						log := &usage.UsageLog{UserID: userIDs[i], BillingUserID: userIDs[i], APIKeyID: keyIDs[i], ProviderID: p.ID, RequestID: fmt.Sprintf("bench-%d-%d", current, i), Model: "test", CreatedAt: time.Now(), InputTokens: 100, OutputTokens: 100, TotalCost: 0.1, ActualCost: 0.1}
						keys[i] = usageLogBatchKey(log.RequestID, log.APIKeyID)
						prepared[keys[i]] = prepareUsageLogInsert(log)
					}
					_, _, _, writeErr := repo.batchInsertUsageLogs(integrationDB, keys, prepared)
					if writeErr != nil {
						b.Error(writeErr)
					}
				}
				if concurrent {
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							write()
						}
					})
				} else {
					for b.Loop() {
						write()
					}
				}
			})
		}
	}
}

// TestUsageLogBillingKeyUpgradeReplay 覆盖旧用量记录与统一 ID 记录之间的双向重放。
func TestUsageLogBillingKeyUpgradeReplay(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &identity.User{Email: suffix + "@billing-key.test"})
	key := mustCreateApiKey(t, client, &apikey.APIKey{UserID: user.ID, Key: suffix, Name: "billing-key"})
	provider := mustCreateProvider(t, client, &providercore.Record{Name: suffix})
	repo := &Store{sql: integrationDB}
	for _, oldFirst := range []bool{true, false} {
		billingKey := "creative_settle:" + uuid.NewString()
		legacy := &usage.UsageLog{UserID: user.ID, APIKeyID: key.ID, ProviderID: provider.ID, RequestID: billingKey, Model: "model", CreatedAt: time.Now().UTC()}
		current := *legacy
		current.RequestID = uuid.NewString()
		current.BillingKey = billingKey
		first, second := legacy, &current
		if !oldFirst {
			first, second = &current, legacy
		}
		inserted, err := repo.createSingle(ctx, integrationDB, first)
		require.NoError(t, err)
		require.True(t, inserted)
		inserted, err = repo.createSingle(ctx, integrationDB, second)
		require.NoError(t, err)
		require.False(t, inserted)
		require.Equal(t, first.ID, second.ID)
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE COALESCE(billing_key, request_id)=$1 AND api_key_id=$2", billingKey, key.ID).Scan(&count))
		require.Equal(t, 1, count)
	}
}
