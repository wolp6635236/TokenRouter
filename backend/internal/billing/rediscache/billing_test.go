package rediscache

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingBalanceKey(t *testing.T) {
	tests := []struct {
		name     string
		userID   int64
		expected string
	}{
		{
			name:     "normal_user_id",
			userID:   123,
			expected: "billing:balance:123",
		},
		{
			name:     "zero_user_id",
			userID:   0,
			expected: "billing:balance:0",
		},
		{
			name:     "negative_user_id",
			userID:   -1,
			expected: "billing:balance:-1",
		},
		{
			name:     "max_int64",
			userID:   math.MaxInt64,
			expected: "billing:balance:9223372036854775807",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := billingBalanceKey(tc.userID)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestJitteredTTL(t *testing.T) {
	const (
		minTTL = 4*time.Minute + 30*time.Second // 270s = 5min - 30s
		maxTTL = 5*time.Minute + 30*time.Second // 330s = 5min + 30s
	)

	for range 200 {
		ttl := jitteredTTL()
		require.GreaterOrEqual(t, ttl, minTTL, "jitteredTTL() 返回值低于下限: %v", ttl)
		require.LessOrEqual(t, ttl, maxTTL, "jitteredTTL() 返回值超过上限: %v", ttl)
	}
}

func TestJitteredTTL_HasVariation(t *testing.T) {
	// 多次调用应该产生不同的值（验证抖动存在）
	seen := make(map[time.Duration]struct{}, 50)
	for range 50 {
		seen[jitteredTTL()] = struct{}{}
	}
	// 50 次调用中应该至少有 2 个不同的值
	require.Greater(t, len(seen), 1, "jitteredTTL() 应产生不同的 TTL 值")
}

func TestJitteredTTL_WithinExpectedRange(t *testing.T) {
	// jitteredTTL 使用减法抖动: billingCacheTTL - [0, billingCacheJitter)
	// 所以结果应在 [billingCacheTTL - billingCacheJitter, billingCacheTTL] 范围内
	lowerBound := billingCacheTTL - billingCacheJitter // 5min - 30s = 4min30s
	upperBound := billingCacheTTL                      // 5min

	for range 200 {
		ttl := jitteredTTL()
		assert.GreaterOrEqual(t, int64(ttl), int64(lowerBound),
			"TTL 不应低于 %v，实际得到 %v", lowerBound, ttl)
		assert.LessOrEqual(t, int64(ttl), int64(upperBound),
			"TTL 不应超过 %v（上界不变保证），实际得到 %v", upperBound, ttl)
	}
}

func TestJitteredTTL_NeverExceedsBase(t *testing.T) {
	// 减法抖动产生的 TTL 不超过 billingCacheTTL。
	for range 500 {
		ttl := jitteredTTL()
		assert.LessOrEqual(t, int64(ttl), int64(billingCacheTTL),
			"jitteredTTL 不应超过基础 TTL（上界预期不被打破）")
	}
}

func TestJitteredTTL_HasVariance(t *testing.T) {
	// 验证抖动确实产生了不同的值
	results := make(map[time.Duration]bool)
	for range 100 {
		ttl := jitteredTTL()
		results[ttl] = true
	}

	require.Greater(t, len(results), 1,
		"jitteredTTL 应产生不同的值（抖动生效），但 100 次调用结果全部相同")
}

func TestJitteredTTL_AverageNearCenter(t *testing.T) {
	// 验证平均值大约在抖动范围中间
	var sum time.Duration
	runs := 1000
	for range runs {
		sum += jitteredTTL()
	}

	avg := sum / time.Duration(runs)
	expectedCenter := billingCacheTTL - billingCacheJitter/2 // 4min45s

	// 允许 ±5s 的误差
	tolerance := 5 * time.Second
	assert.InDelta(t, float64(expectedCenter), float64(avg), float64(tolerance),
		"平均 TTL 应接近抖动范围中心 %v", expectedCenter)
}

func TestBillingKeyGeneration(t *testing.T) {
	t.Run("balance_key", func(t *testing.T) {
		key := billingBalanceKey(12345)
		assert.Equal(t, "billing:balance:12345", key)
	})
}

func BenchmarkJitteredTTL(b *testing.B) {
	for range b.N {
		_ = jitteredTTL()
	}
}
