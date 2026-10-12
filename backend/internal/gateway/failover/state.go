package failover

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	// FailoverContinue 继续循环（同提供商重试或切换提供商，调用方统一 continue）。
	FailoverContinue FailoverAction = iota
	// FailoverExhausted 切换次数耗尽（调用方应返回错误响应）。
	FailoverExhausted
	// FailoverCanceled context 已取消（调用方应直接 return）。
	FailoverCanceled

	// MaxSameProviderRetries 是 RetryableOnSameProvider 错误的默认重试上限。
	// 生产调用方可传入 provider.GetPoolModeRetryCount()，测试和缺省调用使用该值。
	MaxSameProviderRetries = 3
	// SameProviderRetryDelay 同提供商重试间隔。
	SameProviderRetryDelay = 500 * time.Millisecond
	// MaxRequestScopedRetryDelay 限制请求级瞬时错误的指数退避时长，高重试次数下也按此上限等待。
	MaxRequestScopedRetryDelay = 8 * time.Second
	// SingleProviderBackoffDelay 单提供商分组 503 退避重试固定延时。
	// Service 层在 SingleProviderRetry 模式下已做充分原地重试（最多 3 次、总等待 30s），
	// Handler 层只需短暂间隔后重新进入 Service 层即可。
	SingleProviderBackoffDelay = 2 * time.Second
)

// FailureInfo 不携带平台响应、凭据或 HTTP 对象。
type FailureInfo struct {
	StatusCode                int
	ForceCacheBilling         bool
	RetryableOnSameProvider   bool
	RequestScopedTransient    bool
	SameProviderRetryDelay    time.Duration
	SameProviderRetryDeadline time.Time
	SameProviderRetryMax      int
	RetryNext                 bool
}
type Failure interface {
	comparable
	RetryFailure() *FailureInfo
}
type Observe func(context.Context, string, map[string]any)

// TempUnscheduler 用于 HandleFailoverError 中同提供商重试耗尽后的临时封禁。
// 执行适配器提供提供商读取与故障转移所需的窄接口。
type TempUnscheduler[E Failure] interface {
	TempUnscheduleRetryableError(ctx context.Context, providerID int64, failoverErr E)
}

// FailoverAction 表示 failover 错误处理后的下一步动作。
type FailoverAction int

// FailoverState 跨循环迭代共享的 failover 状态。
type FailoverState[E Failure] struct {
	observe                Observe
	SwitchCount            int
	MaxSwitches            int
	FailedProviderIDs      map[int64]struct{}
	SameProviderRetryCount map[int64]int
	LastFailoverErr        E
	ForceCacheBilling      bool
	HasBoundSession        bool
}

// SameProviderRetryDelayFor 为请求级瞬时错误计算有上限的指数退避；
// 其它同提供商错误继续使用固定 500ms，保持既有重试时延。
func SameProviderRetryDelayFor(failoverErr *FailureInfo, retryCount int) time.Duration {
	if failoverErr != nil && failoverErr.SameProviderRetryDelay > 0 {
		return failoverErr.SameProviderRetryDelay
	}
	if failoverErr == nil || !failoverErr.RequestScopedTransient || retryCount <= 1 {
		return SameProviderRetryDelay
	}

	delay := SameProviderRetryDelay
	for i := 1; i < retryCount; i++ {
		if delay >= MaxRequestScopedRetryDelay/2 {
			return MaxRequestScopedRetryDelay
		}
		delay *= 2
	}
	return delay
}

func SameProviderRetryAllowed(failoverErr *FailureInfo, retryCount, retryLimit int) bool {
	if failoverErr == nil || !failoverErr.RetryableOnSameProvider {
		return false
	}
	if !SameProviderRetryDeadlineAllows(failoverErr) {
		return false
	}
	// 错误级上限（Grok 容量/流空闲）即使带有重建的 deadline 也必须生效。
	if failoverErr.SameProviderRetryMax > 0 {
		if retryLimit <= 0 {
			return false
		}
		if failoverErr.SameProviderRetryMax < retryLimit {
			retryLimit = failoverErr.SameProviderRetryMax
		}
		return retryCount < retryLimit
	}
	// OAuth 429 明确使用时间窗口，不受普通池重试次数限制。
	if !failoverErr.SameProviderRetryDeadline.IsZero() {
		return true
	}
	return retryLimit > 0 && retryCount < retryLimit
}

// SameProviderRetryDeadlineAllows 检查调用方提供的重试截止时间。
func SameProviderRetryDeadlineAllows(failoverErr *FailureInfo) bool {
	return failoverErr == nil || failoverErr.SameProviderRetryDeadline.IsZero() || time.Now().Before(failoverErr.SameProviderRetryDeadline)
}

// NewFailoverState 创建 failover 状态。
func NewFailoverState[E Failure](maxSwitches int, hasBoundSession bool, observers ...Observe) *FailoverState[E] {
	var observe Observe
	if len(observers) > 0 {
		observe = observers[0]
	}
	return &FailoverState[E]{
		observe:                observe,
		MaxSwitches:            maxSwitches,
		FailedProviderIDs:      make(map[int64]struct{}),
		SameProviderRetryCount: make(map[int64]int),
		HasBoundSession:        hasBoundSession,
	}
}

