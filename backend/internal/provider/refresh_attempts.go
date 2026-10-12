package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
)

// RefreshTokenOperation 是兼容直接刷新路径的最小交换能力。
type RefreshTokenOperation interface {
	Refresh(context.Context, *Record) (map[string]any, error)
}
type ProviderRefreshAttemptGate interface {
	RefreshAttemptGate
	AcquireRate(context.Context) (func(), error)
}

// GrokRefreshMutationWriter 提供 Grok 的条件更新和失败分类，其他平台使用通用失败写入。
type GrokRefreshMutationWriter interface {
	SetGrokOAuthRefreshErrorIfCredentialsUnchanged(context.Context, int64, map[string]any, *int64, string) (bool, error)
	SetGrokOAuthRefreshTempUnschedulableIfCredentialsUnchanged(context.Context, int64, map[string]any, *int64, time.Time, string) (bool, error)
}

// RefreshAttempts 拥有单提供商的尝试、预算、重试、失败隔离与原后置动作顺序；共享 API 由 app 注入。
type RefreshAttempts struct {
	API                               *OAuthRefreshAPI
	Tuning                            *RefreshTuning
	Policy                            BackgroundRefreshPolicy
	AttemptTimeout                    time.Duration
	Now                               func() time.Time
	Info, Warn, Error                 func(string, ...any)
	NonRetryable, SharedProviderError func(error) bool
	AmbiguousEntitlement              func(*Record, error) bool
	Persist                           func(context.Context, *Record, map[string]any) error
	FailureWriter                     RefreshFailureWriter
	GrokMutation                      GrokRefreshMutationWriter
	Invalidate                        func(context.Context, *Record) error
	PrepareFailure                    func(*Record) func(time.Time, string)
	ClearRefreshRequest               func(context.Context, *Record, string)
	PostActions, SyncCleanup          func(context.Context, *Record)
}

type rateLimitedRefreshExecutor struct {
	OAuthRefreshExecutor
	acquireRate func(context.Context) (func(), error)
}

