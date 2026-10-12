package apikey

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
)

type authRepoStub struct {
	getByKeyForAuth   func(ctx context.Context, key string) (*APIKey, error)
	listKeysByUserID  func(ctx context.Context, userID int64) ([]string, error)
	listKeysByGroupID func(ctx context.Context, groupID int64) ([]string, error)
}

type authCacheStub struct {
	getAuthCache   func(ctx context.Context, key string) (*APIKeyAuthCacheEntry, error)
	setAuthKeys    []string
	deleteAuthKeys []string
}

// apiKeyRepoStub 是 APIKeyRepository 接口的测试桩实现。
// APIKeyService.Delete 的测试通过它设置仓储返回值并记录删除操作。
//
// 设计说明：
//   - apiKey/getByIDErr: 模拟 GetKeyAndOwnerID 返回的记录与错误
//   - deleteErr: 模拟 Delete 返回的错误
//   - deletedIDs: 记录被调用删除的 API Key ID，用于断言验证
type apiKeyRepoStub struct {
	apiKey              *APIKey // 轻量查询返回的记录
	getByIDErr          error   // 轻量查询的错误返回值
	deleteErr           error   // 删除操作的错误返回值
	updateErr           error   // 更新操作的错误返回值
	deletedIDs          []int64 // 记录已删除的密钥编号列表
	updatedKeys         []APIKey
	allowListByUserID   bool
	listByUserIDKeys    []APIKey
	listByUserIDErr     error
	listByUserIDCalls   []int64
	listByUserIDParams  []pagination.PaginationParams
	listByUserIDFilters []APIKeyListFilters
	updateLastUsed      func(ctx context.Context, id int64, usedAt time.Time) error
	touchedIDs          []int64
	touchedUsedAts      []time.Time
}

// apiKeyCacheStub 是 APIKeyCache 接口的测试桩实现。
// 用于验证删除操作时缓存清理逻辑是否被正确调用。
//
// 设计说明：
//   - invalidated: 记录被清除缓存的用户 ID 列表
type apiKeyCacheStub struct {
	invalidated    []int64  // 记录调用 DeleteCreateAttemptCount 时传入的用户 ID
	deleteAuthKeys []string // 记录调用 DeleteAuthCache 时传入的缓存 key
}

type touchSingleflightRepo struct {
	*apiKeyRepoStub
	mu      sync.Mutex
	calls   int
	blockCh chan struct{}
}

// dailyUsageCalendarCache 记录日期计算用到的计数和 TTL。
type dailyUsageCalendarCache struct {
	APIKeyCache
	key string
	ttl time.Duration
}

func TestAPIKeyService_GetByKey_UsesL1Cache(t *testing.T) {
	var calls int32
	cache := &authCacheStub{}
	repo := &authRepoStub{
		getByKeyForAuth: func(ctx context.Context, key string) (*APIKey, error) {
			atomic.AddInt32(&calls, 1)
			return &APIKey{
				ID:     21,
				UserID: 3,
				Status: StatusActive,
				User: &User{
					ID:          3,
					Status:      StatusActive,
					Role:        "user",
					Balance:     5,
					Concurrency: 2,
				},
			}, nil
		},
	}
	cfg := &Options{
		APIKeyAuth: APIKeyAuthCacheConfig{
			L1Size:       1000,
			L1TTLSeconds: 60,
		},
	}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, cfg)
	svc.Start()
	require.NotNil(t, svc.authCacheL1.Load())

	_, err := svc.GetByKey(context.Background(), "k-l1")
	require.NoError(t, err)
	svc.authCacheL1.Load().Wait()
	cacheKey := svc.KeyAuthCacheKey("k-l1")
	_, ok := svc.authCacheL1.Load().Get(cacheKey)
	require.True(t, ok)
	_, err = svc.GetByKey(context.Background(), "k-l1")
	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestAPIKeyService_GetByKey_CachesNegativeOnRepoMiss(t *testing.T) {
	var repoCalls atomic.Int32
	cache := &authCacheStub{}
	repo := &authRepoStub{
		getByKeyForAuth: func(ctx context.Context, key string) (*APIKey, error) {
			repoCalls.Add(1)
			return nil, ErrAPIKeyNotFound
		},
	}
	cfg := &Options{
		APIKeyAuth: APIKeyAuthCacheConfig{
			L1Size:             100,
			L1TTLSeconds:       60,
			L2TTLSeconds:       60,
			NegativeTTLSeconds: 30,
		},
	}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, cfg)
	svc.Start()
	cache.getAuthCache = func(ctx context.Context, key string) (*APIKeyAuthCacheEntry, error) {
		return nil, redis.Nil
	}

	_, err := svc.GetByKey(context.Background(), "missing")
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
	require.Empty(t, cache.setAuthKeys, "attacker-controlled misses must not be written to Redis")
	svc.authNegativeCacheL1.Load().Wait()
	_, err = svc.GetByKey(context.Background(), "missing")
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
	require.Equal(t, int32(1), repoCalls.Load())
}

