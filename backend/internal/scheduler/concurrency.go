package scheduler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	openAIWSIngressLeaseTTL             = 60 * time.Second
	openAIWSIngressLeaseRefreshInterval = 20 * time.Second
	openAIWSIngressLeaseOperationTO     = 2 * time.Second

	// 默认等待队列额外槽位。
	defaultExtraWaitSlots = 20

	defaultProviderLoadBatchCacheTTL = 200 * time.Millisecond
	providerLoadBatchFetchTimeout    = 3 * time.Second
	maxProviderLoadBatchCacheEntries = 256
	apiKeyConcurrencyFetchTimeout    = 3 * time.Second
	apiKeySlotTrackTimeout           = 2 * time.Second
)

var (
	ErrOpenAIWSIngressLeaseLost = errors.New("openai websocket ingress lease lost")

	requestIDPrefix  = initRequestIDPrefix()
	requestIDCounter atomic.Uint64
)

// ConcurrencyCache 定义并发控制的缓存接口
// 使用有序集合存储槽位，按时间戳清理过期条目。
type ConcurrencyCache interface {
	// 提供商槽位管理
	// 键格式: concurrency:provider:{providerID}（有序集合，成员为 requestID）
	AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error)
	ReleaseProviderSlot(ctx context.Context, providerID int64, requestID string) error
	GetProviderConcurrency(ctx context.Context, providerID int64) (int, error)
	GetProviderConcurrencyBatch(ctx context.Context, providerIDs []int64) (map[int64]int, error)

	// 提供商等待队列（提供商级）
	IncrementProviderWaitCount(ctx context.Context, providerID int64, maxWait int) (bool, error)
	DecrementProviderWaitCount(ctx context.Context, providerID int64) error
	GetProviderWaitingCount(ctx context.Context, providerID int64) (int, error)

	// 用户槽位管理
	// 键格式: concurrency:user:{userID}（有序集合，成员为 requestID）
	AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error)
	ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error
	GetUserConcurrency(ctx context.Context, userID int64) (int, error)

	// 等待队列计数，每次入队刷新 TTL，长时间排队时持续续期。
	IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error)
	DecrementWaitCount(ctx context.Context, userID int64) error

	// 批量负载查询（只读）
	GetProvidersLoadBatch(ctx context.Context, providers []ProviderWithConcurrency) (map[int64]*ProviderLoadInfo, error)
	GetUsersLoadBatch(ctx context.Context, users []UserWithConcurrency) (map[int64]*UserLoadInfo, error)

	// 清理过期槽位（后台任务）
	CleanupExpiredProviderSlots(ctx context.Context, providerID int64) error
	CleanupExpiredProviderSlotKeys(ctx context.Context) error

	// 启动时清理旧进程遗留槽位与等待计数
	CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error
}

// APIKeyConcurrencyCache 定义 API Key 实时并发统计所需的缓存能力。
type APIKeyConcurrencyCache interface {
	TrackAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error
	ReleaseAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error
	GetAPIKeyConcurrencyBatch(ctx context.Context, apiKeyIDs []int64) (map[int64]int, error)
}

// OpenAIWSIngressLeaseCache 管理限制客户端 WebSocket 活跃会话数的短期分布式租约。
// 它与请求槽位命名空间相互独立，空闲入站连接不会占用轮次槽位。
type OpenAIWSIngressLeaseCache interface {
	AcquireOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, maxConnections int, leaseID string) (bool, error)
	RefreshOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, leaseID string) (bool, error)
	ReleaseOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, leaseID string) error
}

// OpenAIWSIngressLease 维持 Redis 入站租约；若整个租约周期都无法确认所有权，则取消上下文。
// handler 的所有退出路径都必须调用 Release，立即归还容量。
type OpenAIWSIngressLease struct {
	diagnostics Diagnostics
	ctx         context.Context
	cancel      context.CancelCauseFunc
	cache       OpenAIWSIngressLeaseCache
	apiKeyID    int64
	leaseID     string

	stopOnce      sync.Once
	stopCh        chan struct{}
	refreshDone   chan struct{}
	finishRuntime func()
}

// ConcurrencyService 管理提供商和用户的并发限制。
type ConcurrencyService struct {
	diagnostics Diagnostics
	runtime     WorkerRuntime

	cache ConcurrencyCache

	providerLoadCacheTTL atomic.Int64
	providerLoadCacheMu  sync.RWMutex
	providerLoadCache    map[string]cachedProviderLoadBatch
	providerLoadGroup    singleflight.Group
}

