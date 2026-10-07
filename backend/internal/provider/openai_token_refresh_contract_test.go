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

// openAIProviderRepoStub is a minimal stub implementing only the methods used by OpenAITokenProvider
type openAIProviderRepoStub struct {
	provider     *Record
	getErr       error
	updateErr    error
	getCalled    int32
	updateCalled int32
}

func (r *openAIProviderRepoStub) GetByID(ctx context.Context, id int64) (*Record, error) {
	atomic.AddInt32(&r.getCalled, 1)
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.provider, nil
}

func (r *openAIProviderRepoStub) Update(ctx context.Context, provider *Record) error {
	atomic.AddInt32(&r.updateCalled, 1)
	if r.updateErr != nil {
		return r.updateErr
	}
	r.provider = provider
	return nil
}

// openAIOAuthServiceStub implements OpenAIOAuthService methods for testing
type openAIOAuthServiceStub struct {
	tokenInfo     *OpenAITokenInfo
	refreshErr    error
	refreshCalled int32
}

func (s *openAIOAuthServiceStub) RefreshProviderToken(ctx context.Context, provider *Record) (*OpenAITokenInfo, error) {
	atomic.AddInt32(&s.refreshCalled, 1)
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return s.tokenInfo, nil
}

func TestOpenAITokenProvider_TokenRefresh(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	providerRepo := &openAIProviderRepoStub{}
	oauthService := &openAIOAuthServiceStub{
		tokenInfo: &OpenAITokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// Token expires soon (within refresh skew)
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       102,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
	}
	providerRepo.provider = provider

	// We need to directly test with the stub - create a custom provider
	customProvider := newOpenAIRefreshSourceFixture(providerRepo, cache, oauthService)

	token, err := customProvider.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "refreshed-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&oauthService.refreshCalled))
}

func TestOpenAITokenProvider_LockRaceCondition(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	cache.simulateLockRace = true
	providerRepo := &openAIProviderRepoStub{}

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       103,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "race-token",
			"expires_at":   expiresAt,
		},
	}
	providerRepo.provider = provider

	// Simulate another worker already refreshed and cached
	cacheKey := OpenAITokenCacheKey(provider)
	go func() {
		time.Sleep(5 * time.Millisecond)
		cache.mu.Lock()
		cache.tokens[cacheKey] = "winner-token"
		cache.mu.Unlock()
	}()

	tokenSource := newOpenAIRefreshSourceFixture(providerRepo, cache, nil)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	// Should get the token set by the "winner" or the original
	require.NotEmpty(t, token)
}

func TestOpenAITokenProvider_RefreshError(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	providerRepo := &openAIProviderRepoStub{}
	oauthService := &openAIOAuthServiceStub{
		refreshErr: errors.New("oauth refresh failed"),
	}

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       110,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
	}
	providerRepo.provider = provider

	tokenSource := newOpenAIRefreshSourceFixture(providerRepo, cache, oauthService)

	// Now with fallback behavior, should return existing token even if refresh fails
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "old-token", token) // Fallback to existing token
}

func TestOpenAITokenProvider_OAuthServiceNotConfigured(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	providerRepo := &openAIProviderRepoStub{}

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       111,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "old-token",
			"expires_at":   expiresAt,
		},
	}
	providerRepo.provider = provider

	tokenSource := newOpenAIRefreshSourceFixture(providerRepo, cache, nil)

	// Now with fallback behavior, should return existing token even if oauth service not configured
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "old-token", token) // Fallback to existing token
}

func TestOpenAITokenProvider_DoubleCheckAfterLock(t *testing.T) {
	cache := newOpenAITokenCacheStub()
	providerRepo := &openAIProviderRepoStub{}
	oauthService := &openAIOAuthServiceStub{
		tokenInfo: &OpenAITokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// Token expires soon
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       112,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "old-token",
			"expires_at":   expiresAt,
		},
	}
	providerRepo.provider = provider
	cacheKey := OpenAITokenCacheKey(provider)

	// Simulate: first GetAccessToken returns empty, but after lock acquired, cache has token

	cache.tokens[cacheKey] = "" // Empty initially

	tokenSource := newOpenAIRefreshSourceFixture(providerRepo, cache, oauthService)

	// In a goroutine, set the cached token after a small delay (simulating race)
	go func() {
		time.Sleep(5 * time.Millisecond)
		cache.mu.Lock()
		cache.tokens[cacheKey] = "cached-by-other"
		cache.mu.Unlock()
	}()

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	// Should get either the refreshed token or the cached one
	require.NotEmpty(t, token)
}
