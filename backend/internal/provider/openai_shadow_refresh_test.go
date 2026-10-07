package provider_test

import (
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// --- 1. CanRefresh 守卫 ---

// TestOpenAITokenRefresherSkipsShadow 验证影子提供商不被后台 token 刷新器处理。
func TestOpenAITokenRefresherSkipsShadow(t *testing.T) {
	pid := int64(100)
	r := &providercore.OpenAITokenRefresher{}
	// 影子提供商：ParentProviderID 非 nil → CanRefresh 应返回 false
	require.False(t, r.CanRefresh(&providercore.Record{ID: 200, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ParentProviderID: &pid}))
	// 普通提供商：有 refresh_token → CanRefresh 应返回 true
	require.True(t, r.CanRefresh(&providercore.Record{ID: 100, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{"refresh_token": "RT"}}))
}

// --- 2. TestProviderConnection 影子凭据解析 ---

// --- 3. EnsureOpenAIPrivacy 守卫 ---
