package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
)

const (
	cacheWriteSetBalance cacheWriteKind = iota
	cacheWriteDeductBalance
	cacheWriteUpdateRateLimitUsage

	// 异步缓存写入工作池配置
	//
	// 固定大小的工作池限制并发写入：
	// 1. 预创建 10 个 worker goroutine，避免频繁创建销毁
	// 2. 使用带缓冲的 Go 通道（1000）作为任务队列，平滑写入峰值
	// 3. 非阻塞写入，队列满时关键任务同步回退，非关键任务丢弃并告警
	// 4. 统一超时控制，避免慢操作阻塞工作池。
	cacheWriteWorkerCount     = 10              // 工作协程数量
	cacheWriteBufferSize      = 1000            // 任务队列缓冲大小
	cacheWriteTimeout         = 2 * time.Second // 单个写入操作超时
	cacheWriteDropLogInterval = 5 * time.Second // 丢弃日志节流间隔
	balanceLoadTimeout        = 3 * time.Second
)

//nolint:decorder // iota 按块内序号计数，每个枚举需要独立的 const 块。
const (
	cacheWriteEnqueued cacheWriteEnqueueResult = iota
	cacheWriteQueueFull
	cacheWriteQueueClosed
)

//nolint:decorder // iota 按块内序号计数，每个枚举需要独立的 const 块。
const (
	billingCircuitClosed billingCircuitBreakerState = iota
	billingCircuitOpen
	billingCircuitHalfOpen
)

var (
	// 错误定义
	// 注：ErrInsufficientBalance 在 redeem_service.go 中定义
	// 注：ErrDailyLimitExceeded/ErrWeeklyLimitExceeded/ErrMonthlyLimitExceeded 在 subscription_service.go 中定义。
	ErrBillingServiceUnavailable = apperror.ServiceUnavailable("BILLING_SERVICE_ERROR", "Billing service temporarily unavailable. Please retry later.")

	ErrAPIKeyNotFound                    = apperror.NotFound("API_KEY_NOT_FOUND", "api key not found")
	ErrAPIKeyQuotaExhausted              = apperror.TooManyRequests("API_KEY_QUOTA_EXHAUSTED", "api key quota exhausted")
	ErrAPIKeyRateLimit1dExceeded         = apperror.TooManyRequests("API_KEY_RATE_1D_EXCEEDED", "The API key daily limit has been reached")
	ErrAPIKeyRateLimit5hExceeded         = apperror.TooManyRequests("API_KEY_RATE_5H_EXCEEDED", "The API key five-hour limit has been reached")
	ErrAPIKeyRateLimit7dExceeded         = apperror.TooManyRequests("API_KEY_RATE_7D_EXCEEDED", "The API key seven-day limit has been reached")
	ErrProviderNotFound                  = apperror.NotFound("PROVIDER_NOT_FOUND", "provider not found")
	ErrPreferredSubscriptionInsufficient = apperror.TooManyRequests("PREFERRED_SUBSCRIPTION_EXHAUSTED", "preferred subscription has insufficient remaining quota")
	ErrPreferredSubscriptionInvalid      = apperror.Forbidden("PREFERRED_SUBSCRIPTION_INVALID", "preferred subscription is unavailable")
	ErrUserNotFound                      = apperror.NotFound("USER_NOT_FOUND", "user not found")
	ErrInsufficientBalance               = apperror.BadRequest("INSUFFICIENT_BALANCE", "insufficient balance")
	ErrPreferredSubscriptionGroup        = apperror.Forbidden("PREFERRED_SUBSCRIPTION_GROUP_NOT_ALLOWED", "preferred subscription does not allow this group")
)

// 缓存写入任务类型。
type cacheWriteKind int

type cacheWriteEnqueueResult int

// cacheWriteTask 缓存写入任务。
type cacheWriteTask struct {
	kind     cacheWriteKind
	userID   int64
	apiKeyID int64
	balance  float64
	amount   float64
}

