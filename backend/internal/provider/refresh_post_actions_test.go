package provider

import (
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// 迟到的成功后置动作不能清除管理员已经更新的凭据与临时停调。
type refreshSuccessCooldownRepo struct {
	current *Record
	clears  int
}

// 清理完成后发布的快照包含已提交的健康状态。
type refreshSuccessScheduler struct {
	last *Record
}

func cooldownPostActions(repo *refreshSuccessCooldownRepo) *RefreshPostActions {
	return &RefreshPostActions{
		Now: time.Now, Info: slog.Info, Warn: slog.Warn, Debug: slog.Debug, ClearBlock: func(int64) {}, NeedsReauth: GrokNeedsReauth,
		ClearCooldown: func(ctx context.Context, value *Record) (bool, error) {
			return repo.ClearRefreshCooldownIfUnchanged(ctx, ObserveRefreshCooldown(value))
		},
	}
}

// ClearRefreshCooldownIfUnchanged 模拟交错执行的条件更新，PostgreSQL 集成测试覆盖行锁和 JSONB 比较。
func (r *refreshSuccessCooldownRepo) ClearRefreshCooldownIfUnchanged(ctx context.Context, v RefreshCooldownVersion) (bool, error) {
	if !reflect.DeepEqual(ObserveRefreshCooldown(r.current), v) {
		return false, nil
	}
	return true, r.ClearTempUnschedulable(ctx, v.ID)
}

func (r *refreshSuccessCooldownRepo) ClearTempUnschedulable(context.Context, int64) error {
	r.clears++
	r.current.TempUnschedulableUntil = nil
	r.current.TempUnschedulableReason = ""
	return nil
}

func TestRefreshSuccessCannotClearNewAdministratorCooldown(t *testing.T) {
	until := time.Now().Add(time.Hour)
	observed := &Record{ID: 941, Platform: capability.PlatformGemini, Type: capability.ProviderTypeOAuth, Status: StatusActive, Credentials: map[string]any{"refresh_token": "observed"}, TempUnschedulableUntil: &until, TempUnschedulableReason: "old"}
	current := *observed
	current.Credentials = map[string]any{"refresh_token": "administrator"}
	current.TempUnschedulableReason = "new cooldown"
	repo := &refreshSuccessCooldownRepo{current: &current}
	post := cooldownPostActions(repo)
	post.Run(context.Background(), observed)
	require.Zero(t, repo.clears)
	require.NotNil(t, current.TempUnschedulableUntil)
	require.Equal(t, "new cooldown", current.TempUnschedulableReason)
}

func (s *refreshSuccessScheduler) SetProvider(_ context.Context, v *Record) error {
	copy := *v
	s.last = &copy
	return nil
}

func TestRefreshSuccessPublishesClearedCooldown(t *testing.T) {
	until := time.Now().Add(time.Hour)
	observed := &Record{ID: 942, Platform: capability.PlatformGemini, Type: capability.ProviderTypeOAuth, Status: StatusActive, TempUnschedulableUntil: &until, TempUnschedulableReason: "old"}
	current := *observed
	repo := &refreshSuccessCooldownRepo{current: &current}
	cache := &refreshSuccessScheduler{}
	post := cooldownPostActions(repo)
	post.SyncProvider = cache.SetProvider
	post.Run(context.Background(), observed)
	require.Equal(t, 1, repo.clears)
	require.NotNil(t, cache.last)
	require.Nil(t, cache.last.TempUnschedulableUntil)
	require.Empty(t, cache.last.TempUnschedulableReason)
}
