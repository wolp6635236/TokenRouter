package scheduler

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// UserMsgQueueCache 用户消息串行队列 Redis 缓存接口。
type UserMsgQueueCache interface {
	// AcquireLock 尝试获取提供商级串行锁
	AcquireLock(ctx context.Context, providerID int64, requestID string, lockTtlMs int) (acquired bool, err error)
	// ReleaseLock 释放锁并记录完成时间
	ReleaseLock(ctx context.Context, providerID int64, requestID string) (released bool, err error)
	// GetLastCompletedMs 获取上次完成时间（毫秒时间戳，Redis TIME 源）
	GetLastCompletedMs(ctx context.Context, providerID int64) (int64, error)
	// GetCurrentTimeMs 获取 Redis 服务器当前时间（毫秒），与 ReleaseLock 记录的时间源一致
	GetCurrentTimeMs(ctx context.Context) (int64, error)
	// ReconcileExpiredLockCandidates 处理锁索引中的到期候选，按 Redis 返回的 PTTL 清理或刷新索引。
	ReconcileExpiredLockCandidates(ctx context.Context, maxCount int) (cleaned int, err error)
}

// QueueLockResult 锁获取结果。
type QueueLockResult struct {
	Acquired  bool
	RequestID string
}

// UserMessageQueueService 用户消息串行队列服务
// 用户消息按提供商串行执行，并根据 RPM 调整发送间隔。
type UserMessageQueueService struct {
	runtime     WorkerRuntime
	diagnostics Diagnostics
	cache       UserMsgQueueCache
	rpmCache    RPMCache
	cfg         *MessageQueueOptions
}

// MessageQueueOptions 保存消息队列的静态时间参数。
type MessageQueueOptions struct {
	LockTTLMs  int
	MinDelayMs int
	MaxDelayMs int
}

// NewUserMessageQueueService 创建用户消息串行队列服务。
func NewUserMessageQueueService(cache UserMsgQueueCache, rpmCache RPMCache, cfg *MessageQueueOptions, options ...Diagnostics) *UserMessageQueueService {
	var diagnostics Diagnostics
	if len(options) > 0 {
		diagnostics = options[0]
	}
	return &UserMessageQueueService{
		cache:       cache,
		rpmCache:    rpmCache,
		cfg:         cfg,
		diagnostics: diagnostics,
	}
}

// TryAcquire 尝试立即获取串行锁。
func (s *UserMessageQueueService) TryAcquire(ctx context.Context, providerID int64) (*QueueLockResult, error) {
	operation, done, err := s.runtime.Enter(ctx, "TryAcquire")
	if err != nil {
		return nil, err
	}
	defer done()
	ctx = operation
	if s.cache == nil {
		return &QueueLockResult{Acquired: true}, nil // fail-open
	}

	requestID := generateUMQRequestID()
	lockTTL := s.cfg.LockTTLMs
	if lockTTL <= 0 {
		lockTTL = 120000
	}

	acquired, err := s.cache.AcquireLock(ctx, providerID, requestID, lockTTL)
	if err != nil {
		s.diagnostics.printf("service.umq", "AcquireLock failed for provider %d: %v", providerID, err)
		return &QueueLockResult{Acquired: true}, nil // fail-open
	}

	return &QueueLockResult{
		Acquired:  acquired,
		RequestID: requestID,
	}, nil
}

// Release 释放串行锁。
func (s *UserMessageQueueService) Release(ctx context.Context, providerID int64, requestID string) error {
	if s.cache == nil || requestID == "" {
		return nil
	}
	released, err := s.cache.ReleaseLock(ctx, providerID, requestID)
	if err != nil {
		s.diagnostics.printf("service.umq", "ReleaseLock failed for provider %d: %v", providerID, err)
		return err
	}
	if !released {
		s.diagnostics.printf("service.umq", "ReleaseLock no-op for provider %d (requestID mismatch or expired)", providerID)
	}
	return nil
}