type cachedProviderLoadBatch struct {
	loadMap   map[int64]*ProviderLoadInfo
	expiresAt time.Time
}

// AcquireResult 记录是否取得并发槽位及其释放函数。
type AcquireResult struct {
	Acquired    bool
	ReleaseFunc func() // 调用方在请求结束时调用，可使用 defer。
}

type ProviderWithConcurrency struct {
	ID             int64
	MaxConcurrency int
}

type UserWithConcurrency struct {
	ID             int64
	MaxConcurrency int
}

type ProviderLoadInfo struct {
	ProviderID         int64
	CurrentConcurrency int
	WaitingCount       int
	LoadRate           int // 0-100+ (percent)
}

type UserLoadInfo struct {
	UserID             int64
	CurrentConcurrency int
	WaitingCount       int
	LoadRate           int // 0-100+ (percent)
}

func (l *OpenAIWSIngressLease) Context() context.Context {
	if l == nil || l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}

func (l *OpenAIWSIngressLease) Release() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() {
		if l.finishRuntime != nil {
			defer l.finishRuntime()
		}
		if l.stopCh != nil {
			close(l.stopCh)
		}
		if l.cancel != nil {
			l.cancel(nil)
		}
		if l.refreshDone != nil {
			<-l.refreshDone
		}
		if l.cache == nil || l.apiKeyID <= 0 || l.leaseID == "" {
			return
		}
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), openAIWSIngressLeaseOperationTO)
		defer releaseCancel()
		if err := l.cache.ReleaseOpenAIWSIngressLease(releaseCtx, l.apiKeyID, l.leaseID); err != nil {
			l.diagnostics.event("warn", "openai_ws_ingress_lease_release_failed",
				"api_key_id", l.apiKeyID,
				"error", err,
			)
		}
	})
}

func (l *OpenAIWSIngressLease) refreshLoop() {
	defer func() {
		if l != nil && l.refreshDone != nil {
			close(l.refreshDone)
		}
	}()
	if l == nil || l.cache == nil {
		return
	}
	ticker := time.NewTicker(openAIWSIngressLeaseRefreshInterval)
	defer ticker.Stop()
	lastConfirmedAt := time.Now()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.stopCh:
			return
		case <-ticker.C:
			var lost bool
			lastConfirmedAt, lost = l.refresh(lastConfirmedAt)
			if lost {
				l.cancel(ErrOpenAIWSIngressLeaseLost)
				return
			}
		}
	}
}

// refresh 确认租约仍归当前进程所有。成员缺失会立即判定丢失，临时 Redis 错误最多容忍一个 TTL。
func (l *OpenAIWSIngressLease) refresh(lastConfirmedAt time.Time) (time.Time, bool) {
	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), openAIWSIngressLeaseOperationTO)
	owned, err := l.cache.RefreshOpenAIWSIngressLease(refreshCtx, l.apiKeyID, l.leaseID)
	refreshCancel()
	if err == nil && owned {
		return time.Now(), false
	}
	if err == nil {
		err = ErrOpenAIWSIngressLeaseLost
	}
	elapsed := time.Since(lastConfirmedAt)
	l.diagnostics.event("warn", "openai_ws_ingress_lease_refresh_failed",
		"api_key_id", l.apiKeyID,
		"unconfirmed_for", elapsed,
		"error", err,
	)
	if errors.Is(err, ErrOpenAIWSIngressLeaseLost) || elapsed >= openAIWSIngressLeaseTTL {
		l.diagnostics.event("error", "openai_ws_ingress_lease_lost",
			"api_key_id", l.apiKeyID,
			"unconfirmed_for", elapsed,
			"error", err,
		)
		return lastConfirmedAt, true
	}
	return lastConfirmedAt, false
}

func initRequestIDPrefix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		return "r" + strconv.FormatUint(binary.BigEndian.Uint64(b), 36)
	}
	fallback := uint64(time.Now().UnixNano()) ^ (uint64(os.Getpid()) << 16)
	return "r" + strconv.FormatUint(fallback, 36)
}

func RequestIDPrefix() string {
	return requestIDPrefix
}

func GenerateRequestID() string {
	seq := requestIDCounter.Add(1)
	return requestIDPrefix + "-" + strconv.FormatUint(seq, 36)
}

