//go:build integration

package redis

// Redis 会话消费场景覆盖 session/store.go 的 JSON 存取与单次消费，
// 并使用 helpers_integration_test.go 创建容器和客户端连接。

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/infra/redis/session"
)

// TestRedisSessionConcurrentConsumption 检查两个客户端共享 JSON 和键格式，并发消费时恰好一个请求成功。
func TestRedisSessionConcurrentConsumption(t *testing.T) {
	client := startRedis(t, t.Context())
	first := session.New(client, "test:session", time.Minute)
	second := session.New(client, "test:session:", time.Minute)
	expected := map[string]string{"state": "opaque", "verifier": "test-value"}
	require.NoError(t, first.Set(t.Context(), " id ", expected))
	var decoded map[string]string
	found, err := second.Get(t.Context(), "id", &decoded)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, expected, decoded)
	type result struct {
		claimed bool
		err     error
	}
	results := make(chan result, 16)
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			claimed, err := second.TryConsume(t.Context(), "id")
			results <- result{claimed, err}
		})
	}
	group.Wait()
	close(results)
	winners := 0
	for value := range results {
		require.NoError(t, value.err)
		if value.claimed {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	require.NoError(t, first.Delete(t.Context(), "id"))
	found, err = second.Get(t.Context(), "id", &decoded)
	require.NoError(t, err)
	require.False(t, found)
	exists, err := client.Exists(t.Context(), "test:session:id", "test:session:used:id").Result()
	require.NoError(t, err)
	require.Zero(t, exists)
}