// Eligibility 计费缓存服务
// 负责余额与 API Key 限速缓存管理，提供高性能的计费资格检查。
type Eligibility struct {
	background            func(string, func())
	cache                 BillingCache
	userRepo              BalanceReader
	apiKeyRateLimitLoader APIKeyRateLimitLoader
	options               func() EligibilityOptions
	observer              Observe
	circuitBreaker        *billingCircuitBreaker

	cacheWriteChan      chan cacheWriteTask
	cacheWriteStartOnce sync.Once
	cacheWriteWg        sync.WaitGroup
	cacheWriteStopOnce  sync.Once
	cacheWriteMu        sync.RWMutex
	stopped             atomic.Bool
	balanceLoadSF       singleflight.Group
	// 丢弃日志节流计数器（减少高负载下日志噪音）
	cacheWriteDropFullCount     uint64
	cacheWriteDropFullLastLog   int64
	cacheWriteDropClosedCount   uint64
	cacheWriteDropClosedLastLog int64
}

type billingCircuitBreakerState int

type billingCircuitBreaker struct {
	observer          Observe
	mu                sync.Mutex
	state             billingCircuitBreakerState
	failures          int
	openedAt          time.Time
	failureThreshold  int
	resetTimeout      time.Duration
	halfOpenRequests  int
	halfOpenRemaining int
}

func NewEligibility(cache BillingCache, users BalanceReader, keys APIKeyRateLimitLoader, options func() EligibilityOptions, observe Observe, background ...func(string, func())) *Eligibility {
	s := &Eligibility{cache: cache, userRepo: users, apiKeyRateLimitLoader: keys, options: options, observer: observe}
	if len(background) > 0 {
		s.background = background[0]
	}
	s.circuitBreaker = newBillingCircuitBreaker(options().Billing.CircuitBreaker)
	if s.circuitBreaker != nil {
		s.circuitBreaker.observer = observe
	}
	return s
}

// Start 在应用完成绑定后启动缓存写入 worker。
func (s *Eligibility) Start() {
	s.cacheWriteStartOnce.Do(func() {
		s.cacheWriteMu.Lock()
		defer s.cacheWriteMu.Unlock()
		if !s.stopped.Load() {
			s.startCacheWriteWorkers()
		}
	})
}

// Stop 关闭缓存写入工作池。
func (s *Eligibility) Stop() {
	s.cacheWriteStopOnce.Do(func() {
		s.stopped.Store(true)

		s.cacheWriteMu.Lock()
		ch := s.cacheWriteChan
		if ch != nil {
			close(ch)
		}
		s.cacheWriteMu.Unlock()

		if ch == nil {
			return
		}
		s.cacheWriteWg.Wait()

		s.cacheWriteMu.Lock()
		if s.cacheWriteChan == ch {
			s.cacheWriteChan = nil
		}
		s.cacheWriteMu.Unlock()
	})
}

func (s *Eligibility) startCacheWriteWorkers() {
	ch := make(chan cacheWriteTask, cacheWriteBufferSize)
	s.cacheWriteChan = ch
	for range cacheWriteWorkerCount {
		s.cacheWriteWg.Add(1)
		go s.cacheWriteWorker(ch)
	}
}

func (s *Eligibility) tryEnqueueCacheWrite(task cacheWriteTask) cacheWriteEnqueueResult {
	if s.stopped.Load() {
		return cacheWriteQueueClosed
	}

	s.cacheWriteMu.RLock()
	defer s.cacheWriteMu.RUnlock()

	if s.cacheWriteChan == nil {
		return cacheWriteQueueClosed
	}

	select {
	case s.cacheWriteChan <- task:
		return cacheWriteEnqueued
	default:
		// 队列满时不阻塞主流程，交由调用方决定是否同步回退。
		return cacheWriteQueueFull
	}
}

// enqueueCacheWrite 尝试将任务入队，队列满时返回 false（并记录实际丢弃告警）。
func (s *Eligibility) enqueueCacheWrite(task cacheWriteTask) (enqueued bool) {
	switch s.tryEnqueueCacheWrite(task) {
	case cacheWriteEnqueued:
		return true
	case cacheWriteQueueFull:
		s.logCacheWriteDrop(task, "full")
	case cacheWriteQueueClosed:
		s.logCacheWriteDrop(task, "closed")
	}
	return false
}

