package provider_test

import (
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGrokTokenRefreshWindowWithJitter_StableAndBounded(t *testing.T) {
	base := providercore.GrokTokenRefreshSkew
	w1 := providercore.GrokTokenRefreshWindowWithJitter(42, base)
	w2 := providercore.GrokTokenRefreshWindowWithJitter(42, base)
	require.Equal(t, w1, w2, "same provider id must yield stable window")
	require.GreaterOrEqual(t, w1, providercore.GrokTokenRefreshSkewMin)
	require.LessOrEqual(t, w1, base)

	// 不同提供商通常应得到不同结果。该性质并不保证任意提供商对都不同，
	// 但连续 ID 的哈希分布足以在小样本中断言存在差异。
	seen := map[time.Duration]bool{}
	for id := int64(1); id <= 50; id++ {
		seen[providercore.GrokTokenRefreshWindowWithJitter(id, base)] = true
	}
	require.Greater(t, len(seen), 1, "jitter should spread windows across providers")
}

func TestGrokTokenRefresher_NeedsRefresh_UsesSkewFloor(t *testing.T) {
	refresher := providercore.NewGrokTokenRefresher(nil)
	// 令牌 50 分钟后过期，位于一小时预热窗口内，应当刷新。
	expires := time.Now().Add(50 * time.Minute).UTC().Format(time.RFC3339)
	provider := &providercore.Record{
		ID:       7,
		Platform: capability.PlatformGrok,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at",
			"refresh_token": "rt",
			"expires_at":    expires,
		},
	}
	// 传入很小的窗口，NeedsRefresh 会先提升到 grokTokenRefreshSkew 再应用错峰偏移。
	require.True(t, refresher.NeedsRefresh(provider, time.Minute))

	// 过期时间仍很远时无需刷新。
	provider.Credentials["expires_at"] = time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	require.False(t, refresher.NeedsRefresh(provider, time.Minute))
}
