package provider

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/querycache"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

type grokTokenCacheForProviderTest struct {
	token        string
	setKey       string
	setToken     string
	setTTL       time.Duration
	lockResult   bool
	releaseCalls int
	deletedKeys  []string
	deleteErr    error
	getCalls     int
	mu           sync.Mutex
}

type grokCredentialRaceRepo struct {
	*tokenRefreshProviderRepo
	mu sync.RWMutex
}

func (r *grokCredentialRaceRepo) GetByID(ctx context.Context, id int64) (*providercore.Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tokenRefreshProviderRepo.GetByID(ctx, id)
}

func (r *grokCredentialRaceRepo) setProvider(provider *providercore.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providersByID[provider.ID] = provider
}

func (c *grokTokenCacheForProviderTest) GetAccessToken(context.Context, string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getCalls++
	if c.token == "" {
		return "", errors.New("not cached")
	}
	return c.token, nil
}

func (c *grokTokenCacheForProviderTest) SetAccessToken(_ context.Context, key string, token string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setKey = key
	c.setToken = token
	c.setTTL = ttl
	return nil
}

func (c *grokTokenCacheForProviderTest) DeleteAccessToken(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletedKeys = append(c.deletedKeys, key)
	return c.deleteErr
}

func (c *grokTokenCacheForProviderTest) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return c.lockResult, nil
}

func (c *grokTokenCacheForProviderTest) ReleaseRefreshLock(context.Context, string) error {
	c.releaseCalls++
	return nil
}

func TestGrokTokenProviderRefreshesExpiredTokenOnRequestPath(t *testing.T) {
	t.Setenv(xai.EnvBaseURL, xai.DefaultCLIBaseURL)

	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	provider := &providercore.Record{
		ID:          54,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiredAt,
			"base_url":      xai.DefaultCLIBaseURL,
			"client_id":     "client-id",
		},
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{54: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	oauthSvc := newGrokAuthorizationForTest(nil, &grokOAuthClientStub{
		refreshResponse: &xai.TokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
	})
	oauthSvc.Start()
	defer stopGrokAuthorizationForTest(t, oauthSvc)

	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), providercore.NewGrokTokenRefresher(oauthSvc))

	token, err := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))
	require.NoError(t, err)
	require.Equal(t, "new-access-token", token)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, "new-access-token", repo.providersByID[54].GetGrokAccessToken())
	require.Equal(t, "refresh-token", repo.providersByID[54].GetGrokRefreshToken())
	require.Equal(t, xai.DefaultCLIBaseURL, GrokProviderBaseURL(repo.providersByID[54]))
	require.Equal(t, "grok:provider:54", cache.setKey)
	require.Equal(t, "new-access-token", cache.setToken)
	require.Greater(t, cache.setTTL, time.Duration(0))
	require.Equal(t, 1, cache.releaseCalls)
}

func TestGrokTokenProviderRefreshFailureUnschedulesWithRedactedReason(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	provider := &providercore.Record{
		ID:          55,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiredAt,
			"base_url":      xai.DefaultCLIBaseURL,
		},
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{55: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{
		err: errors.New("temporary refresh failure access_token=leaked-access refresh_token=leaked-refresh"),
	})

	token, err := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))
	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, 0, repo.setTempUnschedCalls)
	require.Equal(t, 0, repo.setErrorCalls)
}