func (s *Eligibility) cacheWriteWorker(ch <-chan cacheWriteTask) {
	defer s.cacheWriteWg.Done()
	for task := range ch {
		ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
		switch task.kind {
		case cacheWriteSetBalance:
			s.setBalanceCache(ctx, task.userID, task.balance)
		case cacheWriteDeductBalance:
			if s.cache != nil {
				if err := s.cache.DeductUserBalance(ctx, task.userID, task.amount); err != nil {
					s.observer.Printf("service.billing_cache", "Warning: deduct balance cache failed for user %d: %v", task.userID, err)
				}
			}
		case cacheWriteUpdateRateLimitUsage:
			if s.cache != nil {
				if err := s.cache.UpdateAPIKeyRateLimitUsage(ctx, task.apiKeyID, task.amount); err != nil {
					s.observer.Printf("service.billing_cache", "Warning: update rate limit usage cache failed for api key %d: %v", task.apiKeyID, err)
				}
			}
		}
		cancel()
	}
}

// cacheWriteKindName 用于日志中的任务类型标识，便于排查丢弃原因。
func cacheWriteKindName(kind cacheWriteKind) string {
	switch kind {
	case cacheWriteSetBalance:
		return "set_balance"
	case cacheWriteDeductBalance:
		return "deduct_balance"
	case cacheWriteUpdateRateLimitUsage:
		return "update_rate_limit_usage"
	default:
		return "unknown"
	}
}

// logCacheWriteDrop 使用节流方式记录丢弃情况，并汇总丢弃数量。
func (s *Eligibility) logCacheWriteDrop(task cacheWriteTask, reason string) {
	var (
		countPtr *uint64
		lastPtr  *int64
	)
	switch reason {
	case "full":
		countPtr = &s.cacheWriteDropFullCount
		lastPtr = &s.cacheWriteDropFullLastLog
	case "closed":
		countPtr = &s.cacheWriteDropClosedCount
		lastPtr = &s.cacheWriteDropClosedLastLog
	default:
		return
	}

	atomic.AddUint64(countPtr, 1)
	now := s.dateRuntime().now().UnixNano()
	last := atomic.LoadInt64(lastPtr)
	if now-last < int64(cacheWriteDropLogInterval) {
		return
	}
	if !atomic.CompareAndSwapInt64(lastPtr, last, now) {
		return
	}
	dropped := atomic.SwapUint64(countPtr, 0)
	if dropped == 0 {
		return
	}
	s.observer.Printf("service.billing_cache", "Warning: cache write queue %s, dropped %d tasks in last %s (latest kind=%s user %d)",
		reason,
		dropped,
		cacheWriteDropLogInterval,
		cacheWriteKindName(task.kind),
		task.userID,
	)
}

// GetUserBalance 获取用户余额（优先从缓存读取）。
func (s *Eligibility) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	if s == nil || s.cache == nil {
		// Redis 不可用，直接查询数据库
		return s.getUserBalanceFromDB(ctx, userID)
	}

	// 尝试从缓存读取
	balance, err := s.cache.GetUserBalance(ctx, userID)
	if err == nil {
		return balance, nil
	}

	// 缓存未命中：singleflight 合并同一 userID 的并发回源请求。
	value, err, _ := s.balanceLoadSF.Do(strconv.FormatInt(userID, 10), func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.Background(), balanceLoadTimeout)
		defer cancel()

		balance, err := s.getUserBalanceFromDB(loadCtx, userID)
		if err != nil {
			return nil, err
		}

		// 异步建立缓存
		_ = s.enqueueCacheWrite(cacheWriteTask{
			kind:    cacheWriteSetBalance,
			userID:  userID,
			balance: balance,
		})
		return balance, nil
	})
	if err != nil {
		return 0, err
	}
	balance, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected balance type: %T", value)
	}
	return balance, nil
}

// getUserBalanceFromDB 从数据库获取用户余额。
func (s *Eligibility) getUserBalanceFromDB(ctx context.Context, userID int64) (float64, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("get user balance: %w", err)
	}
	return user.Balance, nil
}

