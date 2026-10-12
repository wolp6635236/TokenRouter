//go:build integration

package rediscache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

type SessionLimitCacheSuite struct {
	IntegrationRedisSuite
	cache scheduler.SessionLimitCache
}

// TestSessionBatchAfterScriptFlush 检查 Redis 丢失脚本缓存后，批量读取仍返回已有的活动会话。
func TestSessionBatchAfterScriptFlush(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewSessionLimitCache(rdb, 5)
	allowed, err := cache.RegisterSession(ctx, 91001, "session-a", 2, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, rdb.ScriptFlush(ctx).Err())

	counts, err := cache.GetActiveSessionCountBatch(ctx, []int64{91001, 91002}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, counts[91001])
	require.Equal(t, 0, counts[91002])

	// 后续单次活动刷新复用该注册项和空闲窗口。
	require.NoError(t, cache.RefreshSession(ctx, 91001, "session-a", time.Minute))
	count, err := cache.GetActiveSessionCount(ctx, 91001)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func (s *SessionLimitCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewSessionLimitCache(s.rdb, 5)
}

// TestUnregisterSession_FreesSlotForNewSession 检查 max_sessions=1 时释放会话 A 后，会话 B 能立即注册。
// 转发失败的请求若未注销，会话槽会占用整个空闲超时窗口（默认 5 分钟）。
func (s *SessionLimitCacheSuite) TestUnregisterSession_FreesSlotForNewSession() {
	const providerID = int64(101)
	maxSessions := 1
	idleTimeout := 5 * time.Minute

	// 会话 A 注册成功（请求开始转发）
	allowed, err := s.cache.RegisterSession(s.ctx, providerID, "session-A", maxSessions, idleTimeout)
	s.RequireNoError(err, "RegisterSession A")
	s.True(allowed, "首个会话应被允许")

	// A 仍占用会话槽，空闲超时窗口内的新会话 B 被拒。
	allowed, err = s.cache.RegisterSession(s.ctx, providerID, "session-B", maxSessions, idleTimeout)
	s.RequireNoError(err, "RegisterSession B before release")
	s.False(allowed, "槽位被 A 占用时新会话应被拒绝")

	// 请求最终失败：立即释放 A 的会话注册（不等待空闲超时）
	s.RequireNoError(s.cache.UnregisterSession(s.ctx, providerID, "session-A"), "UnregisterSession A")

	// A 释放后 B 立即可注册，无需等待空闲超时
	allowed, err = s.cache.RegisterSession(s.ctx, providerID, "session-B", maxSessions, idleTimeout)
	s.RequireNoError(err, "RegisterSession B after release")
	s.True(allowed, "释放后新会话应立即可注册")

	count, err := s.cache.GetActiveSessionCount(s.ctx, providerID)
	s.RequireNoError(err, "GetActiveSessionCount")
	s.Equal(1, count, "释放后活跃会话数应为 1（仅 B）")
}

// TestUnregisterSession_IdempotentAndMissing 验证释放操作幂等：
// 重复释放与释放不存在的会话均不应报错。
func (s *SessionLimitCacheSuite) TestUnregisterSession_IdempotentAndMissing() {
	const providerID = int64(102)

	s.RequireNoError(s.cache.UnregisterSession(s.ctx, providerID, "missing"), "释放不存在的会话")
	s.RequireNoError(s.cache.UnregisterSession(s.ctx, providerID, "missing"), "重复释放")

	s.RequireNoError(s.cache.UnregisterSession(s.ctx, providerID, ""), "空 sessionUUID 应为 no-op")
}

// TestUnregisterSession_OnlyTargetsSpecifiedSession 检查指定会话被移除后，同提供商的其他会话仍然活跃。
func (s *SessionLimitCacheSuite) TestUnregisterSession_OnlyTargetsSpecifiedSession() {
	const providerID = int64(103)
	maxSessions := 2
	idleTimeout := 5 * time.Minute

	for _, sid := range []string{"session-A", "session-B"} {
		allowed, err := s.cache.RegisterSession(s.ctx, providerID, sid, maxSessions, idleTimeout)
		s.RequireNoError(err, "RegisterSession "+sid)
		s.True(allowed)
	}

	s.RequireNoError(s.cache.UnregisterSession(s.ctx, providerID, "session-A"), "UnregisterSession A")

	active, err := s.cache.IsSessionActive(s.ctx, providerID, "session-A")
	s.RequireNoError(err, "IsSessionActive A")
	s.False(active, "A 应已释放")

	active, err = s.cache.IsSessionActive(s.ctx, providerID, "session-B")
	s.RequireNoError(err, "IsSessionActive B")
	s.True(active, "B 不应受影响")
}

func TestSessionLimitCacheSuite(t *testing.T) {
	suite.Run(t, new(SessionLimitCacheSuite))
}