// TestApiKeyService_Delete_Success 测试所有者成功删除 API Key 的场景。
// 预期行为：
//   - GetKeyAndOwnerID 返回所有者 ID 为 7
//   - 调用者 userID 为 7（匹配）
//   - Delete 成功执行
//   - 缓存被正确清除（使用 ownerID）
//   - 返回 nil 错误
func TestApiKeyService_Delete_Success(t *testing.T) {
	repo := &apiKeyRepoStub{
		apiKey: &APIKey{ID: 42, UserID: 7, Key: "k"},
	}
	cache := &apiKeyCacheStub{}
	svc := &APIKeyService{apiKeyRepo: repo, cache: cache}
	svc.lastUsedTouchL1.Store(int64(42), time.Now())

	err := svc.Delete(context.Background(), 42, 7) // API Key ID=42, 调用者 userID=7
	require.NoError(t, err)
	require.Equal(t, []int64{42}, repo.deletedIDs)  // 验证正确的 API Key 被删除
	require.Equal(t, []int64{7}, cache.invalidated) // 验证所有者的缓存被清除
	require.Equal(t, []string{svc.KeyAuthCacheKey("k")}, cache.deleteAuthKeys)
	_, exists := svc.lastUsedTouchL1.Load(int64(42))
	require.False(t, exists, "delete should clear touch debounce cache")
}

func TestAPIKeyService_TouchLastUsed_InvalidKeyID(t *testing.T) {
	repo := &apiKeyRepoStub{
		updateLastUsed: func(ctx context.Context, id int64, usedAt time.Time) error {
			return errors.New("should not be called")
		},
	}
	svc := &APIKeyService{apiKeyRepo: repo}

	require.NoError(t, svc.TouchLastUsed(context.Background(), 0))
	require.NoError(t, svc.TouchLastUsed(context.Background(), -1))
	require.Empty(t, repo.touchedIDs)
}

func TestAPIKeyService_TouchLastUsed_FirstTouchSucceeds(t *testing.T) {
	repo := &apiKeyRepoStub{}
	svc := &APIKeyService{apiKeyRepo: repo}

	err := svc.TouchLastUsed(context.Background(), 123)
	require.NoError(t, err)
	require.Equal(t, []int64{123}, repo.touchedIDs)
	require.Len(t, repo.touchedUsedAts, 1)
	require.False(t, repo.touchedUsedAts[0].IsZero())

	cached, ok := svc.lastUsedTouchL1.Load(int64(123))
	require.True(t, ok, "successful touch should update debounce cache")
	_, isTime := cached.(time.Time)
	require.True(t, isTime)
}

func TestAPIKeyService_TouchLastUsed_DebouncedWithinWindow(t *testing.T) {
	repo := &apiKeyRepoStub{}
	svc := &APIKeyService{apiKeyRepo: repo}

	require.NoError(t, svc.TouchLastUsed(context.Background(), 123))
	require.NoError(t, svc.TouchLastUsed(context.Background(), 123))

	require.Equal(t, []int64{123}, repo.touchedIDs, "second touch within debounce window should not hit repository")
}

func TestAPIKeyService_TouchLastUsed_ExpiredDebounceTouchesAgain(t *testing.T) {
	repo := &apiKeyRepoStub{}
	svc := &APIKeyService{apiKeyRepo: repo}

	require.NoError(t, svc.TouchLastUsed(context.Background(), 123))

	// 强制将 debounce 时间回拨到窗口之外，触发第二次写库。
	svc.lastUsedTouchL1.Store(int64(123), time.Now().Add(-KeyApiKeyLastUsedMinTouch-time.Second))

	require.NoError(t, svc.TouchLastUsed(context.Background(), 123))
	require.Len(t, repo.touchedIDs, 2)
	require.Equal(t, int64(123), repo.touchedIDs[0])
	require.Equal(t, int64(123), repo.touchedIDs[1])
}