// HandleFailoverError 处理 UpstreamFailoverError，返回下一步动作。
// 包含：缓存计费判断、同提供商重试、临时封禁、切换计数、Antigravity 延时。
func (s *FailoverState[E]) HandleFailoverError(
	ctx context.Context,
	gatewayService TempUnscheduler[E],
	providerID int64,
	platform string,
	retryLimit int,
	failoverErr E,
) FailoverAction {
	// 客户端已断开：failover 只会用已取消的 context 重新选号并必然失败，
	// 不应再被当成提供商耗尽处理（误报 502）。
	if ctx != nil && ctx.Err() != nil {
		return FailoverCanceled
	}
	s.LastFailoverErr = failoverErr
	failure := failoverErr.RetryFailure()
	if failure == nil || !failure.RetryNext {
		return FailoverExhausted
	}

	// 同提供商重试不算切换提供商，粘性会话仅在实际切换时强制缓存计费。
	retryCount := s.SameProviderRetryCount[providerID]
	sameProviderRetry := SameProviderRetryAllowed(failure, retryCount, retryLimit)
	if NeedForceCacheBilling(s.HasBoundSession, failure, sameProviderRetry) {
		s.ForceCacheBilling = true
	}

	// 同提供商重试：对 RetryableOnSameProvider 的临时性错误，先在同一提供商上重试。
	// 重试次数上限 retryLimit 由调用方传入（提供商级 pool_mode_retry_count 配置）。
	if sameProviderRetry {
		s.SameProviderRetryCount[providerID]++
		retryDelay := SameProviderRetryDelayFor(failure, s.SameProviderRetryCount[providerID])
		s.emit(ctx, "gateway.failover_same_provider_retry", map[string]any{"provider_id": providerID, "upstream_status": failure.StatusCode, "same_provider_retry_count": s.SameProviderRetryCount[providerID], "same_provider_retry_max": retryLimit, "retry_delay": retryDelay})
		if !SleepWithContext(ctx, retryDelay) {
			return FailoverCanceled
		}
		return FailoverContinue
	}

	// 同提供商重试用尽，执行临时封禁
	if failure.RetryableOnSameProvider {
		gatewayService.TempUnscheduleRetryableError(ctx, providerID, failoverErr)
	}

	// 加入失败列表
	s.FailedProviderIDs[providerID] = struct{}{}

	// 检查是否耗尽
	if s.SwitchCount >= s.MaxSwitches {
		return FailoverExhausted
	}

	// 递增切换计数
	s.SwitchCount++
	s.emit(ctx, "gateway.failover_switch_provider", map[string]any{"provider_id": providerID, "upstream_status": failure.StatusCode, "switch_count": s.SwitchCount, "max_switches": s.MaxSwitches})

	// Antigravity 平台换号线性递增延时
	if platform == capability.PlatformAntigravity {
		delay := time.Duration(s.SwitchCount-1) * time.Second
		if !SleepWithContext(ctx, delay) {
			return FailoverCanceled
		}
	}

	return FailoverContinue
}

// HandleSelectionExhausted 处理选号失败（所有候选提供商都在排除列表中）时的退避重试决策。
// 针对 Antigravity 单提供商分组的 503 (MODEL_CAPACITY_EXHAUSTED) 场景：
// 清除排除列表、等待退避后重新选号。
//
// 返回 FailoverContinue 时，调用方应设置 SingleProviderRetry context 并 continue。
// 返回 FailoverExhausted 时，调用方应返回错误响应。
// 返回 FailoverCanceled 时，调用方应直接 return。
func (s *FailoverState[E]) HandleSelectionExhausted(ctx context.Context) FailoverAction {
	// 客户端已断开时选号失败是 context canceled 的必然结果，
	// 不代表提供商耗尽，直接按取消终止。
	if ctx.Err() != nil {
		return FailoverCanceled
	}

	failure := s.LastFailoverErr.RetryFailure()
	if failure != nil &&
		failure.StatusCode == 503 &&
		s.SwitchCount <= s.MaxSwitches {
		s.emit(ctx, "gateway.failover_single_provider_backoff", map[string]any{"backoff_delay": SingleProviderBackoffDelay, "switch_count": s.SwitchCount, "max_switches": s.MaxSwitches})
		if !SleepWithContext(ctx, SingleProviderBackoffDelay) {
			return FailoverCanceled
		}
		s.emit(ctx, "gateway.failover_single_provider_retry", map[string]any{"switch_count": s.SwitchCount, "max_switches": s.MaxSwitches})
		s.FailedProviderIDs = make(map[int64]struct{})
		return FailoverContinue
	}
	return FailoverExhausted
}

// NeedForceCacheBilling 判断 failover 时是否需要强制缓存计费。
// 粘性会话实际切换提供商、或上游明确标记时，将 input_tokens 转为 cache_read 计费。
func NeedForceCacheBilling(hasBoundSession bool, failoverErr *FailureInfo, sameProviderRetry bool) bool {
	return (hasBoundSession && !sameProviderRetry) || (failoverErr != nil && failoverErr.ForceCacheBilling)
}

// SleepWithContext 等待指定时长，返回 false 表示 context 已取消。
func SleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (s *FailoverState[E]) emit(ctx context.Context, event string, fields map[string]any) {
	if s.observe != nil {
		s.observe(ctx, event, fields)
	}
}

// EffectiveSameProviderRetryLimit 取提供商预算和错误级上限中较小的值。
func EffectiveSameProviderRetryLimit(failure *FailureInfo, limit int) int {
	if limit > 0 && failure != nil && failure.SameProviderRetryMax > 0 && failure.SameProviderRetryMax < limit {
		return failure.SameProviderRetryMax
	}
	return limit
}