func (s RefreshAttempts) Run(
	ctx context.Context,
	provider *Record,
	refresher RefreshTokenOperation,
	executor OAuthRefreshExecutor,
	refreshWindow time.Duration,
	gate RefreshAttemptGate,
) error {
	var lastErr error
	var failureProvider *Record
	maxRetries := s.Tuning.Retries()

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		releaseAttempt := func() {}
		var acquireRate func(context.Context) (func(), error)
		if gate != nil {
			if providerGate, ok := gate.(ProviderRefreshAttemptGate); ok {
				var err error
				releaseAttempt, err = providerGate.Acquire(ctx)
				if err != nil {
					return err
				}
				acquireRate = providerGate.AcquireRate
			} else {
				// 兼容门只负责速率准入，仅在即将真正调用上游 Refresh 时获取。
				acquireRate = gate.Acquire
			}
		}
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, s.AttemptTimeout)
		failureProvider = nil
		var newCredentials map[string]any
		var err error
		shortCircuit := false
		credentialsPersisted := false

		// 优先使用统一 API（带分布式锁 + DB 重读保护）
		if s.API != nil && executor != nil {
			actualExecutor := executor
			if acquireRate != nil {
				actualExecutor = &rateLimitedRefreshExecutor{
					OAuthRefreshExecutor: executor,
					acquireRate:          acquireRate,
				}
			}
			result, refreshErr := s.API.RefreshWithAttemptSnapshot(attemptCtx, provider, actualExecutor, refreshWindow)
			if result != nil && result.Provider != nil {
				provider = result.Provider
				failureProvider = CloneRecord(provider)
			}
			if refreshErr != nil {
				err = refreshErr
			} else if result.LockHeld {
				// 锁被其他 worker 持有，由调用侧策略决定如何计数
				err = s.Policy.HandleLockHeld()
				shortCircuit = true
			} else if !result.Refreshed {
				// 已被其他路径刷新，由调用侧策略决定如何计数
				err = s.Policy.HandleAlreadyRefreshed()
				shortCircuit = true
			} else {
				credentialsPersisted = result.NewCredentials != nil
				_ = result.NewCredentials // 统一 API 已设置 _token_version 并更新 DB，无需重复操作
			}
		} else {
			// 未配置协调器时调用 refresher。
			failureProvider = CloneRecord(provider)
			releaseRate := func() {}
			if acquireRate != nil {
				releaseRate, err = acquireRate(attemptCtx)
			}
			if err == nil {
				newCredentials, err = refresher.Refresh(attemptCtx, provider)
			}
			if releaseRate != nil {
				releaseRate()
			}
			attemptTimedOut := errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
			if err == nil && newCredentials != nil && !attemptTimedOut {
				newCredentials["_token_version"] = s.Now().UnixMilli()
				if saveErr := s.Persist(attemptCtx, provider, newCredentials); saveErr != nil {
					err = fmt.Errorf("failed to save credentials: %w", saveErr)
				} else {
					credentialsPersisted = true
				}
			}
		}
		attemptTimedOut := errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
		cancelAttempt()
		releaseAttempt()
		persistedAfterAttemptDeadline := attemptTimedOut && credentialsPersisted && err == nil
		if attemptTimedOut && !persistedAfterAttemptDeadline && !IsProviderScopedTerminalRefreshError(err) {
			cause := err
			if cause == nil {
				cause = context.DeadlineExceeded
			}
			err = &RefreshAttemptTimeoutError{Cause: cause}
			shortCircuit = false
			if credentialsPersisted {
				s.SyncCleanup(ctx, provider)
			}
		}
		if shortCircuit {
			return err
		}

		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				if credentialsPersisted {
					s.SyncCleanup(ctx, provider)
				}
				return ctxErr
			}
			if persistedAfterAttemptDeadline {
				// 上游结果与精确状态 CAS 已持久化；仅内部尝试预算在有界清理期间耗尽，不能把成功转换为重试、冷却或熔断证据。
				s.SyncCleanup(ctx, provider)
				return nil
			}
			s.PostActions(ctx, provider)
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			if credentialsPersisted {
				s.SyncCleanup(ctx, provider)
			}
			return ctxErr
		}
		if errors.Is(err, ErrRefreshSkipped) {
			return ErrRefreshSkipped
		}
		if IsProviderScopedTerminalRefreshError(err) {
			return err
		}
		if _, ok := errors.AsType[*RefreshStateUnavailableError](err); ok {
			return &ProviderCycleContainmentRefreshError{Cause: err}
		}
		if s.AmbiguousEntitlement(provider, err) {
			// Grok 客户端将 token 端点的所有 403 标为权益拒绝，缺少归属证据时本轮暂停该平台，提供商保持当前状态。
			return &ProviderCycleContainmentRefreshError{Cause: err}
		}

		// 平台级 OAuth client 或 scope 故障不能证明所有提供商无效；返回类型化内部信号，仅隔离平台而不修改提供商状态。
		if s.SharedProviderError(err) {
			return &ProviderConfigurationRefreshError{Cause: err}
		}

		// 不可重试错误（invalid_grant/invalid_client 等）直接标记 error 状态并返回
		if s.NonRetryable(err) {
			errorMsg := "Token refresh failed (non-retryable): " + logredact.RedactText(err.Error())
			isGrokOAuth := provider.IsGrokOAuth()
			publishFailure := s.PrepareFailure(failureProvider)
			persistentlyBlocked := false
			var setErr error
			if isGrokOAuth {
				conditionalRepo := s.GrokMutation
				ok := conditionalRepo != nil
				if !ok {
					return &ProviderConfigurationRefreshError{
						Cause: errors.New("grok OAuth conditional refresh mutation repository is not configured"),
					}
				}
				persistentlyBlocked, setErr = conditionalRepo.SetGrokOAuthRefreshErrorIfCredentialsUnchanged(
					ctx,
					provider.ID,
					provider.Credentials,
					provider.ProxyID,
					errorMsg,
				)
				if setErr == nil && !persistentlyBlocked {
					s.Info("token_refresh.grok_error_status_skipped_stale_credentials", "provider_id", provider.ID)
					return ErrRefreshSkipped
				}
			} else {
				if failureProvider == nil {
					return err
				}
				writer := s.FailureWriter
				ok := writer != nil
				if !ok {
					return &ProviderConfigurationRefreshError{Cause: errors.New("OAuth conditional refresh failure writer is not configured")}
				}
				persistentlyBlocked, setErr = writer.ApplyOAuthRefreshFailure(ctx, FailureVersion(failureProvider), RefreshFailure{Kind: RefreshFailurePermanent, Message: errorMsg})
				if setErr == nil && !persistentlyBlocked {
					s.Info("token_refresh.error_status_skipped_stale_credentials", "provider_id", provider.ID)
					return ErrRefreshSkipped
				}
			}
			if setErr != nil {
				s.Error("token_refresh.set_error_status_failed",
					"provider_id", provider.ID,
					"error", setErr,
				)
				if isGrokOAuth {
					return &ProviderCycleContainmentRefreshError{
						Cause: fmt.Errorf("failed to conditionally persist Grok OAuth refresh failure: %w", setErr),
					}
				}
			} else if persistentlyBlocked {
				s.ClearRefreshRequest(ctx, failureProvider, "non_retryable")
				publishFailure(time.Time{}, "token_refresh_non_retryable")
			}
			cacheInvalidationFailed := false
			if provider.Type == ProviderTypeOAuth && (!isGrokOAuth || persistentlyBlocked) {
				if s.Invalidate == nil {
					cacheInvalidationFailed = true
				} else if invalidateErr := s.Invalidate(ctx, provider); invalidateErr != nil {
					cacheInvalidationFailed = true
					s.Warn("token_refresh.invalidate_failed_token_cache_failed",
						"provider_id", provider.ID,
						"error", logredact.RedactText(invalidateErr.Error()),
					)
				}
			}
			return &ProviderPermanentRefreshError{
				Cause:                   err,
				PersistentlyBlocked:     persistentlyBlocked,
				CacheInvalidationFailed: cacheInvalidationFailed,
			}
		}

		lastErr = err
		s.Warn("token_refresh.retry_attempt_failed",
			"provider_id", provider.ID,
			"attempt", attempt,
			"max_retries", maxRetries,
			"error", logredact.RedactText(err.Error()),
		)

		// 如果还有重试机会，等待后重试
		if attempt < maxRetries {
			backoff := s.Tuning.RetryBackoff(provider.ID, attempt)
			if backoff > 0 {
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// 可重试错误耗尽时临时停调该提供商，后续请求会跳过它。
	s.Warn("token_refresh.retry_exhausted",
		"provider_id", provider.ID,
		"platform", provider.Platform,
		"max_retries", maxRetries,
		"error", logredact.RedactText(lastErr.Error()),
	)

	// 设置临时不可调度 10 分钟（不标记 error，保持 status=active 让下个刷新周期能继续尝试）
	until := s.Now().Add(TokenRefreshTempUnschedDuration)
	reason := "token refresh retry exhausted"
	if lastErr != nil {
		reason += ": " + logredact.RedactText(lastErr.Error())
	}
	publishFailure := s.PrepareFailure(failureProvider)
	if provider.IsGrokOAuth() {
		conditionalRepo := s.GrokMutation
		ok := conditionalRepo != nil
		if !ok {
			return &ProviderConfigurationRefreshError{
				Cause: errors.New("grok OAuth conditional refresh mutation repository is not configured"),
			}
		}
		applied, setErr := conditionalRepo.SetGrokOAuthRefreshTempUnschedulableIfCredentialsUnchanged(
			ctx,
			provider.ID,
			provider.Credentials,
			provider.ProxyID,
			until,
			reason,
		)
		if setErr != nil {
			s.Warn("token_refresh.set_temp_unschedulable_failed",
				"provider_id", provider.ID,
				"error", setErr,
			)
			return &ProviderCycleContainmentRefreshError{
				Cause: fmt.Errorf("failed to conditionally persist Grok OAuth refresh cooldown: %w", setErr),
			}
		} else if !applied {
			s.Info("token_refresh.grok_temp_unschedulable_skipped_stale_credentials", "provider_id", provider.ID)
			return ErrRefreshSkipped
		}
		publishFailure(until, "token_refresh_retry_exhausted")
		s.Info("token_refresh.temp_unschedulable_set",
			"provider_id", provider.ID,
			"until", until.Format(time.RFC3339),
		)
		return lastErr
	}

	// 排队或读取阶段尚未取得交换快照，失败时保持提供商状态不变。
	if failureProvider == nil {
		return lastErr
	}

	writer := s.FailureWriter
	ok := writer != nil
	if !ok {
		return &ProviderConfigurationRefreshError{Cause: errors.New("OAuth conditional refresh failure writer is not configured")}
	}
	applied, setErr := writer.ApplyOAuthRefreshFailure(ctx, FailureVersion(failureProvider), RefreshFailure{Kind: RefreshFailureCooldown, Message: reason, Until: until})
	if setErr == nil && !applied {
		s.Info("token_refresh.temp_unschedulable_skipped_stale_credentials", "provider_id", provider.ID)
		return ErrRefreshSkipped
	}
	if setErr != nil {
		s.Warn("token_refresh.set_temp_unschedulable_failed",
			"provider_id", provider.ID,
			"error", setErr,
		)
	} else {
		publishFailure(until, "token_refresh_retry_exhausted")
		s.Info("token_refresh.temp_unschedulable_set",
			"provider_id", provider.ID,
			"until", until.Format(time.RFC3339),
		)
	}

	return lastErr
}

func (e *rateLimitedRefreshExecutor) Refresh(ctx context.Context, provider *Record) (map[string]any, error) {
	if e == nil || e.OAuthRefreshExecutor == nil {
		return nil, errors.New("OAuth refresh executor is not configured")
	}
	release := func() {}
	if e.acquireRate != nil {
		var err error
		release, err = e.acquireRate(ctx)
		if err != nil {
			return nil, err
		}
	}
	defer release()
	return e.OAuthRefreshExecutor.Refresh(ctx, provider)
}
