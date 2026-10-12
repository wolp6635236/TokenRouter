//go:build integration

package rediscache

import (
	"context"
	"log"
	"os"
	"testing"

	redisclient "github.com/redis/go-redis/v9"

	"github.com/TokenFlux/TokenRouter/internal/testutil/rediscontainer"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := rediscontainer.Run(ctx, "redis:8.4-alpine")
	if err != nil {
		log.Printf("启动隔离 Redis 失败: %v", err)
		os.Exit(1)
	}
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		log.Print(err)
		os.Exit(1)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		log.Print(err)
		os.Exit(1)
	}
	integrationRedis = redisclient.NewClient(&redisclient.Options{Addr: host + ":" + port.Port()})
	m.Run()
	_ = integrationRedis.Close()
	_ = container.Terminate(ctx)
}