func TestGrokTokenProviderLockHeldWaitsForRefreshedCacheAndNeverUsesExpiredToken(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(56)
	baseRepo := &tokenRefreshProviderRepo{}
	baseRepo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	repo := &grokCredentialRaceRepo{tokenRefreshProviderRepo: baseRepo}
	cache := &grokTokenCacheForProviderTest{lockResult: false, token: "expired-access-token"}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{})

	go func() {
		time.Sleep(40 * time.Millisecond)
		refreshed := *provider
		refreshed.Credentials = querycache.ShallowMap(provider.Credentials)
		refreshed.Credentials["access_token"] = "refreshed-after-lock"
		refreshed.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		refreshed.Credentials["_token_version"] = time.Now().UnixMilli()
		repo.setProvider(&refreshed)
		cache.mu.Lock()
		cache.token = "refreshed-after-lock"
		cache.mu.Unlock()
	}()

	startedAt := time.Now()
	token, err := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))
	require.NoError(t, err)
	require.Equal(t, "refreshed-after-lock", token)
	require.NotEqual(t, "expired-access-token", token)
	require.GreaterOrEqual(t, time.Since(startedAt), 25*time.Millisecond,
		"expired provider metadata must prevent returning the old cached token")
}

func TestGrokTokenProviderLockHeldTimeoutDoesNotReturnExpiredToken(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(57)
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: false}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	token, err := tokenSource.GetAccessToken(ctx, providercore.CloneRecord(provider))
	require.Error(t, err)
	require.Empty(t, token)
}

func TestGrokTokenProviderLockHeldRejectsChangedTokenWithoutExpiry(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(58)
	baseRepo := &tokenRefreshProviderRepo{}
	baseRepo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	repo := &grokCredentialRaceRepo{tokenRefreshProviderRepo: baseRepo}
	cache := &grokTokenCacheForProviderTest{lockResult: false, token: "expired-access-token"}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{})

	go func() {
		time.Sleep(30 * time.Millisecond)
		refreshed := *provider
		refreshed.Credentials = querycache.ShallowMap(provider.Credentials)
		refreshed.Credentials["access_token"] = "changed-without-expiry"
		delete(refreshed.Credentials, "expires_at")
		refreshed.Credentials["_token_version"] = time.Now().UnixMilli()
		repo.setProvider(&refreshed)
		cache.mu.Lock()
		cache.token = "changed-without-expiry"
		cache.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	token, err := tokenSource.GetAccessToken(ctx, providercore.CloneRecord(provider))

	require.Error(t, err)
	require.Empty(t, token, "an unbounded credential must not win the lock-held race")
}

func TestGrokTokenProviderLockHeldUsesVersionedDBTokenAndRepairsStaleCache(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(60)
	baseRepo := &tokenRefreshProviderRepo{}
	baseRepo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	repo := &grokCredentialRaceRepo{tokenRefreshProviderRepo: baseRepo}
	cache := &grokTokenCacheForProviderTest{lockResult: false, token: "expired-access-token"}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{})

	go func() {
		time.Sleep(30 * time.Millisecond)
		refreshed := *provider
		refreshed.Credentials = querycache.ShallowMap(provider.Credentials)
		refreshed.Credentials["access_token"] = "db-authoritative-token"
		refreshed.Credentials["expires_at"] = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
		refreshed.Credentials["_token_version"] = time.Now().UnixMilli()
		repo.setProvider(&refreshed)
	}()

	token, err := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))

	require.NoError(t, err)
	require.Equal(t, "db-authoritative-token", token)
	require.Equal(t, "db-authoritative-token", cache.setToken)
	require.Greater(t, cache.setTTL, time.Duration(0))
}

func TestGrokTokenProviderRejectsStaleDBTokenWithoutExpiry(t *testing.T) {
	expiresAt := time.Now().Add(2 * providercore.GrokTokenRefreshSkew).UTC().Format(time.RFC3339)
	provider := &providercore.Record{
		ID:          59,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "old-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiresAt,
		},
	}
	latest := *provider
	latest.Credentials = querycache.ShallowMap(provider.Credentials)
	latest.Credentials["access_token"] = "new-access-token-without-expiry"
	latest.Credentials["_token_version"] = time.Now().UnixMilli()
	delete(latest.Credentials, "expires_at")
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: &latest}
	cache := &grokTokenCacheForProviderTest{}
	tokenSource := newGrokTokenSourceForTest(repo, cache)

	token, err := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))

	require.ErrorIs(t, err, providercore.ErrGrokOAuthAccessTokenExpired)
	require.Empty(t, token)
}

