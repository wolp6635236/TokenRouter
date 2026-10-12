package rediscache

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestApiKeyRateLimitKey(t *testing.T) {
	tests := []struct {
		name     string
		userID   int64
		expected string
	}{
		{
			name:     "normal_user_id",
			userID:   123,
			expected: "apikey:ratelimit:123",
		},
		{
			name:     "zero_user_id",
			userID:   0,
			expected: "apikey:ratelimit:0",
		},
		{
			name:     "negative_user_id",
			userID:   -1,
			expected: "apikey:ratelimit:-1",
		},
		{
			name:     "max_int64",
			userID:   math.MaxInt64,
			expected: "apikey:ratelimit:9223372036854775807",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ApiKeyRateLimitKey(tc.userID)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestAPIKeyCacheSubscriber_BlocksUntilContextCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache := NewAPIKeyCache(client)
	ctx, cancel := context.WithCancel(context.Background())
	received := make(chan string, 1)
	returned := make(chan error, 1)
	go func() {
		returned <- cache.SubscribeAuthCacheInvalidation(ctx, func(value string) { received <- value })
	}()

	var value string
	require.Eventually(t, func() bool {
		require.NoError(t, client.Publish(context.Background(), AuthCacheInvalidateChannel, "hash").Err())
		select {
		case value = <-received:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, "hash", value)
	select {
	case err := <-returned:
		t.Fatalf("subscriber returned while connection was active: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-returned:
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(time.Second):
		t.Fatal("subscriber did not stop after context cancellation")
	}
}