// setBalanceCache 设置余额缓存。
func (s *Eligibility) setBalanceCache(ctx context.Context, userID int64, balance float64) {
	if s == nil || s.cache == nil {
		return
	}
	if err := s.cache.SetUserBalance(ctx, userID, balance); err != nil {
		s.observer.Printf("service.billing_cache", "Warning: set balance cache failed for user %d: %v", userID, err)
	}
}

// DeductBalanceCache 扣减余额缓存（同步调用）。
func (s *Eligibility) DeductBalanceCache(ctx context.Context, userID int64, amount float64) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.DeductUserBalance(ctx, userID, amount)
}

// QueueDeductBalance 异步扣减余额缓存。
func (s *Eligibility) QueueDeductBalance(userID int64, amount float64) {
	if s == nil || s.cache == nil {
		return
	}
	task := cacheWriteTask{
		kind:   cacheWriteDeductBalance,
		userID: userID,
		amount: amount,
	}
	switch s.tryEnqueueCacheWrite(task) {
	case cacheWriteEnqueued:
		return
	case cacheWriteQueueClosed:
		s.logCacheWriteDrop(task, "closed")
		return
	}
	// 队列满时同步回退，避免关键扣减被静默丢弃。
	ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
	defer cancel()
	if err := s.DeductBalanceCache(ctx, userID, amount); err != nil {
		s.observer.Printf("service.billing_cache", "Warning: deduct balance cache fallback failed for user %d: %v", userID, err)
	}
}

// InvalidateUserBalance 失效用户余额缓存。
func (s *Eligibility) InvalidateUserBalance(ctx context.Context, userID int64) error {
	if s == nil || s.cache == nil {
		return nil
	}
	if err := s.cache.InvalidateUserBalance(ctx, userID); err != nil {
		s.observer.Printf("service.billing_cache", "Warning: invalidate balance cache failed for user %d: %v", userID, err)
		return err
	}
	return nil
}

// InvalidateAPIKeyRateLimit invalidates the Redis rate-limit usage cache for an API key.
func (s *Eligibility) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	if s == nil || s.cache == nil {
		return nil
	}
	if err := s.cache.InvalidateAPIKeyRateLimit(ctx, keyID); err != nil {
		s.observer.Printf("service.billing_cache", "Warning: invalidate api key rate limit cache failed for key %d: %v", keyID, err)
		return err
	}
	return nil
}

// checkAPIKeyRateLimits checks rate limit windows for an API key.
// It loads usage from Redis cache (falling back to DB on cache miss),
// resets expired windows in-memory and triggers async DB reset,
// and returns an error if any window limit is exceeded.
func (s *Eligibility) checkAPIKeyRateLimits(ctx context.Context, apiKey *KeySnapshot) error {
	if s == nil || s.cache == nil {
		// No cache: fall back to reading from DB directly
		if s.apiKeyRateLimitLoader == nil {
			return nil
		}
		data, err := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, apiKey.ID)
		if err != nil {
			return nil // Don't block requests on DB errors
		}
		return s.evaluateRateLimits(ctx, apiKey, data.Usage5h, data.Usage1d, data.Usage7d,
			data.Window5hStart, data.Window1dStart, data.Window7dStart)
	}

	cacheData, err := s.cache.GetAPIKeyRateLimit(ctx, apiKey.ID)
	if err != nil {
		// Cache miss: load from DB and populate cache
		if s.apiKeyRateLimitLoader == nil {
			return nil
		}
		dbData, dbErr := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, apiKey.ID)
		if dbErr != nil {
			return nil // Don't block requests on DB errors
		}
		// Build cache entry from DB data
		cacheEntry := &APIKeyRateLimitCacheData{
			Usage5h: dbData.Usage5h,
			Usage1d: dbData.Usage1d,
			Usage7d: dbData.Usage7d,
		}
		if dbData.Window5hStart != nil {
			cacheEntry.Window5h = dbData.Window5hStart.Unix()
		}
		if dbData.Window1dStart != nil {
			cacheEntry.Window1d = dbData.Window1dStart.Unix()
		}
		if dbData.Window7dStart != nil {
			cacheEntry.Window7d = dbData.Window7dStart.Unix()
		}
		_ = s.cache.SetAPIKeyRateLimit(ctx, apiKey.ID, cacheEntry)
		cacheData = cacheEntry
	}

	var w5h, w1d, w7d *time.Time
	if cacheData.Window5h > 0 {
		t := time.Unix(cacheData.Window5h, 0)
		w5h = &t
	}
	if cacheData.Window1d > 0 {
		t := time.Unix(cacheData.Window1d, 0)
		w1d = &t
	}
	if cacheData.Window7d > 0 {
		t := time.Unix(cacheData.Window7d, 0)
		w7d = &t
	}
	return s.evaluateRateLimits(ctx, apiKey, cacheData.Usage5h, cacheData.Usage1d, cacheData.Usage7d, w5h, w1d, w7d)
}