// NewConcurrencyService 创建并发控制服务。
func NewConcurrencyService(cache ConcurrencyCache, options ...Diagnostics) *ConcurrencyService {
	var diagnostics Diagnostics
	if len(options) > 0 {
		diagnostics = options[0]
	}

	svc := &ConcurrencyService{
		cache:             cache,
		diagnostics:       diagnostics,
		providerLoadCache: make(map[string]cachedProviderLoadBatch),
	}
	svc.SetProviderLoadBatchCacheTTL(defaultProviderLoadBatchCacheTTL)
	return svc
}

func (s *ConcurrencyService) CleanupStaleProcessSlots(ctx context.Context) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.CleanupStaleProcessSlots(ctx, RequestIDPrefix())
}

// AcquireOpenAIWSIngressLease 为 API Key 原子预留一个活跃入站连接；非正数上限表示关闭保护。
func (s *ConcurrencyService) AcquireOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, maxConnections int) (*OpenAIWSIngressLease, bool, error) {
	if maxConnections <= 0 {
		return nil, true, nil
	}
	if s == nil || s.cache == nil || apiKeyID <= 0 {
		return nil, false, errors.New("openai websocket ingress lease cache is unavailable")
	}
	cache, ok := s.cache.(OpenAIWSIngressLeaseCache)
	if !ok {
		return nil, false, errors.New("openai websocket ingress lease cache is unsupported")
	}
	leaseID := GenerateRequestID()
	baseCtx, done, enterErr := s.runtime.Enter(context.WithoutCancel(nonNilContext(ctx)), fmt.Sprintf("ws-ingress:%d", apiKeyID))
	if enterErr != nil {
		return nil, false, enterErr
	}
	acquireCtx, acquireCancel := context.WithTimeout(baseCtx, openAIWSIngressLeaseOperationTO)
	acquired, err := cache.AcquireOpenAIWSIngressLease(acquireCtx, apiKeyID, maxConnections, leaseID)
	acquireCancel()
	if err != nil || !acquired {
		done()
		return nil, acquired, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	leaseCtx, leaseCancel := context.WithCancelCause(ctx)
	lease := &OpenAIWSIngressLease{
		diagnostics: s.diagnostics,
		ctx:         leaseCtx,
		cancel:      leaseCancel,
		cache:       cache,
		apiKeyID:    apiKeyID,
		leaseID:     leaseID,
		stopCh:      make(chan struct{}),
		refreshDone: make(chan struct{}),
	}
	stop := context.AfterFunc(baseCtx, func() { leaseCancel(ErrRuntimeStopped) })
	lease.finishRuntime = func() { stop(); done() }
	go lease.refreshLoop()
	return lease, true, nil
}

// SetProviderLoadBatchCacheTTL 设置提供商负载批量读取的极短 TTL 缓存；非正数表示禁用缓存。
func (s *ConcurrencyService) SetProviderLoadBatchCacheTTL(ttl time.Duration) {
	if s == nil {
		return
	}
	s.providerLoadCacheTTL.Store(int64(ttl))
	if ttl <= 0 {
		s.providerLoadCacheMu.Lock()
		s.providerLoadCache = make(map[string]cachedProviderLoadBatch)
		s.providerLoadCacheMu.Unlock()
	}
}

// acquireProviderSlot 尝试获取提供商并发槽位。
// 达到上限时返回 Acquired=false，取得槽位后调用方需要在请求结束时调用释放函数。
func (s *ConcurrencyService) acquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int) (*AcquireResult, error) {
	// maxConcurrency 非正时直接放行。
	if maxConcurrency <= 0 {
		return &AcquireResult{
			Acquired:    true,
			ReleaseFunc: func() {}, // no-op
		}, nil
	}

	// 为本次槽位申请生成请求 ID。
	requestID := GenerateRequestID()

	acquired, err := s.cache.AcquireProviderSlot(ctx, providerID, maxConcurrency, requestID)
	if err != nil {
		return nil, err
	}

	if acquired {
		return &AcquireResult{
			Acquired: true,
			ReleaseFunc: func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.cache.ReleaseProviderSlot(bgCtx, providerID, requestID); err != nil {
					s.diagnostics.printf("service.concurrency", "Warning: failed to release provider slot for %d (req=%s): %v", providerID, requestID, err)
				}
			},
		}, nil
	}

	return &AcquireResult{
		Acquired:    false,
		ReleaseFunc: nil,
	}, nil
}

