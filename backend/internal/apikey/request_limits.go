package apikey

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TokenFlux/TokenRouter/internal/pkg/requestcontext"
)

var (
	// ErrKeyConcurrencyExceeded 表示该 Key 的活跃请求已达到上限。
	ErrKeyConcurrencyExceeded = errors.New("API key concurrency limit exceeded")
	// ErrKeyRPMExceeded 表示该 Key 在最近一分钟的请求已达到上限。
	ErrKeyRPMExceeded = errors.New("API key RPM limit exceeded")
	// ErrKeyLimiterUnavailable 表示请求限制存储不可用。
	ErrKeyLimiterUnavailable = errors.New("API key request limiter unavailable")
)

// RequestLimitCache 原子预占 Key 的请求额度，并维护活跃请求租约。
type RequestLimitCache interface {
	ReserveRequest(context.Context, int64, string, int, int) (time.Duration, error)
	RefreshRequest(context.Context, int64, string) error
	ReleaseRequest(context.Context, int64, string) error
}

// ValidateRequestLimits 校验 PostgreSQL integer 能保存的非负请求上限。
func ValidateRequestLimits(concurrency, rpm *int) error {
	for field, value := range map[string]*int{"concurrency_limit": concurrency, "rpm_limit": rpm} {
		if value != nil && (*value < 0 || int64(*value) > math.MaxInt32) {
			return ErrAPIKeyLimitInvalid.WithMetadata(map[string]string{"field": field})
		}
	}
	return nil
}

// AcquireRequest 在准入时计入 RPM，并让并发租约存续到 release 被调用。
// 续租失败会取消返回的上下文，调用方应停止处理请求。
// @project-doc docs/domains/routing_and_billing.md#api_key_request_limits
func (s *APIKeyService) AcquireRequest(ctx context.Context, key *APIKey) (context.Context, func(), time.Duration, error) {
	if key.ConcurrencyLimit <= 0 && key.RPMLimit <= 0 {
		return ctx, func() {}, 0, nil
	}
	cache, ok := s.cache.(RequestLimitCache)
	if !ok {
		return ctx, nil, 0, ErrKeyLimiterUnavailable
	}
	if !s.operations.enter() {
		return ctx, nil, 0, ErrKeyLimiterUnavailable
	}
	id := uuid.NewString()
	reserveCtx, cancelReserve := context.WithTimeout(ctx, 3*time.Second)
	retry, err := cache.ReserveRequest(reserveCtx, key.ID, id, key.ConcurrencyLimit, key.RPMLimit)
	cancelReserve()
	if err != nil {
		// 响应丢失时 Redis 可能已经预占，使用独立上下文清理本次并发槽。
		if !errors.Is(err, ErrKeyConcurrencyExceeded) && !errors.Is(err, ErrKeyRPMExceeded) {
			releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 3*time.Second)
			_ = cache.ReleaseRequest(releaseCtx, key.ID, id)
			cancelRelease()
		}
		s.operations.leave()
		return ctx, nil, retry, err
	}
	if key.ConcurrencyLimit <= 0 {
		s.operations.leave()
		return ctx, func() {}, 0, nil
	}
	requestCtx, abort := requestcontext.WithAbort(ctx)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// 图片等处理器可能在断连后继续收集结果，租约维持到处理器返回。
				refreshCtx, cancelRefresh := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				err := cache.RefreshRequest(refreshCtx, key.ID, id)
				cancelRefresh()
				if err != nil {
					abort()
					return
				}
			}
		}
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			defer s.operations.leave()
			close(stop)
			abort()
			<-done
			// 客户端断开后仍需归还租约，清理使用独立的超时预算。
			releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelRelease()
			_ = cache.ReleaseRequest(releaseCtx, key.ID, id)
		})
	}
	return requestCtx, release, 0, nil
}