// evaluateRateLimits checks usage against limits, triggering async resets for expired windows.
func (s *Eligibility) evaluateRateLimits(ctx context.Context, apiKey *KeySnapshot, usage5h, usage1d, usage7d float64, w5h, w1d, w7d *time.Time) error {
	needsReset := false

	// Reset expired windows in-memory for check purposes
	if IsWindowExpired(w5h, RateLimitWindow5h) {
		usage5h = 0
		needsReset = true
	}
	if IsWindowExpired(w1d, RateLimitWindow1d) {
		usage1d = 0
		needsReset = true
	}
	if IsWindowExpired(w7d, RateLimitWindow7d) {
		usage7d = 0
		needsReset = true
	}

	// Trigger async DB reset if any window expired
	if needsReset {
		keyID := apiKey.ID
		s.runBackground("service/billing_cache_service.go:evaluateRateLimits", func() {
			resetCtx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
			defer cancel()
			if s.apiKeyRateLimitLoader != nil {
				// Use the repo directly - reset then reload cache
				if loader, ok := s.apiKeyRateLimitLoader.(interface {
					ResetRateLimitWindows(ctx context.Context, id int64) error
				}); ok {
					if err := loader.ResetRateLimitWindows(resetCtx, keyID); err != nil {
						s.observer.Printf("service.billing_cache", "Warning: reset rate limit windows failed for api key %d: %v", keyID, err)
					}
				}
			}
			// Invalidate cache so next request loads fresh data
			if s.cache != nil {
				if err := s.cache.InvalidateAPIKeyRateLimit(resetCtx, keyID); err != nil {
					s.observer.Printf("service.billing_cache", "Warning: invalidate rate limit cache failed for api key %d: %v", keyID, err)
				}
			}
		})
	}

	// Check limits
	if apiKey.RateLimit5h > 0 && usage5h >= apiKey.RateLimit5h {
		return ErrAPIKeyRateLimit5hExceeded
	}
	if apiKey.RateLimit1d > 0 && usage1d >= apiKey.RateLimit1d {
		return ErrAPIKeyRateLimit1dExceeded
	}
	if apiKey.RateLimit7d > 0 && usage7d >= apiKey.RateLimit7d {
		return ErrAPIKeyRateLimit7dExceeded
	}
	return nil
}

// QueueUpdateAPIKeyRateLimitUsage asynchronously updates rate limit usage in the cache.
func (s *Eligibility) QueueUpdateAPIKeyRateLimitUsage(apiKeyID int64, cost float64) {
	if s == nil || s.cache == nil {
		return
	}
	task := cacheWriteTask{
		kind:     cacheWriteUpdateRateLimitUsage,
		apiKeyID: apiKeyID,
		amount:   cost,
	}
	switch s.tryEnqueueCacheWrite(task) {
	case cacheWriteEnqueued:
		return
	case cacheWriteQueueClosed:
		s.logCacheWriteDrop(task, "closed")
		return
	}
	// 队列满时同步回退，避免限速使用量统计被静默丢弃。
	ctx, cancel := context.WithTimeout(context.Background(), cacheWriteTimeout)
	defer cancel()
	if err := s.cache.UpdateAPIKeyRateLimitUsage(ctx, apiKeyID, cost); err != nil {
		s.observer.Printf("service.billing_cache", "Warning: update rate limit usage cache fallback failed for api key %d: %v", apiKeyID, err)
	}
}

