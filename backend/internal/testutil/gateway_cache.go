package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	gatewayredis "github.com/TokenFlux/TokenRouter/internal/gateway/rediscache"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
)

var _ session.GatewayCache = StubGatewayCache{}

// StubGatewayCache 是返回空会话和可领取状态的网关缓存替身。
type StubGatewayCache struct{}

// NewRedisGatewayCache 使用 miniredis 创建网关缓存，并注册连接清理。
func NewRedisGatewayCache(t *testing.T) session.GatewayCache {
	t.Helper()

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	return gatewayredis.NewGatewayCache(redisClient)
}

// GetSessionProviderID 返回空的会话提供商 ID。
func (c StubGatewayCache) GetSessionProviderID(_ context.Context, _ int64, _ string) (int64, error) {
	return 0, nil
}

// SetSessionProviderID 模拟成功写入会话提供商 ID。
func (c StubGatewayCache) SetSessionProviderID(_ context.Context, _ int64, _ string, _ int64, _ time.Duration) error {
	return nil
}

// RefreshSessionTTL 模拟成功续期会话。
func (c StubGatewayCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

// DeleteSessionProviderID 模拟成功删除会话提供商 ID。
func (c StubGatewayCache) DeleteSessionProviderID(_ context.Context, _ int64, _ string) error {
	return nil
}

// SetSessionOwnerGroupID 模拟成功写入会话所属分组。
func (c StubGatewayCache) SetSessionOwnerGroupID(_ context.Context, _ int64, _, _ string, _ int64, _ time.Duration) (bool, error) {
	return true, nil
}

// GetSessionOwnerGroupID 返回空的会话所属分组 ID。
func (c StubGatewayCache) GetSessionOwnerGroupID(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}

// RefreshSessionOwnerTTL 模拟成功续期会话所属分组。
func (c StubGatewayCache) RefreshSessionOwnerTTL(_ context.Context, _ int64, _, _ string, _ time.Duration) error {
	return nil
}

// SetGrokVideoPendingBilling 模拟成功写入视频待计费数据。
func (c StubGatewayCache) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

// GetGrokVideoPendingBilling 返回空的视频待计费数据。
func (c StubGatewayCache) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

// ClaimGrokVideoBilled 模拟成功领取视频计费标记。
func (c StubGatewayCache) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

// ReleaseGrokVideoBilled 模拟成功释放视频计费标记。
func (c StubGatewayCache) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}