// acquireUserSlot 尝试取得用户并发槽位，达到上限时返回 Acquired=false。
// 请求结束后必须调用返回的释放函数。
func (s *ConcurrencyService) acquireUserSlot(ctx context.Context, userID int64, maxConcurrency int) (*AcquireResult, error) {
	// maxConcurrency 非正时直接放行。
	if maxConcurrency <= 0 {
		return &AcquireResult{
			Acquired:    true,
			ReleaseFunc: func() {}, // no-op
		}, nil
	}

	// 为本次槽位申请生成请求 ID。
	requestID := GenerateRequestID()

	acquired, err := s.cache.AcquireUserSlot(ctx, userID, maxConcurrency, requestID)
	if err != nil {
		return nil, err
	}

	if acquired {
		return &AcquireResult{
			Acquired: true,
			ReleaseFunc: func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.cache.ReleaseUserSlot(bgCtx, userID, requestID); err != nil {
					s.diagnostics.printf("service.concurrency", "Warning: failed to release user slot for %d (req=%s): %v", userID, requestID, err)
				}
			},
		}, nil
	}

	return &AcquireResult{
		Acquired:    false,
		ReleaseFunc: nil,
	}, nil
}

// trackAPIKeySlot 登记 API Key 活跃请求槽位，用于统计。
// 统计采用故障放行：Redis 出错时记录日志并返回空操作释放函数。
func (s *ConcurrencyService) trackAPIKeySlot(ctx context.Context, apiKeyID int64) func() {
	if s == nil || s.cache == nil || apiKeyID <= 0 {
		return func() {}
	}
	cache, ok := s.cache.(APIKeyConcurrencyCache)
	if !ok {
		return func() {}
	}

	requestID := GenerateRequestID()
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = ctx
	}
	trackCtx, cancel := context.WithTimeout(baseCtx, apiKeySlotTrackTimeout)
	err := cache.TrackAPIKeySlot(trackCtx, apiKeyID, requestID)
	cancel()
	if err != nil {
		s.diagnostics.printf("service.concurrency", "Warning: failed to track api key slot for %d (req=%s): %v", apiKeyID, requestID, err)
		return func() {}
	}

	return func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.ReleaseAPIKeySlot(bgCtx, apiKeyID, requestID); err != nil {
			s.diagnostics.printf("service.concurrency", "Warning: failed to release api key slot for %d (req=%s): %v", apiKeyID, requestID, err)
		}
	}
}

// GetAPIKeyConcurrencyBatch 批量获取 API Key 的实时活跃请求数。
// 缓存不支持统计或 Redis 出错时返回零值。
func (s *ConcurrencyService) GetAPIKeyConcurrencyBatch(ctx context.Context, apiKeyIDs []int64) (map[int64]int, error) {
	result := zeroAPIKeyConcurrencyMap(apiKeyIDs)
	if len(apiKeyIDs) == 0 {
		return result, nil
	}
	if s == nil || s.cache == nil {
		return result, nil
	}
	cache, ok := s.cache.(APIKeyConcurrencyCache)
	if !ok {
		return result, nil
	}

	operation, done, err := s.runtime.Enter(context.Background(), "GetAPIKeyConcurrencyBatch")
	if err != nil {
		return result, err
	}
	defer done()
	redisCtx, cancel := context.WithTimeout(operation, apiKeyConcurrencyFetchTimeout)
	defer cancel()

	counts, err := cache.GetAPIKeyConcurrencyBatch(redisCtx, apiKeyIDs)
	if err != nil {
		s.diagnostics.printf("service.concurrency", "Warning: get api key concurrency batch failed: %v", err)
		return result, nil
	}
	for _, apiKeyID := range apiKeyIDs {
		result[apiKeyID] = counts[apiKeyID]
	}
	return result, nil
}

func zeroAPIKeyConcurrencyMap(apiKeyIDs []int64) map[int64]int {
	result := make(map[int64]int, len(apiKeyIDs))
	for _, apiKeyID := range apiKeyIDs {
		result[apiKeyID] = 0
	}
	return result
}

