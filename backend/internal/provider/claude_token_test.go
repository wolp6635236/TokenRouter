package provider

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// claudeTokenCacheStub 记录 Claude 令牌缓存操作。
type claudeTokenCacheStub struct {
	mu               sync.Mutex
	tokens           map[string]string
	getErr           error
	setErr           error
	deleteErr        error
	lockAcquired     bool
	lockErr          error
	releaseLockErr   error
	getCalled        int32
	setCalled        int32
	lockCalled       int32
	unlockCalled     int32
	simulateLockRace bool
}

// claudeProviderRepoStub 模拟 Claude 令牌来源所需的存储操作。
type claudeProviderRepoStub struct {
	provider     *Record
	getErr       error
	updateErr    error
	getCalled    int32
	updateCalled int32
}

// claudeOAuthServiceStub 返回测试设置的 Claude 令牌交换结果。
type claudeOAuthServiceStub struct {
	tokenInfo     *ClaudeTokenInfo
	refreshErr    error
	refreshCalled int32
}

// Claude 执行器注入测试交换结果，并调用凭据资格判断和合并函数。
type claudeExchangeFixture struct{ exchange *claudeOAuthServiceStub }

func newClaudeTokenCacheStub() *claudeTokenCacheStub {
	return &claudeTokenCacheStub{
		tokens:       make(map[string]string),
		lockAcquired: true,
	}
}

func (s *claudeTokenCacheStub) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	atomic.AddInt32(&s.getCalled, 1)
	if s.getErr != nil {
		return "", s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[cacheKey], nil
}

func (s *claudeTokenCacheStub) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	atomic.AddInt32(&s.setCalled, 1)
	if s.setErr != nil {
		return s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[cacheKey] = token
	return nil
}

func (s *claudeTokenCacheStub) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, cacheKey)
	return nil
}

func (s *claudeTokenCacheStub) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	atomic.AddInt32(&s.lockCalled, 1)
	if s.lockErr != nil {
		return false, s.lockErr
	}
	if s.simulateLockRace {
		return false, nil
	}
	return s.lockAcquired, nil
}

func (s *claudeTokenCacheStub) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	atomic.AddInt32(&s.unlockCalled, 1)
	return s.releaseLockErr
}

func (r *claudeProviderRepoStub) GetByID(ctx context.Context, id int64) (*Record, error) {
	atomic.AddInt32(&r.getCalled, 1)
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.provider, nil
}

func (r *claudeProviderRepoStub) Update(ctx context.Context, provider *Record) error {
	atomic.AddInt32(&r.updateCalled, 1)
	if r.updateErr != nil {
		return r.updateErr
	}
	r.provider = provider
	return nil
}

func (s *claudeOAuthServiceStub) RefreshProviderToken(ctx context.Context, provider *Record) (*ClaudeTokenInfo, error) {
	atomic.AddInt32(&s.refreshCalled, 1)
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return s.tokenInfo, nil
}

func TestClaudeTokenProvider_TokenRefresh(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{}
	oauthService := &claudeOAuthServiceStub{
		tokenInfo: &ClaudeTokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh-token",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// 令牌处于刷新提前量内。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       102,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
	}
	providerRepo.provider = provider

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, oauthService)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "refreshed-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&oauthService.refreshCalled))
}

func TestClaudeTokenProvider_LockRaceCondition(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.simulateLockRace = true
	providerRepo := &claudeProviderRepoStub{}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       103,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "race-token",
			"expires_at":   expiresAt,
		},
	}
	providerRepo.provider = provider

	// 模拟其他 worker 已完成刷新并写入缓存。
	cacheKey := ClaudeTokenCacheKey(provider)
	go func() {
		time.Sleep(5 * time.Millisecond)
		cache.mu.Lock()
		cache.tokens[cacheKey] = "winner-token"
		cache.mu.Unlock()
	}()

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, nil)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestClaudeTokenProvider_RefreshError(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{}
	oauthService := &claudeOAuthServiceStub{
		refreshErr: errors.New("oauth refresh failed"),
	}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       111,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
		},
	}
	providerRepo.provider = provider

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, oauthService)

	// 刷新失败时返回已有令牌。
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "old-token", token) // Fallback to existing token
}