// TestGrokTokenProviderManualTestBypassesSchedulingGate 复现 #4598：管理员必须
// 能对调度器当前排除的提供商（手动关闭、限流、过载或临时冷却）运行“测试连接”，
// 同时生产请求路径仍应拒绝这些提供商。
func TestGrokTokenProviderManualTestBypassesSchedulingGate(t *testing.T) {
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name   string
		mutate func(*providercore.Record)
	}{
		{name: "not schedulable", mutate: func(provider *providercore.Record) { provider.Schedulable = false }},
		{name: "temporarily unschedulable", mutate: func(provider *providercore.Record) { provider.TempUnschedulableUntil = &future }},
		{name: "rate limited", mutate: func(provider *providercore.Record) { provider.RateLimitResetAt = &future }},
		{name: "overloaded", mutate: func(provider *providercore.Record) { provider.OverloadUntil = &future }},
		{name: "disabled by error", mutate: func(provider *providercore.Record) { provider.Status = providercore.StatusError }},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := expiredGrokOAuthProviderForCredentialTest(int64(120 + index))
			provider.Credentials["access_token"] = "still-valid-token"
			provider.Credentials["expires_at"] = time.Now().Add(2 * providercore.GrokTokenRefreshSkew).UTC().Format(time.RFC3339)
			tt.mutate(provider)
			tokenSource := newGrokTokenSourceForTest(&tokenRefreshProviderRepo{}, &grokTokenCacheForProviderTest{})

			_, requestErr := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))
			require.ErrorIs(t, requestErr, providercore.ErrRefreshProviderStateChanged)

			token, err := tokenSource.GetAccessTokenForManualTest(context.Background(), providercore.CloneRecord(provider))
			require.NoError(t, err)
			require.Equal(t, "still-valid-token", token)
		})
	}
}

func TestGrokTokenProviderManualTestRefreshesExpiredTokenWhileUnschedulable(t *testing.T) {
	t.Setenv(xai.EnvBaseURL, xai.DefaultCLIBaseURL)

	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	provider := &providercore.Record{
		ID:          130,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiredAt,
			"base_url":      xai.DefaultCLIBaseURL,
			"client_id":     "client-id",
		},
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{130: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	oauthSvc := newGrokAuthorizationForTest(nil, &grokOAuthClientStub{
		refreshResponse: &xai.TokenResponse{
			AccessToken: "manual-test-refreshed-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
	})
	oauthSvc.Start()
	defer stopGrokAuthorizationForTest(t, oauthSvc)

	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), providercore.NewGrokTokenRefresher(oauthSvc))

	token, err := tokenSource.GetAccessTokenForManualTest(context.Background(), providercore.CloneRecord(provider))
	require.NoError(t, err)
	require.Equal(t, "manual-test-refreshed-token", token)
	require.Equal(t, 1, repo.updateCredentialsCalls)
}

func TestGrokTokenProviderManualTestFallsBackToValidTokenOnRefreshFailure(t *testing.T) {
	provider := &providercore.Record{
		ID:          131,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"access_token":  "near-expiry-token",
			"refresh_token": "refresh-token",

			"expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		},
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{131: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{
		err: errors.New("upstream refresh unavailable"),
	})

	token, err := tokenSource.GetAccessTokenForManualTest(context.Background(), providercore.CloneRecord(provider))
	require.NoError(t, err)
	require.Equal(t, "near-expiry-token", token)
}

func TestGrokTokenProviderManualTestReportsRefreshFailureWhenTokenExpired(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(132)
	provider.Schedulable = false
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{
		err: errors.New("invalid_client: client credentials rejected"),
	})

	token, err := tokenSource.GetAccessTokenForManualTest(context.Background(), providercore.CloneRecord(provider))
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "invalid_client")
}

