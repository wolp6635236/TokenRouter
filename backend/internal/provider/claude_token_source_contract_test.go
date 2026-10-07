package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestClaudeTokenProvider_CacheHit(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	provider := &Record{
		ID:       100,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "db-token",
		},
	}
	cacheKey := ClaudeTokenCacheKey(provider)
	cache.tokens[cacheKey] = "cached-token"

	tokenSource := newClaudeTokenSourceContract(cache)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "cached-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.getCalled))
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.setCalled))
}

func TestClaudeTokenProvider_CacheMiss_FromCredentials(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	// Token expires in far future, no refresh needed
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       101,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "credential-token",
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "credential-token", token)

	// Should have stored in cache
	cacheKey := ClaudeTokenCacheKey(provider)
	require.Equal(t, "credential-token", cache.tokens[cacheKey])
}

func TestClaudeTokenProvider_NilProvider(t *testing.T) {
	tokenSource := newClaudeTokenSourceContract(nil)

	token, err := tokenSource.GetAccessToken(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider is nil")
	require.Empty(t, token)
}

func TestClaudeTokenProvider_WrongPlatform(t *testing.T) {
	tokenSource := newClaudeTokenSourceContract(nil)
	provider := &Record{
		ID:       104,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
	}

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an anthropic oauth or service account")
	require.Empty(t, token)
}

func TestClaudeTokenProvider_WrongProviderType(t *testing.T) {
	tokenSource := newClaudeTokenSourceContract(nil)
	provider := &Record{
		ID:       105,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeAPIKey,
	}

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an anthropic oauth or service account")
	require.Empty(t, token)
}

func TestClaudeTokenProvider_SetupTokenType(t *testing.T) {
	tokenSource := newClaudeTokenSourceContract(nil)
	provider := &Record{
		ID:       106,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeSetupToken,
	}

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an anthropic oauth or service account")
	require.Empty(t, token)
}

func TestClaudeTokenProvider_NilCache(t *testing.T) {
	// Token doesn't need refresh
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       107,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "nocache-token",
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(nil)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "nocache-token", token)
}

func TestClaudeTokenProvider_CacheGetError(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.getErr = errors.New("redis connection failed")

	// Token doesn't need refresh
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       108,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "fallback-token",
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)

	// Should gracefully degrade and return from credentials
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "fallback-token", token)
}

func TestClaudeTokenProvider_CacheSetError(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.setErr = errors.New("redis write failed")

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       109,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "still-works-token",
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)

	// Should still work even if cache set fails
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "still-works-token", token)
}

func TestClaudeTokenProvider_MissingAccessToken(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       110,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt,
			// missing access_token
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestClaudeTokenProvider_TTLCalculation(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn time.Duration
	}{
		{
			name:      "far_future_expiry",
			expiresIn: 1 * time.Hour,
		},
		{
			name:      "medium_expiry",
			expiresIn: 10 * time.Minute,
		},
		{
			name:      "near_expiry",
			expiresIn: 6 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newClaudeTokenCacheStub()
			expiresAt := time.Now().Add(tt.expiresIn).Format(time.RFC3339)
			provider := &Record{
				ID:       200,
				Platform: capability.PlatformAnthropic,
				Type:     capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"access_token": "test-token",
					"expires_at":   expiresAt,
				},
			}

			tokenSource := newClaudeTokenSourceContract(cache)

			_, err := tokenSource.GetAccessToken(context.Background(), provider)
			require.NoError(t, err)

			// Verify token was cached
			cacheKey := ClaudeTokenCacheKey(provider)
			require.Equal(t, "test-token", cache.tokens[cacheKey])
		})
	}
}

// TestClaudeTokenProvider_Real_LockFailedWait 检查 Claude token 源获取锁失败后的等待行为。
func TestClaudeTokenProvider_Real_LockFailedWait(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.lockAcquired = false // Lock acquisition fails

	// Token expires soon (within refresh skew) to trigger lock attempt
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       300,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "fallback-token",
			"expires_at":   expiresAt,
		},
	}

	// Set token in cache after lock wait period (simulate other worker refreshing)
	cacheKey := ClaudeTokenCacheKey(provider)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cache.mu.Lock()
		cache.tokens[cacheKey] = "refreshed-by-other"
		cache.mu.Unlock()
	}()

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestClaudeTokenProvider_Real_CacheHitAfterWait(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.lockAcquired = false // Lock acquisition fails

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       301,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "original-token",
			"expires_at":   expiresAt,
		},
	}

	cacheKey := ClaudeTokenCacheKey(provider)
	// Set token in cache immediately after wait starts
	go func() {
		time.Sleep(50 * time.Millisecond)
		cache.mu.Lock()
		cache.tokens[cacheKey] = "winner-token"
		cache.mu.Unlock()
	}()

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestClaudeTokenProvider_Real_NoExpiresAt(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.lockAcquired = false // Prevent entering refresh logic

	// Token with nil expires_at (no expiry set)
	provider := &Record{
		ID:       302,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "no-expiry-token",
		},
	}

	// After lock wait, return token from credentials
	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "no-expiry-token", token)
}

func TestClaudeTokenProvider_Real_WhitespaceToken(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cacheKey := "claude:provider:303"
	cache.tokens[cacheKey] = "   " // Whitespace only - should be treated as empty

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       303,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "real-token",
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "real-token", token)
}

func TestClaudeTokenProvider_Real_EmptyCredentialToken(t *testing.T) {
	cache := newClaudeTokenCacheStub()

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       304,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "   ", // Whitespace only
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

func TestClaudeTokenProvider_Real_LockError(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.lockErr = errors.New("redis lock failed")

	// Token expires soon (within refresh skew)
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       305,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "fallback-on-lock-error",
			"expires_at":   expiresAt,
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "fallback-on-lock-error", token)
}

func TestClaudeTokenProvider_Real_NilCredentials(t *testing.T) {
	cache := newClaudeTokenCacheStub()

	expiresAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	provider := &Record{
		ID:       306,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"expires_at": expiresAt,
			// No access_token
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}