func TestClaudeTokenProvider_OAuthServiceNotConfigured(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       112,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "old-token",
			"expires_at":   expiresAt,
		},
	}
	providerRepo.provider = provider

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, nil)

	// 未配置 OAuth 服务时返回已有令牌。
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "old-token", token) // Fallback to existing token
}

func TestClaudeTokenProvider_ProviderRepoGetError(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{
		getErr: errors.New("db connection failed"),
	}
	oauthService := &claudeOAuthServiceStub{
		tokenInfo: &ClaudeTokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       113,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh",
			"expires_at":    expiresAt,
		},
	}

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, oauthService)

	// 刷新协调器读取失败时，调用方使用已有 token。
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "old-token", token)
	require.Zero(t, atomic.LoadInt32(&oauthService.refreshCalled))
	require.Zero(t, atomic.LoadInt32(&providerRepo.updateCalled))
	require.Equal(t, "old-refresh", provider.Credentials["refresh_token"])
}

func TestClaudeTokenProvider_ProviderUpdateError(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{
		updateErr: errors.New("db write failed"),
	}
	oauthService := &claudeOAuthServiceStub{
		tokenInfo: &ClaudeTokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       114,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-token",
			"refresh_token": "old-refresh",
			"expires_at":    expiresAt,
		},
	}
	providerRepo.provider = provider

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, oauthService)

	// 持久化失败不能发布未保存的令牌，也不能污染传入的凭据快照。
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "old-token", token)
	require.Equal(t, int32(1), atomic.LoadInt32(&oauthService.refreshCalled))
	require.Equal(t, int32(1), atomic.LoadInt32(&providerRepo.updateCalled))
	require.Equal(t, "old-token", providerRepo.provider.Credentials["access_token"])
	require.Equal(t, "old-refresh", provider.Credentials["refresh_token"])
}

func TestClaudeTokenProvider_RefreshPreservesExistingCredentials(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{}
	oauthService := &claudeOAuthServiceStub{
		tokenInfo: &ClaudeTokenInfo{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       115,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    expiresAt,
			"custom_field":  "should-be-preserved",
			"organization":  "test-org",
		},
	}
	providerRepo.provider = provider

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, oauthService)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, "new-access-token", token)

	// 检查已有凭据字段。
	require.Equal(t, "should-be-preserved", providerRepo.provider.Credentials["custom_field"])
	require.Equal(t, "test-org", providerRepo.provider.Credentials["organization"])
	// 检查交换结果中的凭据字段。
	require.Equal(t, "new-access-token", providerRepo.provider.Credentials["access_token"])
	require.Equal(t, "new-refresh-token", providerRepo.provider.Credentials["refresh_token"])
}

func TestClaudeTokenProvider_DoubleCheckCacheAfterLock(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	providerRepo := &claudeProviderRepoStub{}
	oauthService := &claudeOAuthServiceStub{
		tokenInfo: &ClaudeTokenInfo{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	}

	// 令牌即将到期。
	expiresAt := time.Now().Add(1 * time.Minute).Format(time.RFC3339)
	provider := &Record{
		ID:       116,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "old-token",
			"expires_at":   expiresAt,
		},
	}
	providerRepo.provider = provider
	cacheKey := ClaudeTokenCacheKey(provider)

	// 模拟其他 worker 在取得锁之前写入令牌缓存。
	go func() {
		time.Sleep(5 * time.Millisecond)
		cache.mu.Lock()
		cache.tokens[cacheKey] = "cached-by-other-worker"
		cache.mu.Unlock()
	}()

	tokenSource := newClaudeRefreshSourceFixture(providerRepo, cache, oauthService)

	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

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
	// 令牌距离到期时间较远，读取时跳过刷新。
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

	// 令牌应已写入缓存。
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
	// 令牌处于有效期内。
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

	// 令牌处于有效期内。
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

	// 缓存读取失败时，从凭据返回令牌。
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

	// 缓存写入失败时仍返回令牌。
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
			// 缺少 access_token。
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

			// 检查缓存中的令牌。
			cacheKey := ClaudeTokenCacheKey(provider)
			require.Equal(t, "test-token", cache.tokens[cacheKey])
		})
	}
}