func TestGrokTokenProviderManualTestLockHeldWithExpiredTokenReturnsSpecificError(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(133)
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	cache := &grokTokenCacheForProviderTest{lockResult: false}
	tokenSource := newGrokTokenSourceForTest(repo, cache)
	bindGrokRefreshForTest(tokenSource, newRefreshAPI(repo, cache), &tokenRefresherStub{})

	token, err := tokenSource.GetAccessTokenForManualTest(context.Background(), providercore.CloneRecord(provider))
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "refresh is already in progress")
}

func TestGrokTokenProviderManualTestRequiresRefreshToken(t *testing.T) {
	provider := expiredGrokOAuthProviderForCredentialTest(134)
	delete(provider.Credentials, "refresh_token")
	tokenSource := newGrokTokenSourceForTest(&tokenRefreshProviderRepo{}, &grokTokenCacheForProviderTest{})

	token, err := tokenSource.GetAccessTokenForManualTest(context.Background(), providercore.CloneRecord(provider))
	require.ErrorIs(t, err, providercore.ErrGrokOAuthRefreshTokenMissing)
	require.Empty(t, token)
}

func TestGrokTokenProviderRejectsIneligibleSelectedProviderBeforeWarmCache(t *testing.T) {
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name   string
		mutate func(*providercore.Record)
	}{
		{name: "disabled", mutate: func(provider *providercore.Record) { provider.Status = billing.StatusDisabled }},
		{name: "not schedulable", mutate: func(provider *providercore.Record) { provider.Schedulable = false }},
		{name: "temporarily unschedulable", mutate: func(provider *providercore.Record) { provider.TempUnschedulableUntil = &future }},
		{name: "rate limited", mutate: func(provider *providercore.Record) { provider.RateLimitResetAt = &future }},
		{name: "overloaded", mutate: func(provider *providercore.Record) { provider.OverloadUntil = &future }},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := expiredGrokOAuthProviderForCredentialTest(int64(90 + index))
			provider.Credentials["access_token"] = "warm-cache-token"
			provider.Credentials["expires_at"] = time.Now().Add(2 * providercore.GrokTokenRefreshSkew).UTC().Format(time.RFC3339)
			tt.mutate(provider)
			cache := &grokTokenCacheForProviderTest{token: "warm-cache-token"}
			tokenSource := newGrokTokenSourceForTest(&tokenRefreshProviderRepo{}, cache)

			token, err := tokenSource.GetAccessToken(context.Background(), providercore.CloneRecord(provider))

			require.ErrorIs(t, err, providercore.ErrRefreshProviderStateChanged)
			require.Empty(t, token)
			require.Zero(t, cache.getCalls, "an ineligible selected provider must be rejected before cache lookup")
		})
	}
}

// newGrokTokenSourceForTest 组合提供商读取、缓存和 token 策略。
func newGrokTokenSourceForTest(repo providercore.RefreshRepository, cache providercore.AccessTokenCache) *providercore.GrokTokenSource {
	return &providercore.GrokTokenSource{Repository: repo, Cache: cache, Policy: providercore.GrokProviderRefreshPolicy()}
}

// bindGrokRefreshForTest 注入共享刷新协调器，锁和持久化由协调器管理。
func bindGrokRefreshForTest(source *providercore.GrokTokenSource, refresh *providercore.OAuthRefreshAPI, executor providercore.OAuthRefreshExecutor) {
	source.Refresh = func(ctx context.Context, value *providercore.Record, window time.Duration) (*providercore.OAuthRefreshResult, error) {
		return refresh.RefreshIfNeeded(ctx, value, executor, window)
	}
}

func expiredGrokOAuthProviderForCredentialTest(id int64) *providercore.Record {
	return &providercore.Record{
		ID:          id,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			"base_url":      xai.DefaultCLIBaseURL,
		},
	}
}