// EnforceDelay 根据 RPM 负载执行自适应延迟
// 时间取自 Redis TIME，与 releaseLockScript 使用同一个时间源。
func (s *UserMessageQueueService) EnforceDelay(ctx context.Context, providerID int64, baseRPM int) error {
	operation, done, err := s.runtime.Enter(ctx, "EnforceDelay")
	if err != nil {
		return err
	}
	defer done()
	ctx = operation
	if s.cache == nil {
		return nil
	}

	// 先检查历史记录，没有历史时立即返回零延迟。
	lastMs, err := s.cache.GetLastCompletedMs(ctx, providerID)
	if err != nil {
		s.diagnostics.printf("service.umq", "GetLastCompletedMs failed for provider %d: %v", providerID, err)
		return nil // fail-open
	}
	if lastMs == 0 {
		return nil // 没有历史记录，无需延迟
	}

	delay := s.CalculateRPMAwareDelay(ctx, providerID, baseRPM)
	if delay <= 0 {
		return nil
	}

	// 当前时间与 lastMs 都取自 Redis。
	nowMs, err := s.cache.GetCurrentTimeMs(ctx)
	if err != nil {
		s.diagnostics.printf("service.umq", "GetCurrentTimeMs failed: %v", err)
		return nil // fail-open
	}

	elapsed := time.Duration(nowMs-lastMs) * time.Millisecond
	if elapsed < 0 {
		// 时钟异常（Redis 故障转移等），fail-open
		return nil
	}
	remaining := delay - elapsed
	if remaining <= 0 {
		return nil
	}

	// 执行延迟
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// CalculateRPMAwareDelay 根据当前 RPM 负载计算自适应延迟
// ratio = currentRPM / baseRPM
// ratio < 0.5  → MinDelay
// 0.5 ≤ ratio < 0.8 → 线性插值 MinDelay..MaxDelay
// ratio ≥ 0.8 → MaxDelay
// 返回值加入 ±15% 的随机抖动，分散请求时间并减少固定间隔特征。
func (s *UserMessageQueueService) CalculateRPMAwareDelay(ctx context.Context, providerID int64, baseRPM int) time.Duration {
	minDelay := time.Duration(s.cfg.MinDelayMs) * time.Millisecond
	maxDelay := time.Duration(s.cfg.MaxDelayMs) * time.Millisecond

	if minDelay <= 0 {
		minDelay = 200 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 2000 * time.Millisecond
	}
	// 防止配置错误：minDelay > maxDelay 时交换
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	var baseDelay time.Duration

	if baseRPM <= 0 || s.rpmCache == nil {
		baseDelay = minDelay
	} else {
		currentRPM, err := s.rpmCache.GetRPM(ctx, providerID)
		if err != nil {
			s.diagnostics.printf("service.umq", "GetRPM failed for provider %d: %v", providerID, err)
			baseDelay = minDelay // fail-open
		} else {
			ratio := float64(currentRPM) / float64(baseRPM)
			if ratio < 0.5 {
				baseDelay = minDelay
			} else if ratio >= 0.8 {
				baseDelay = maxDelay
			} else {
				// 线性插值: 0.5 → minDelay, 0.8 → maxDelay
				t := (ratio - 0.5) / 0.3
				interpolated := float64(minDelay) + t*(float64(maxDelay)-float64(minDelay))
				baseDelay = time.Duration(math.Round(interpolated))
			}
		}
	}

	// ±15% 随机抖动
	return applyJitter(baseDelay, 0.15)
}

// StartCleanupWorker 在首个周期结束后开始清理，停止时取消任务并等待退出。
func (s *UserMessageQueueService) StartCleanupWorker(interval time.Duration) {
	if s == nil || s.cache == nil || interval <= 0 {
		return
	}
	s.runtime.Start(RuntimeTask{Name: "message-queue-cleanup", Interval: interval, Run: func(ctx context.Context) {
		cleanup, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cleaned, err := s.cache.ReconcileExpiredLockCandidates(cleanup, 1000)
		if err != nil {
			s.diagnostics.printf("service.umq", "Cleanup reconcile failed: %v", err)
			return
		}
		if cleaned > 0 {
			s.diagnostics.printf("service.umq", "Cleanup completed: released %d orphaned locks", cleaned)
		}
	}})
}

func (s *UserMessageQueueService) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = s.StopContext(ctx)
}

func (s *UserMessageQueueService) StopContext(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.runtime.StopContext(ctx)
}

func applyJitter(d time.Duration, jitterPct float64) time.Duration {
	if d <= 0 || jitterPct <= 0 {
		return d
	}
	// [-jitterPct, +jitterPct]
	jitter := (rand.Float64()*2 - 1) * jitterPct
	return time.Duration(float64(d) * (1 + jitter))
}

// generateUMQRequestID 生成唯一请求 ID（与 generateRequestID 一致的 fallback 模式）。
func generateUMQRequestID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
