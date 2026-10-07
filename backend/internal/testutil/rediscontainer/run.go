//go:build integration

package rediscontainer

import (
	"context"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Run 等待 Redis 开始监听并输出就绪日志，启动上限为一分钟。
// Docker 在并发创建容器时可能较慢，调用方的 context 仍可提前取消启动。
func Run(ctx context.Context, image string) (*redis.RedisContainer, error) {
	return redis.Run(ctx, image, testcontainers.WithWaitStrategy(
		wait.ForListeningPort("6379/tcp").WithStartupTimeout(time.Minute),
		wait.ForLog("* Ready to accept connections").WithStartupTimeout(time.Minute),
	))
}
