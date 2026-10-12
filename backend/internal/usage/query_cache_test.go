package usage

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// statsCancellationRepo 将查询停在读取阶段，便于交错两个 HTTP 请求的取消。
type statsCancellationRepo struct {
	UsageLogRepository
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

// joinedStatsContext 在等待者进入取消选择时通知测试。
type joinedStatsContext struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func TestUsageStatsCacheKey_StableAndDistinct(t *testing.T) {
	start := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	base := UsageLogFilters{StartTime: &start, EndTime: &end, Model: "claude-3"}

	k1 := usageStatsCacheKey(base)
	k2 := usageStatsCacheKey(base)
	require.NotEmpty(t, k1)
	require.Equal(t, k1, k2, "same filters must produce same key")

	other := base
	other.Model = "gpt-4o"
	require.NotEqual(t, k1, usageStatsCacheKey(other), "different model must change key")

	withUser := base
	withUser.UserID = 7
	require.NotEqual(t, k1, usageStatsCacheKey(withUser), "different user must change key")

	// 团队维度必须参与缓存键，防止切换团队后复用旧汇总。
	withTeam := base
	withTeam.TeamID = 11
	require.NotEqual(t, k1, usageStatsCacheKey(withTeam), "different team must change key")
	// 同一范围的不同端点图分别缓存。
	keys := map[string]bool{k1: true}
	for _, source := range []string{"inbound", "upstream", "path"} {
		filtered := base
		filtered.EndpointSource = source
		key := usageStatsCacheKey(filtered)
		require.False(t, keys[key])
		keys[key] = true
	}
}

// GetStatsWithFilters 等待测试释放查询或底层上下文取消。
func (r *statsCancellationRepo) GetStatsWithFilters(ctx context.Context, _ UsageLogFilters) (*UsageStats, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
	}
	select {
	case <-r.release:
		return &UsageStats{TotalRequests: 17}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Done 在等待登记完成后通知主测试协程。
func (c *joinedStatsContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}

// TestStatsCanceledLeaderKeepsLiveWaiter 复现首请求退出时同筛选的另一个请求仍在等待。
func TestStatsCanceledLeaderKeepsLiveWaiter(t *testing.T) {
	repo := &statsCancellationRepo{started: make(chan struct{}), release: make(chan struct{})}
	service := NewUsageService(repo)
	filters := UsageLogFilters{EndpointSource: "upstream"}
	first, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone := make(chan error, 1)
	go func() { _, _, err := service.GetStatsCached(first, filters); firstDone <- err }()
	<-repo.started
	live := &joinedStatsContext{Context: context.Background(), joined: make(chan struct{})}
	nextDone := make(chan error, 1)
	nextValue := make(chan *UsageStats, 1)
	go func() { value, _, err := service.GetStatsCached(live, filters); nextValue <- value; nextDone <- err }()
	<-live.joined
	cancel()
	require.ErrorIs(t, <-firstDone, context.Canceled)
	close(repo.release)
	require.NoError(t, <-nextDone)
	require.EqualValues(t, 17, (<-nextValue).TotalRequests)
	require.EqualValues(t, 1, repo.calls.Load())
}
