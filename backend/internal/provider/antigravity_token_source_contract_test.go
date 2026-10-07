package provider

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestAntigravityTokenProvider_GetAccessToken_Guards(t *testing.T) {
	tokenSource := &AntigravityTokenSource{}

	t.Run("nil provider", func(t *testing.T) {
		token, err := tokenSource.GetAccessToken(context.Background(), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider is nil")
		require.Empty(t, token)
	})

	t.Run("non-antigravity platform", func(t *testing.T) {
		provider := &Record{
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeOAuth,
		}
		token, err := tokenSource.GetAccessToken(context.Background(), provider)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity provider")
		require.Empty(t, token)
	})

	// 静态密钥即使存在，也不能作为 OAuth token 使用。
	for _, kind := range []string{capability.ProviderTypeAPIKey, capability.ProviderTypeUpstream} {
		t.Run(kind, func(t *testing.T) {
			provider := &Record{
				Platform:    capability.PlatformAntigravity,
				Type:        kind,
				Credentials: map[string]any{"api_key": "fixture-key"},
			}
			token, err := tokenSource.GetAccessToken(context.Background(), provider)
			require.Error(t, err)
			require.Contains(t, err.Error(), "not an antigravity oauth provider")
			require.Empty(t, token)
		})
	}
}
