package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestOpenAITokenRefresher_NeedsRefresh_SkipsProviderWithoutRefreshToken(t *testing.T) {
	refresher := &OpenAITokenRefresher{}
	expiresAt := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)

	withoutRT := &Record{
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   expiresAt,
		},
	}
	require.False(t, refresher.NeedsRefresh(withoutRT, 5*time.Minute))

	withRT := &Record{
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiresAt,
		},
	}
	require.True(t, refresher.NeedsRefresh(withRT, 5*time.Minute))
}

// TestOpenAITokenRefresherSkipsShadow 验证影子提供商不被后台 token 刷新器处理。
func TestOpenAITokenRefresherSkipsShadow(t *testing.T) {
	pid := int64(100)
	r := &OpenAITokenRefresher{}
	// 影子提供商：ParentProviderID 非 nil → CanRefresh 应返回 false
	require.False(t, r.CanRefresh(&Record{ID: 200, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ParentProviderID: &pid}))
	// 普通提供商：有 refresh_token → CanRefresh 应返回 true
	require.True(t, r.CanRefresh(&Record{ID: 100, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{"refresh_token": "RT"}}))
}

func TestOpenAITokenRefresher_CanRefresh(t *testing.T) {
	refresher := &OpenAITokenRefresher{}

	tests := []struct {
		name     string
		platform string
		accType  string
		want     bool
	}{
		{
			name:     "openai oauth - can refresh",
			platform: capability.PlatformOpenAI,
			accType:  capability.ProviderTypeOAuth,
			want:     true,
		},
		{
			name:     "openai apikey - cannot refresh",
			platform: capability.PlatformOpenAI,
			accType:  capability.ProviderTypeAPIKey,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Record{
				Platform: tt.platform,
				Type:     tt.accType,
			}
			require.Equal(t, tt.want, refresher.CanRefresh(provider))
		})
	}
}