func TestAPIKeyService_TouchLastUsed_RepoError(t *testing.T) {
	repo := &apiKeyRepoStub{
		updateLastUsed: func(ctx context.Context, id int64, usedAt time.Time) error {
			return errors.New("db write failed")
		},
	}
	svc := &APIKeyService{apiKeyRepo: repo}

	err := svc.TouchLastUsed(context.Background(), 123)
	require.Error(t, err)
	require.ErrorContains(t, err, "touch api key last used")
	require.Equal(t, []int64{123}, repo.touchedIDs)

	cached, ok := svc.lastUsedTouchL1.Load(int64(123))
	require.True(t, ok, "failed touch should still update retry debounce cache")
	_, isTime := cached.(time.Time)
	require.True(t, isTime)
}

func TestAPIKeyService_TouchLastUsed_RepoErrorDebounced(t *testing.T) {
	repo := &apiKeyRepoStub{
		updateLastUsed: func(ctx context.Context, id int64, usedAt time.Time) error {
			return errors.New("db write failed")
		},
	}
	svc := &APIKeyService{apiKeyRepo: repo}

	firstErr := svc.TouchLastUsed(context.Background(), 456)
	require.Error(t, firstErr)
	require.ErrorContains(t, firstErr, "touch api key last used")

	secondErr := svc.TouchLastUsed(context.Background(), 456)
	require.NoError(t, secondErr, "failed touch should be debounced and skip immediate retry")
	require.Equal(t, []int64{456}, repo.touchedIDs, "debounced retry should not hit repository again")
}

func TestAPIKeyService_TouchLastUsed_ConcurrentFirstTouchDeduplicated(t *testing.T) {
	repo := &touchSingleflightRepo{
		apiKeyRepoStub: &apiKeyRepoStub{},
		blockCh:        make(chan struct{}),
	}
	svc := &APIKeyService{apiKeyRepo: repo}

	const workers = 20
	startCh := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			<-startCh
			errCh <- svc.TouchLastUsed(context.Background(), 321)
		})
	}

	close(startCh)

	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.calls >= 1
	}, time.Second, 10*time.Millisecond)

	close(repo.blockCh)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.calls, "并发首次 touch 只应写库一次")
}

func TestDailyUsageCacheUsesInjectedCalendar(t *testing.T) {
	for _, location := range []*time.Location{time.FixedZone("east", 14*3600), time.FixedZone("west", -12*3600)} {
		t.Run(location.String(), func(t *testing.T) {
			calendar := timezone.NewCalendar(location)
			cache := &dailyUsageCalendarCache{}
			service := NewAPIKeyService(nil, nil, nil, nil, nil, cache, &Options{Calendar: calendar})
			before := calendar.Now().Format("2006-01-02")
			require.NoError(t, service.IncrementUsage(context.Background(), 17))
			after := calendar.Now().Format("2006-01-02")
			require.Contains(t, []string{"apikey:usage:17:" + before, "apikey:usage:17:" + after}, cache.key)
			require.Equal(t, 24*time.Hour, cache.ttl)
		})
	}
}

func (s *authRepoStub) Create(ctx context.Context, key *APIKey) error {
	panic("unexpected Create call")
}

func (s *authRepoStub) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	panic("unexpected GetByID call")
}

func (s *authRepoStub) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}

func (s *authRepoStub) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}

func (s *authRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	if s.getByKeyForAuth == nil {
		panic("unexpected GetByKeyForAuth call")
	}
	return s.getByKeyForAuth(ctx, key)
}

func (s *authRepoStub) RotateCredential(context.Context, *APIKey, string) error {
	panic("unexpected RotateCredential call")
}

func (s *authRepoStub) Update(ctx context.Context, key *APIKey, _ APIKeyUpdateFields) error {
	panic("unexpected Update call")
}

func (s *authRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}

func (s *authRepoStub) DeleteWithAudit(ctx context.Context, id int64) error {
	panic("unexpected DeleteWithAudit call")
}

func (s *authRepoStub) ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserID call")
}

func (s *authRepoStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}

func (s *authRepoStub) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	panic("unexpected CountByUserID call")
}

func (s *authRepoStub) ExistsByKey(ctx context.Context, key string) (bool, error) {
	panic("unexpected ExistsByKey call")
}

func (s *authRepoStub) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}

func (s *authRepoStub) SearchAPIKeys(ctx context.Context, userID int64, keyword string, limit int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}