// TestClaudeTokenProvider_Real_LockFailedWait 检查 Claude token 源获取锁失败后的等待行为。
func TestClaudeTokenProvider_Real_LockFailedWait(t *testing.T) {
	cache := newClaudeTokenCacheStub()
	cache.lockAcquired = false // Lock acquisition fails

	// 令牌处于刷新提前量内，触发取锁。
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

	// 等待刷新锁期间，模拟其他 worker 写入令牌缓存。
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

	// 令牌即将到期。
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
	// 开始等待后立即写入令牌缓存。
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

	// 令牌未设置 expires_at。
	provider := &Record{
		ID:       302,
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token": "no-expiry-token",
		},
	}

	// 等待锁结束后，从凭据返回令牌。
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

	// 令牌处于刷新提前量内。
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
			// 凭据中缺少 access_token。
		},
	}

	tokenSource := newClaudeTokenSourceContract(cache)
	token, err := tokenSource.GetAccessToken(context.Background(), provider)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token not found")
	require.Empty(t, token)
}

// newClaudeTokenSourceContract 原缓存与等待断言直接执行提供商 token 用例。
func newClaudeTokenSourceContract(cache AccessTokenCache) *ClaudeTokenSource {
	return &ClaudeTokenSource{Options: ClaudeTokenOptions{
		Cache: cache, Policy: ClaudeProviderRefreshPolicy(), Debug: slog.Debug, Warn: slog.Warn,
	}}
}

func newClaudeRefreshSourceFixture(repo *claudeProviderRepoStub, cache *claudeTokenCacheStub, authorization *claudeOAuthServiceStub) *ClaudeTokenSource {
	source := &ClaudeTokenSource{Options: ClaudeTokenOptions{Repository: repo, Cache: cache, Policy: ClaudeProviderRefreshPolicy(), Debug: slog.Debug, Warn: slog.Warn}}
	if authorization != nil {
		api := NewOAuthRefreshAPI(repo, cache, RefreshOptions{Platform: ProviderRefreshPlatformPolicy()})
		executor := claudeExchangeFixture{authorization}
		source.Options.Refresh = func(ctx context.Context, value *Record, window time.Duration) (*OAuthRefreshResult, error) {
			return api.RefreshIfNeeded(ctx, value, executor, window)
		}
	}
	return source
}

func (claudeExchangeFixture) CanRefresh(value *Record) bool { return CanRefreshClaude(value) }

func (claudeExchangeFixture) NeedsRefresh(value *Record, window time.Duration) bool {
	return NeedsRefreshClaude(value, window)
}

func (claudeExchangeFixture) CacheKey(value *Record) string { return ClaudeTokenCacheKey(value) }

func (e claudeExchangeFixture) Refresh(ctx context.Context, value *Record) (map[string]any, error) {
	return RefreshClaudeCredentials(ctx, value, e.exchange.RefreshProviderToken)
}

func (r *claudeProviderRepoStub) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, version CredentialVersion, credentials map[string]any) (bool, error) {
	if r.provider == nil || !reflect.DeepEqual(FailureVersion(r.provider).CredentialVersion, version) {
		return false, nil
	}
	next := CloneRecord(r.provider)
	next.Credentials = CloneValues(credentials)
	err := r.Update(ctx, next)
	return err == nil, err
}

func TestClaudeTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected string
	}{
		{
			name: "basic_provider",
			provider: &Record{
				ID: 400,
			},
			expected: "claude:provider:400",
		},
		{
			name: "provider_with_credentials",
			provider: &Record{
				ID: 401,
				Credentials: map[string]any{
					"access_token": "claude-token",
				},
			},
			expected: "claude:provider:401",
		},
		{
			name: "provider_id_zero",
			provider: &Record{
				ID: 0,
			},
			expected: "claude:provider:0",
		},
		{
			name: "large_provider_id",
			provider: &Record{
				ID: 9999999999,
			},
			expected: "claude:provider:9999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClaudeTokenCacheKey(tt.provider)
			require.Equal(t, tt.expected, result)
		})
	}
}