// GetProviderWaitingCount 获取提供商当前的等待队列长度。
func (s *ConcurrencyService) GetProviderWaitingCount(ctx context.Context, providerID int64) (int, error) {
	if s.cache == nil {
		return 0, nil
	}
	return s.cache.GetProviderWaitingCount(ctx, providerID)
}

// CalculateMaxWait 在用户并发上限上增加 defaultExtraWaitSlots 个等待名额，非正并发上限按 1 计算。
func CalculateMaxWait(userConcurrency int) int {
	if userConcurrency <= 0 {
		userConcurrency = 1
	}
	return userConcurrency + defaultExtraWaitSlots
}

// GetProvidersLoadBatch 批量获取提供商负载信息。
func (s *ConcurrencyService) GetProvidersLoadBatch(ctx context.Context, providers []ProviderWithConcurrency) (map[int64]*ProviderLoadInfo, error) {
	return s.getProvidersLoadBatch(ctx, providers, true)
}

// GetProvidersLoadBatchFresh 直接读取提供商负载，供抢槽失败后刷新候选使用。
func (s *ConcurrencyService) GetProvidersLoadBatchFresh(ctx context.Context, providers []ProviderWithConcurrency) (map[int64]*ProviderLoadInfo, error) {
	return s.getProvidersLoadBatch(ctx, providers, false)
}

func (s *ConcurrencyService) getProvidersLoadBatch(ctx context.Context, providers []ProviderWithConcurrency, allowCache bool) (map[int64]*ProviderLoadInfo, error) {
	if len(providers) == 0 {
		return map[int64]*ProviderLoadInfo{}, nil
	}
	if s.cache == nil {
		return map[int64]*ProviderLoadInfo{}, nil
	}

	ttl := time.Duration(s.providerLoadCacheTTL.Load())
	if !allowCache || ttl <= 0 {
		return s.fetchProvidersLoadBatch(ctx, providers)
	}

	key := providerLoadBatchCacheKey(providers)
	if cached, ok := s.getCachedProviderLoadBatch(key, time.Now()); ok {
		return cached, nil
	}

	value, err, _ := s.providerLoadGroup.Do(key, func() (any, error) {
		now := time.Now()
		if cached, ok := s.getCachedProviderLoadBatch(key, now); ok {
			return cached, nil
		}
		loadMap, fetchErr := s.fetchProvidersLoadBatch(ctx, providers)
		if fetchErr != nil {
			return nil, fetchErr
		}
		cached := cloneProviderLoadMap(loadMap)
		s.storeCachedProviderLoadBatch(key, cached, now.Add(ttl))
		return cached, nil
	})
	if err != nil {
		return nil, err
	}
	loadMap, _ := value.(map[int64]*ProviderLoadInfo)
	if loadMap == nil {
		return map[int64]*ProviderLoadInfo{}, nil
	}
	return loadMap, nil
}

func (s *ConcurrencyService) fetchProvidersLoadBatch(ctx context.Context, providers []ProviderWithConcurrency) (map[int64]*ProviderLoadInfo, error) {
	if s.cache == nil {
		return map[int64]*ProviderLoadInfo{}, nil
	}
	operation, done, err := s.runtime.Enter(context.WithoutCancel(nonNilContext(ctx)), "fetchProvidersLoadBatch")
	if err != nil {
		return nil, err
	}
	defer done()
	redisCtx, cancel := context.WithTimeout(operation, providerLoadBatchFetchTimeout)
	defer cancel()
	return s.cache.GetProvidersLoadBatch(redisCtx, providers)
}

func (s *ConcurrencyService) getCachedProviderLoadBatch(key string, now time.Time) (map[int64]*ProviderLoadInfo, bool) {
	s.providerLoadCacheMu.RLock()
	cached, ok := s.providerLoadCache[key]
	s.providerLoadCacheMu.RUnlock()
	if !ok {
		return nil, false
	}
	if !now.Before(cached.expiresAt) {
		s.providerLoadCacheMu.Lock()
		if current, exists := s.providerLoadCache[key]; exists && !now.Before(current.expiresAt) {
			delete(s.providerLoadCache, key)
		}
		s.providerLoadCacheMu.Unlock()
		return nil, false
	}
	return cached.loadMap, true
}