func (s *authRepoStub) ClearGroupIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}

func (s *authRepoStub) UpdateGroupIDByUserAndGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}

func (s *authRepoStub) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}

func (s *authRepoStub) ListKeysByUserID(ctx context.Context, userID int64) ([]string, error) {
	if s.listKeysByUserID == nil {
		panic("unexpected ListKeysByUserID call")
	}
	return s.listKeysByUserID(ctx, userID)
}

func (s *authRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	if s.listKeysByGroupID == nil {
		panic("unexpected ListKeysByGroupID call")
	}
	return s.listKeysByGroupID(ctx, groupID)
}

func (s *authRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}

func (s *authRepoStub) UpdateLastUsed(ctx context.Context, id int64, usedAt time.Time) error {
	panic("unexpected UpdateLastUsed call")
}

func (s *authRepoStub) IncrementRateLimitUsage(ctx context.Context, id int64, cost float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}

func (s *authRepoStub) ResetRateLimitWindows(ctx context.Context, id int64) error {
	panic("unexpected ResetRateLimitWindows call")
}

func (s *authRepoStub) GetRateLimitData(ctx context.Context, id int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

func (s *authCacheStub) GetCreateAttemptCount(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

func (s *authCacheStub) IncrementCreateAttemptCount(ctx context.Context, userID int64) error {
	return nil
}

func (s *authCacheStub) DeleteCreateAttemptCount(ctx context.Context, userID int64) error {
	return nil
}

func (s *authCacheStub) IncrementDailyUsage(ctx context.Context, apiKey string) error {
	return nil
}

func (s *authCacheStub) SetDailyUsageExpiry(ctx context.Context, apiKey string, ttl time.Duration) error {
	return nil
}

func (s *authCacheStub) GetAuthCache(ctx context.Context, key string) (*APIKeyAuthCacheEntry, error) {
	if s.getAuthCache == nil {
		return nil, redis.Nil
	}
	return s.getAuthCache(ctx, key)
}

func (s *authCacheStub) SetAuthCache(ctx context.Context, key string, entry *APIKeyAuthCacheEntry, ttl time.Duration) error {
	s.setAuthKeys = append(s.setAuthKeys, key)
	return nil
}

func (s *authCacheStub) DeleteAuthCache(ctx context.Context, key string) error {
	s.deleteAuthKeys = append(s.deleteAuthKeys, key)
	return nil
}

func (s *authCacheStub) PublishAuthCacheInvalidation(ctx context.Context, cacheKey string) error {
	return nil
}

func (s *authCacheStub) SubscribeAuthCacheInvalidation(ctx context.Context, handler func(cacheKey string)) error {
	return nil
}

func (s *apiKeyRepoStub) Create(ctx context.Context, key *APIKey) error {
	panic("unexpected Create call")
}

func (s *apiKeyRepoStub) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	if s.apiKey != nil {
		clone := *s.apiKey
		return &clone, nil
	}
	panic("unexpected GetByID call")
}

func (s *apiKeyRepoStub) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	if s.getByIDErr != nil {
		return "", 0, s.getByIDErr
	}
	if s.apiKey != nil {
		return s.apiKey.Key, s.apiKey.UserID, nil
	}
	return "", 0, ErrAPIKeyNotFound
}

func (s *apiKeyRepoStub) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}

func (s *apiKeyRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}

func (s *apiKeyRepoStub) RotateCredential(context.Context, *APIKey, string) error {
	panic("unexpected RotateCredential call")
}

func (s *apiKeyRepoStub) Update(ctx context.Context, key *APIKey, _ APIKeyUpdateFields) error {
	if key != nil {
		s.updatedKeys = append(s.updatedKeys, *key)
	}
	return s.updateErr
}

// Delete 记录被删除的 API Key ID 并返回预设的错误。
// 通过 deletedIDs 可以验证删除操作是否被正确调用。
func (s *apiKeyRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

// DeleteWithAudit 与 Delete 一样记录被删除的 ID,供 service 测试断言。
func (s *apiKeyRepoStub) DeleteWithAudit(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

func (s *apiKeyRepoStub) ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	if !s.allowListByUserID {
		panic("unexpected ListByUserID call")
	}
	s.listByUserIDCalls = append(s.listByUserIDCalls, userID)
	s.listByUserIDParams = append(s.listByUserIDParams, params)
	s.listByUserIDFilters = append(s.listByUserIDFilters, filters)
	if s.listByUserIDErr != nil {
		return nil, nil, s.listByUserIDErr
	}
	keys := append([]APIKey(nil), s.listByUserIDKeys...)
	return keys, &pagination.PaginationResult{
		Total:    int64(len(keys)),
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    1,
	}, nil
}

func (s *apiKeyRepoStub) VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}