// CheckBillingEligibility 检查用户是否有资格发起请求。
// auto 模式保留订阅额度耗尽后回退余额的历史行为；指定订阅和仅余额模式严格遵循 Key 配置。
func (s *Eligibility) CheckBillingEligibility(ctx context.Context, user *UserSummary, apiKey *KeySnapshot, group *GroupSnapshot, subscription *UserSubscription, platform string) error {
	if s.circuitBreaker != nil && !s.circuitBreaker.Allow() {
		return ErrBillingServiceUnavailable
	}

	billingMode := effectiveKeyBillingMode(apiKey)
	if billingMode == APIKeyBillingModeSubscription {
		// 请求期二次校验防止分组回退、套餐变更或异步路径绕过指定套餐的范围。
		if subscription == nil {
			return ErrPreferredSubscriptionInvalid
		}
		if group != nil && !SubscriptionAllowsGroup(subscription, group.ID) {
			return ErrPreferredSubscriptionGroup
		}
		if err := CheckEffectiveSubscriptionEligibility(subscription, s.dateRuntime().calendar()); err != nil {
			if IsSubscriptionQuotaExceeded(err) {
				return ErrPreferredSubscriptionInsufficient
			}
			return ErrPreferredSubscriptionInvalid
		}
	}
	if billingMode == APIKeyBillingModeBalance {
		// 即使上游误传了订阅快照，余额模式也不能因此使用套餐额度。
		subscription = nil
	}

	if subscription != nil {
		if err := CheckEffectiveSubscriptionEligibility(subscription, s.dateRuntime().calendar()); err != nil {
			if !IsSubscriptionQuotaExceeded(err) {
				return err
			}
			if err := s.checkBalanceEligibility(ctx, user.ID); err != nil {
				return err
			}
		}
	} else {
		if err := s.checkBalanceEligibility(ctx, user.ID); err != nil {
			return err
		}
	}

	// Check API Key rate limits (applies to both billing modes)
	if apiKey != nil && apiKey.HasRateLimits() {
		if err := s.checkAPIKeyRateLimits(ctx, apiKey); err != nil {
			return err
		}
	}

	return nil
}

func CheckEffectiveSubscriptionEligibility(subscription *UserSubscription, calendar timezone.Calendar) error {
	if subscription == nil {
		return ErrSubscriptionInvalid
	}
	switch subscription.EffectiveStatus(time.Now()) {
	case SubscriptionStatusExpired:
		return ErrSubscriptionInvalid
	case SubscriptionStatusSuspended:
		return ErrSubscriptionInvalid
	case SubscriptionStatusPending:
		return ErrSubscriptionInvalid
	case SubscriptionStatusRevoked:
		return ErrSubscriptionInvalid
	}

	effective := *subscription
	if effective.NeedsDailyReset(calendar) {
		effective.DailyUsageUSD = 0
	}
	if effective.NeedsWeeklyReset() {
		effective.WeeklyUsageUSD = 0
	}
	if effective.NeedsMonthlyReset() {
		effective.MonthlyUsageUSD = 0
	}

	return CheckSubscriptionUsageLimits(&effective, 0)
}

func IsSubscriptionQuotaExceeded(err error) bool {
	return errors.Is(err, ErrDailyLimitExceeded) ||
		errors.Is(err, ErrWeeklyLimitExceeded) ||
		errors.Is(err, ErrMonthlyLimitExceeded)
}

func (s *Eligibility) MinimumBalanceReserve() float64 {
	if s == nil || s.options == nil || s.options().Billing.MinimumBalanceReserve <= 0 {
		return 0
	}
	return s.options().Billing.MinimumBalanceReserve
}

func (s *Eligibility) BalanceBelowEligibilityThreshold(balance float64) bool {
	if balance <= 0 {
		return true
	}
	minimumReserve := s.MinimumBalanceReserve()
	return minimumReserve > 0 && balance < minimumReserve
}