func (s *ConcurrencyService) storeCachedProviderLoadBatch(key string, loadMap map[int64]*ProviderLoadInfo, expiresAt time.Time) {
	s.providerLoadCacheMu.Lock()
	if s.providerLoadCache == nil {
		s.providerLoadCache = make(map[string]cachedProviderLoadBatch)
	}
	if len(s.providerLoadCache) >= maxProviderLoadBatchCacheEntries {
		now := time.Now()
		for cacheKey, cached := range s.providerLoadCache {
			if !now.Before(cached.expiresAt) {
				delete(s.providerLoadCache, cacheKey)
			}
		}
		for len(s.providerLoadCache) >= maxProviderLoadBatchCacheEntries {
			for cacheKey := range s.providerLoadCache {
				delete(s.providerLoadCache, cacheKey)
				break
			}
		}
	}
	s.providerLoadCache[key] = cachedProviderLoadBatch{
		loadMap:   loadMap,
		expiresAt: expiresAt,
	}
	s.providerLoadCacheMu.Unlock()
}

func providerLoadBatchCacheKey(providers []ProviderWithConcurrency) string {
	hash := sha256.New()
	var buf [16]byte
	for _, provider := range providers {
		binary.LittleEndian.PutUint64(buf[:8], uint64(provider.ID))
		binary.LittleEndian.PutUint64(buf[8:], uint64(int64(provider.MaxConcurrency)))
		_, _ = hash.Write(buf[:])
	}
	sum := hash.Sum(nil)
	return strconv.Itoa(len(providers)) + ":" + hex.EncodeToString(sum)
}

func cloneProviderLoadMap(loadMap map[int64]*ProviderLoadInfo) map[int64]*ProviderLoadInfo {
	if len(loadMap) == 0 {
		return map[int64]*ProviderLoadInfo{}
	}
	clone := make(map[int64]*ProviderLoadInfo, len(loadMap))
	for providerID, loadInfo := range loadMap {
		if loadInfo == nil {
			clone[providerID] = nil
			continue
		}
		copied := *loadInfo
		clone[providerID] = &copied
	}
	return clone
}

// GetUsersLoadBatch 批量读取用户负载。
func (s *ConcurrencyService) GetUsersLoadBatch(ctx context.Context, users []UserWithConcurrency) (map[int64]*UserLoadInfo, error) {
	if s.cache == nil {
		return map[int64]*UserLoadInfo{}, nil
	}
	return s.cache.GetUsersLoadBatch(ctx, users)
}

// CleanupExpiredProviderSlots 清理指定提供商的过期槽位，供后台任务调用。
func (s *ConcurrencyService) CleanupExpiredProviderSlots(ctx context.Context, providerID int64) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.CleanupExpiredProviderSlots(ctx, providerID)
}

// StartSlotCleanupWorker 启动后立即清理一次，随后按周期清理，运行 context 取消时停止。
func (s *ConcurrencyService) StartSlotCleanupWorker(interval time.Duration) {
	if s == nil || s.cache == nil || interval <= 0 {
		return
	}
	s.runtime.Start(RuntimeTask{Name: "slot-cleanup", Interval: interval, Immediate: true, Run: func(ctx context.Context) {
		cleanup, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := s.cache.CleanupExpiredProviderSlotKeys(cleanup); err != nil {
			s.diagnostics.printf("service.concurrency", "Warning: cleanup expired provider slots failed: %v", err)
		}
	}})
}

// Stop 使用 30 秒预算停止任务，应用可通过 StopContext 传入剩余关闭预算。
func (s *ConcurrencyService) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = s.StopContext(ctx)
}

func (s *ConcurrencyService) StopContext(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.runtime.StopContext(ctx)
}

// GetProviderConcurrencyBatch 批量读取提供商的当前并发数。
// 查询使用独立的超时 context，HTTP 请求取消后继续完成查询，以便返回并发计数。
func (s *ConcurrencyService) GetProviderConcurrencyBatch(ctx context.Context, providerIDs []int64) (map[int64]int, error) {
	if len(providerIDs) == 0 {
		return map[int64]int{}, nil
	}
	if s.cache == nil {
		result := make(map[int64]int, len(providerIDs))
		for _, providerID := range providerIDs {
			result[providerID] = 0
		}
		return result, nil
	}

	// Use a detached context so that a cancelled HTTP request doesn't cause
	// the Redis pipeline to fail and return all-zero concurrency counts.
	operation, done, err := s.runtime.Enter(context.Background(), "GetProviderConcurrencyBatch")
	if err != nil {
		return nil, err
	}
	defer done()
	redisCtx, cancel := context.WithTimeout(operation, 3*time.Second)
	defer cancel()

	return s.cache.GetProviderConcurrencyBatch(redisCtx, providerIDs)
}