func (s *apiKeyRepoStub) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	panic("unexpected CountByUserID call")
}

func (s *apiKeyRepoStub) ExistsByKey(ctx context.Context, key string) (bool, error) {
	panic("unexpected ExistsByKey call")
}

func (s *apiKeyRepoStub) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}

func (s *apiKeyRepoStub) SearchAPIKeys(ctx context.Context, userID int64, keyword string, limit int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}

func (s *apiKeyRepoStub) ClearGroupIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}

func (s *apiKeyRepoStub) UpdateGroupIDByUserAndGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}

func (s *apiKeyRepoStub) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}

func (s *apiKeyRepoStub) ListKeysByUserID(ctx context.Context, userID int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}

func (s *apiKeyRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}

func (s *apiKeyRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}

func (s *apiKeyRepoStub) UpdateLastUsed(ctx context.Context, id int64, usedAt time.Time) error {
	s.touchedIDs = append(s.touchedIDs, id)
	s.touchedUsedAts = append(s.touchedUsedAts, usedAt)
	if s.updateLastUsed != nil {
		return s.updateLastUsed(ctx, id, usedAt)
	}
	return nil
}

func (s *apiKeyRepoStub) IncrementRateLimitUsage(ctx context.Context, id int64, cost float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}

func (s *apiKeyRepoStub) ResetRateLimitWindows(ctx context.Context, id int64) error {
	panic("unexpected ResetRateLimitWindows call")
}

func (s *apiKeyRepoStub) GetRateLimitData(ctx context.Context, id int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

// GetCreateAttemptCount 返回 0，表示用户未超过创建次数限制。
func (s *apiKeyCacheStub) GetCreateAttemptCount(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

// IncrementCreateAttemptCount 空实现，本测试不验证此行为。
func (s *apiKeyCacheStub) IncrementCreateAttemptCount(ctx context.Context, userID int64) error {
	return nil
}

// DeleteCreateAttemptCount 记录被清除缓存的用户 ID。
// 删除 API Key 时会调用此方法清除用户的创建尝试计数缓存。
func (s *apiKeyCacheStub) DeleteCreateAttemptCount(ctx context.Context, userID int64) error {
	s.invalidated = append(s.invalidated, userID)
	return nil
}

// IncrementDailyUsage 空实现，本测试不验证此行为。
func (s *apiKeyCacheStub) IncrementDailyUsage(ctx context.Context, apiKey string) error {
	return nil
}

// SetDailyUsageExpiry 空实现，本测试不验证此行为。
func (s *apiKeyCacheStub) SetDailyUsageExpiry(ctx context.Context, apiKey string, ttl time.Duration) error {
	return nil
}

func (s *apiKeyCacheStub) GetAuthCache(ctx context.Context, key string) (*APIKeyAuthCacheEntry, error) {
	return nil, nil
}

func (s *apiKeyCacheStub) SetAuthCache(ctx context.Context, key string, entry *APIKeyAuthCacheEntry, ttl time.Duration) error {
	return nil
}

func (s *apiKeyCacheStub) DeleteAuthCache(ctx context.Context, key string) error {
	s.deleteAuthKeys = append(s.deleteAuthKeys, key)
	return nil
}

func (s *apiKeyCacheStub) PublishAuthCacheInvalidation(ctx context.Context, cacheKey string) error {
	return nil
}

func (s *apiKeyCacheStub) SubscribeAuthCacheInvalidation(ctx context.Context, handler func(cacheKey string)) error {
	return nil
}

func (r *touchSingleflightRepo) UpdateLastUsed(ctx context.Context, id int64, usedAt time.Time) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	<-r.blockCh
	return nil
}

func (c *dailyUsageCalendarCache) IncrementDailyUsage(_ context.Context, key string) error {
	c.key = key
	return nil
}

func (c *dailyUsageCalendarCache) SetDailyUsageExpiry(_ context.Context, key string, ttl time.Duration) error {
	if c.key != key {
		return fmt.Errorf("expiry key differs from counter key")
	}
	c.ttl = ttl
	return nil
}