// checkBalanceEligibility 检查余额模式资格。
func (s *Eligibility) checkBalanceEligibility(ctx context.Context, userID int64) error {
	balance, err := s.GetUserBalance(ctx, userID)
	if err != nil {
		if s.circuitBreaker != nil {
			s.circuitBreaker.OnFailure(err)
		}
		s.observer.Printf("service.billing_cache", "ALERT: billing balance check failed for user %d: %v", userID, err)
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if s.circuitBreaker != nil {
		s.circuitBreaker.OnSuccess()
	}

	if s.BalanceBelowEligibilityThreshold(balance) {
		return ErrInsufficientBalance
	}

	return nil
}

func newBillingCircuitBreaker(cfg CircuitBreakerOptions) *billingCircuitBreaker {
	if !cfg.Enabled {
		return nil
	}
	resetTimeout := time.Duration(cfg.ResetTimeoutSeconds) * time.Second
	if resetTimeout <= 0 {
		resetTimeout = 30 * time.Second
	}
	halfOpen := cfg.HalfOpenRequests
	if halfOpen <= 0 {
		halfOpen = 1
	}
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 5
	}
	return &billingCircuitBreaker{
		state:            billingCircuitClosed,
		failureThreshold: threshold,
		resetTimeout:     resetTimeout,
		halfOpenRequests: halfOpen,
	}
}

func (b *billingCircuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case billingCircuitClosed:
		return true
	case billingCircuitOpen:
		if time.Since(b.openedAt) < b.resetTimeout {
			return false
		}
		b.state = billingCircuitHalfOpen
		b.halfOpenRemaining = b.halfOpenRequests
		b.observer.Printf("service.billing_cache", "ALERT: billing circuit breaker entering half-open state")
		fallthrough
	case billingCircuitHalfOpen:
		if b.halfOpenRemaining <= 0 {
			return false
		}
		b.halfOpenRemaining--
		return true
	default:
		return false
	}
}

func (b *billingCircuitBreaker) OnFailure(err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case billingCircuitOpen:
		return
	case billingCircuitHalfOpen:
		b.state = billingCircuitOpen
		b.openedAt = time.Now()
		b.halfOpenRemaining = 0
		b.observer.Printf("service.billing_cache", "ALERT: billing circuit breaker opened after half-open failure: %v", err)
		return
	default:
		b.failures++
		if b.failures >= b.failureThreshold {
			b.state = billingCircuitOpen
			b.openedAt = time.Now()
			b.halfOpenRemaining = 0
			b.observer.Printf("service.billing_cache", "ALERT: billing circuit breaker opened after %d failures: %v", b.failures, err)
		}
	}
}

func (b *billingCircuitBreaker) OnSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	previousState := b.state
	previousFailures := b.failures

	b.state = billingCircuitClosed
	b.failures = 0
	b.halfOpenRemaining = 0

	// 只有状态真正发生变化时才记录日志
	if previousState != billingCircuitClosed {
		b.observer.Printf("service.billing_cache", "ALERT: billing circuit breaker closed (was %s)", circuitStateString(previousState))
	} else if previousFailures > 0 {
		b.observer.Printf("service.billing_cache", "INFO: billing circuit breaker failures reset from %d", previousFailures)
	}
}

func circuitStateString(state billingCircuitBreakerState) string {
	switch state {
	case billingCircuitClosed:
		return "closed"
	case billingCircuitOpen:
		return "open"
	case billingCircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Check 只执行资金准入；RPM 仍由旧请求编排在资金检查后决定。
func (s *Eligibility) Check(ctx context.Context, input CheckInput) error {
	return s.CheckBillingEligibility(ctx, input.Payer, input.Key, input.Group, input.Subscription, input.Platform)
}

// runBackground 通过装配注入的任务拥有者执行；独立测试构造时同步完成。
func (s *Eligibility) runBackground(name string, fn func()) {
	if s.background == nil {
		fn()
		return
	}
	s.background(name, fn)
}

func (s *Eligibility) dateRuntime() DateRuntime {
	if s != nil && s.options != nil {
		return s.options().Dates
	}
	return DateRuntime{}
}