// EnterUserWait 与 EnterProviderWait 将等待计数的释放责任交给 scheduler。
func (s *ConcurrencyService) EnterUserWait(ctx context.Context, id int64, limit int) (WaitResult, error) {
	operation, done, err := s.runtime.Enter(ctx, "EnterUserWait")
	if err != nil {
		return WaitResult{}, err
	}
	result, err := EnterUserWait(operation, s.cache, id, limit, s.diagnostics)
	if err != nil || !result.Allowed {
		done()
		return result, err
	}
	previous := result.resource
	var release func()
	if previous != nil {
		release = previous.Release
	}
	result.resource = NewLease(context.Background(), ReleaseOnCompletion, done, release)
	return result, nil
}

func (s *ConcurrencyService) EnterProviderWait(ctx context.Context, id int64, limit int) (WaitResult, error) {
	operation, done, err := s.runtime.Enter(ctx, "EnterProviderWait")
	if err != nil {
		return WaitResult{}, err
	}
	result, err := EnterProviderWait(operation, s.cache, id, limit, s.diagnostics)
	if err != nil || !result.Allowed {
		done()
		return result, err
	}
	previous := result.resource
	var release func()
	if previous != nil {
		release = previous.Release
	}
	result.resource = NewLease(context.Background(), ReleaseOnCompletion, done, release)
	return result, nil
}

// LiveLeases 返回长会话的租约接口。
func (s *ConcurrencyService) LiveLeases() LiveConcurrencyCache {
	if s == nil || s.cache == nil {
		return nil
	}
	cache, _ := s.cache.(LiveConcurrencyCache)
	return cache
}

// AcquireProviderSlot 在 I/O 前登记，返回时将停止等待责任转交给实际槽位租约。
func (s *ConcurrencyService) AcquireProviderSlot(ctx context.Context, providerID int64, limit int) (*AcquireResult, error) {
	operation, done, err := s.runtime.Enter(ctx, fmt.Sprintf("provider-slot:%d", providerID))
	if err != nil {
		return nil, err
	}
	result, err := s.acquireProviderSlot(operation, providerID, limit)
	if err != nil || result == nil || !result.Acquired {
		done()
		return result, err
	}
	release := NewLease(context.Background(), ReleaseOnCompletion, done, result.ReleaseFunc).Release
	// 只补偿应用停止时的在途获取；普通请求取消仍由调用方释放模式处理。
	if stopErr := s.runtime.Context().Err(); stopErr != nil {
		release()
		return nil, stopErr
	}
	ownRequestResource(ctx, release)
	result.ReleaseFunc = release
	return result, nil
}

// AcquireUserSlot 在 I/O 前登记，返回时将停止等待责任转交给实际槽位租约。
func (s *ConcurrencyService) AcquireUserSlot(ctx context.Context, userID int64, limit int) (*AcquireResult, error) {
	operation, done, err := s.runtime.Enter(ctx, fmt.Sprintf("user-slot:%d", userID))
	if err != nil {
		return nil, err
	}
	result, err := s.acquireUserSlot(operation, userID, limit)
	if err != nil || result == nil || !result.Acquired {
		done()
		return result, err
	}
	release := NewLease(context.Background(), ReleaseOnCompletion, done, result.ReleaseFunc).Release
	// 只补偿应用停止时的在途获取；普通请求取消仍由调用方释放模式处理。
	if stopErr := s.runtime.Context().Err(); stopErr != nil {
		release()
		return nil, stopErr
	}
	result.ReleaseFunc = release
	return result, nil
}

// TrackAPIKeySlot 在统计查询的超时预算内登记槽位，运行实例关闭后拒绝新的登记。
func (s *ConcurrencyService) TrackAPIKeySlot(ctx context.Context, id int64) func() {
	if s == nil {
		return func() {}
	}
	operation, done, err := s.runtime.Enter(context.WithoutCancel(nonNilContext(ctx)), fmt.Sprintf("apikey-slot:%d", id))
	if err != nil {
		return func() {}
	}
	release := s.trackAPIKeySlot(operation, id)
	return NewLease(context.Background(), ReleaseOnCompletion, done, release).Release
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
