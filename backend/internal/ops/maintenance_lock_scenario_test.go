package ops

// 本场景覆盖 cleanup_service.go 和 scheduled_report_service.go 的维护任务锁竞争。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// leaderCacheFixture 保留锁拥有者，模拟两个实例竞争同一维护任务。
type leaderCacheFixture struct {
	owners map[string]string
	err    error
}

func (c *leaderCacheFixture) Claim(_ context.Context, key, owner string, _ time.Duration) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	if _, exists := c.owners[key]; exists {
		return false, nil
	}
	c.owners[key] = owner
	return true, nil
}

func (c *leaderCacheFixture) Release(_ context.Context, key, owner string) error {
	if c.owners[key] == owner {
		delete(c.owners, key)
	}
	return nil
}

func (*leaderCacheFixture) Get(context.Context, string) (string, error)              { return "", nil }
func (*leaderCacheFixture) Put(context.Context, string, string, time.Duration) error { return nil }

// TestMaintenanceLeaderLockCompetition 验证后台任务必须抢锁后执行，失败实例不能释放其它实例的锁。
func TestMaintenanceLeaderLockCompetition(t *testing.T) {
	for _, task := range []string{"cleanup", "report"} {
		t.Run(task, func(t *testing.T) {
			cache := &leaderCacheFixture{owners: map[string]string{}}
			var first, second func(context.Context) (func(), bool)
			if task == "cleanup" {
				first = (&OpsCleanupService{redisClient: cache, instanceID: "first"}).tryAcquireLeaderLock
				second = (&OpsCleanupService{redisClient: cache, instanceID: "second"}).tryAcquireLeaderLock
			} else {
				first = NewOpsScheduledReportService(nil, nil, nil, cache, nil).tryAcquireLeaderLock
				second = NewOpsScheduledReportService(nil, nil, nil, cache, nil).tryAcquireLeaderLock
			}
			release, ok := first(t.Context())
			require.True(t, ok)
			require.NotNil(t, release)
			otherRelease, ok := second(t.Context())
			require.False(t, ok)
			require.Nil(t, otherRelease)
			require.Len(t, cache.owners, 1)
			release()
			release, ok = second(t.Context())
			require.True(t, ok)
			require.NotNil(t, release)
			release()
			require.Empty(t, cache.owners)
			// Redis 故障且没有数据库锁时，两个任务都不能获得执行资格。
			cache.err = errors.New("cache unavailable")
			release, ok = first(t.Context())
			require.False(t, ok)
			require.Nil(t, release)
		})
	}
}
