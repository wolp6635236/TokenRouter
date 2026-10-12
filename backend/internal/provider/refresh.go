package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultRefreshLockTTL                   = 60 * time.Second
	defaultRefreshLockReleaseTimeout        = 2 * time.Second
	defaultRefreshPostPersistCleanupTimeout = 2 * time.Second
)

var (
	ErrRefreshProviderRereadFailed = errors.New("oauth refresh provider reread failed")
	ErrRefreshProviderStateChanged = errors.New("oauth refresh provider state changed")
	ErrRefreshCredentialPersist    = errors.New("oauth refresh credential persistence failed")
)

// GrokRefreshSuccessWriter 保存 Grok 轮换后的凭据。
// 实现必须比较上游尝试使用的完整凭据和代理，并将成功更新与调度器失效事件原子发布。
type GrokRefreshSuccessWriter interface {
	UpdateGrokOAuthCredentialsIfUnchanged(
		ctx context.Context,
		id int64,
		expectedCredentials map[string]any,
		expectedProxyID *int64,
		credentials map[string]any,
	) (bool, error)
}

type oauthRefreshRequestPathKey struct{}

type RefreshLock struct {
	token chan struct{}
}

type RefreshStateUnavailableError struct {
	err error
}

func WithRefreshRequestPath(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauthRefreshRequestPathKey{}, true)
}

func isOAuthRefreshRequestPath(ctx context.Context) bool {
	requestPath, _ := ctx.Value(oauthRefreshRequestPathKey{}).(bool)
	return requestPath
}

func (e *RefreshStateUnavailableError) Error() string {
	return "OAuth refresh provider state is unavailable"
}

func (e *RefreshStateUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func NewRefreshLock() *RefreshLock {
	return &RefreshLock{token: make(chan struct{}, 1)}
}

func (m *RefreshLock) Lock(ctx context.Context) error {
	select {
	case m.token <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *RefreshLock) Unlock() {
	<-m.token
}

// getLocalLock 返回指定 cacheKey 的进程内互斥锁。
func (api *OAuthRefreshAPI) getLocalLock(cacheKey string) *RefreshLock {
	actual, _ := api.localLocks.LoadOrStore(cacheKey, NewRefreshLock())
	mu, ok := actual.(*RefreshLock)
	if !ok {
		mu = NewRefreshLock()
		api.localLocks.Store(cacheKey, mu)
	}
	return mu
}

// RefreshIfNeeded 在分布式锁保护下按需刷新 OAuth token
//
// 流程:
//  1. 获取分布式锁
//  2. 从 DB 重读最新 provider（防止使用过时的 refresh_token）
//  3. 二次检查是否仍需刷新
//  4. 调用 executor.Refresh() 执行平台特定刷新逻辑
//  5. 设置 _token_version + 更新 DB
//  6. 释放锁
//
// RefreshIfNeeded 检查刷新资格并执行交换，取消时返回取消错误。
func (api *OAuthRefreshAPI) RefreshIfNeeded(ctx context.Context, value *Record, executor OAuthRefreshExecutor, window time.Duration) (*OAuthRefreshResult, error) {
	return api.refreshIfNeeded(ctx, value, executor, window, false)
}

// RefreshWithAttemptSnapshot 仅供周期健康处理取得实际失败身份；取消后仍禁止凭据写入。
func (api *OAuthRefreshAPI) RefreshWithAttemptSnapshot(ctx context.Context, value *Record, executor OAuthRefreshExecutor, window time.Duration) (*OAuthRefreshResult, error) {
	return api.refreshIfNeeded(ctx, value, executor, window, true)
}

func (api *OAuthRefreshAPI) refreshIfNeeded(
	ctx context.Context,
	provider *Record,
	executor OAuthRefreshExecutor,
	refreshWindow time.Duration,
	retainCancelledAttempt bool,
) (*OAuthRefreshResult, error) {
	if api == nil || api.providerRepo == nil {
		return nil, errors.New("oauth refresh provider repository is not configured")
	}
	if provider == nil {
		return nil, errors.New("oauth refresh provider is nil")
	}
	if executor == nil {
		return nil, errors.New("oauth refresh executor is nil")
	}
	ctx, finish, err := api.beginRefresh(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	requestPath := isOAuthRefreshRequestPath(ctx)
	cacheKey := executor.CacheKey(provider)

	release, held, err := api.acquireRefreshLock(ctx, provider.ID, cacheKey)
	if err != nil {
		return nil, err
	}
	if held {
		return &OAuthRefreshResult{LockHeld: true}, nil
	}
	defer release()

	// 2. 在锁内从数据库读取提供商的最新 refresh_token。
	freshProvider, err := api.providerRepo.GetByID(ctx, provider.ID)
	if err != nil {
		if requestPath {
			return nil, fmt.Errorf("%w: %v", ErrRefreshProviderRereadFailed, err)
		}
		return nil, &RefreshStateUnavailableError{err: err}
	}
	if freshProvider == nil {
		if requestPath {
			return nil, fmt.Errorf("%w: provider not found", ErrRefreshProviderStateChanged)
		}
		return nil, &RefreshStateUnavailableError{err: fmt.Errorf("provider not found")}
	}
	if freshProvider.ID != provider.ID {
		return nil, fmt.Errorf("%w: provider identity mismatch", ErrRefreshProviderRereadFailed)
	}
	// 请求期间状态变化时返回错误，后台跳过停用提供商，管理员刷新使用单独入口。
	if !freshProvider.IsActive() {
		if requestPath {
			return nil, fmt.Errorf("%w: provider is not active", ErrRefreshProviderStateChanged)
		}
		return &OAuthRefreshResult{Provider: freshProvider}, nil
	}
	if requestPath && freshProvider.Platform == PlatformGrok {
		if eligibilityErr := api.options.Platform.Eligibility(freshProvider); eligibilityErr != nil {
			return nil, api.options.Platform.SnapshotError(eligibilityErr, freshProvider)
		}
	}
	if !executor.CanRefresh(freshProvider) {
		if requestPath && freshProvider.IsGrokOAuth() && strings.TrimSpace(freshProvider.GetGrokRefreshToken()) == "" {
			return nil, api.options.Platform.SnapshotError(api.options.Platform.MissingRefreshToken(), freshProvider)
		}
		if requestPath {
			return nil, fmt.Errorf("%w: provider is no longer refreshable", ErrRefreshProviderStateChanged)
		}
		return &OAuthRefreshResult{Provider: freshProvider}, nil
	}

	// 3. 二次检查是否仍需刷新（另一条路径可能已刷新）
	if !executor.NeedsRefresh(freshProvider, refreshWindow) {
		return &OAuthRefreshResult{
			Provider: freshProvider,
		}, nil
	}

	// 4. 执行平台特定刷新逻辑
	attemptedProvider := snapshotRefreshRecord(freshProvider)
	if err := ctx.Err(); err != nil {
		if retainCancelledAttempt && !requestPath {
			return &OAuthRefreshResult{Provider: attemptedProvider}, err
		}
		return nil, err
	}
	newCredentials, refreshErr := executor.Refresh(ctx, freshProvider)
	if ctxErr := ctx.Err(); ctxErr != nil {
		// 上游忽略取消并迟到返回时，尝试或刷新周期已结束则丢弃凭据。
		if retainCancelledAttempt && !requestPath {
			return &OAuthRefreshResult{Provider: attemptedProvider}, ctxErr
		}
		return nil, ctxErr
	}
	if refreshErr != nil {
		// 竞争恢复：refresh token 被拒绝可能是另一个 worker 已消费了旧 refresh_token
		// 重新读取 DB，如果 refresh_token 已更新则说明是竞争，返回成功
		if IsInvalidGrantError(refreshErr) {
			if recoveredProvider, recovered := api.tryRecoverFromRefreshRace(ctx, freshProvider); recovered {
				if requestPath && recoveredProvider.Platform == PlatformGrok {
					if eligibilityErr := api.options.Platform.Eligibility(recoveredProvider); eligibilityErr != nil {
						return nil, api.options.Platform.SnapshotError(eligibilityErr, recoveredProvider)
					}
				}
				api.options.Info("oauth_refresh_race_recovered",
					"provider_id", freshProvider.ID,
					"platform", freshProvider.Platform,
				)
				return &OAuthRefreshResult{
					Provider: recoveredProvider,
				}, nil
			}
		}
		// 将交换失败时的提供商快照交给调用方，后续状态更新按该版本比较。
		result := &OAuthRefreshResult{Provider: attemptedProvider}
		if requestPath && attemptedProvider.Platform == PlatformGrok {
			return result, api.options.Platform.SnapshotError(refreshErr, attemptedProvider)
		}
		return result, refreshErr
	}

	// 5. 设置版本号 + 更新 DB
	if newCredentials != nil {
		// 复制 executor.Refresh() 返回的 map 后再合并字段。
		cloned := CloneValues(newCredentials)
		cloned["_token_version"] = api.options.Now().UnixMilli()
		newCredentials = cloned

		if freshProvider.IsGrokOAuth() {
			conditionalRepo, ok := api.providerRepo.(GrokRefreshSuccessWriter)
			if !ok {
				return nil, api.options.Platform.ConfigurationError(fmt.Errorf("grok OAuth refresh success CAS repository is not configured"))
			}
			applied, updateErr := conditionalRepo.UpdateGrokOAuthCredentialsIfUnchanged(
				ctx,
				freshProvider.ID,
				attemptedProvider.Credentials,
				attemptedProvider.ProxyID,
				newCredentials,
			)
			if updateErr != nil {
				api.options.Error("oauth_refresh_update_failed",
					"provider_id", freshProvider.ID,
					"platform", freshProvider.Platform,
					"error", updateErr,
				)
				// 上游可能已轮换并消费 refresh token，本地持久化结果不明时重试会让健康提供商变成 invalid_grant。
				return nil, api.options.Platform.ContainmentError(fmt.Errorf("OAuth refresh succeeded but credential persistence failed: %w", updateErr))
			}
			if !applied {
				currentProvider, readErr := api.providerRepo.GetByID(ctx, freshProvider.ID)
				if readErr != nil || currentProvider == nil {
					if readErr == nil {
						readErr = fmt.Errorf("provider not found after Grok OAuth success CAS miss")
					}
					return nil, api.options.Platform.ContainmentError(fmt.Errorf("grok OAuth success CAS lost and current state is unavailable: %w", readErr))
				}
				api.options.Info("oauth_refresh_success_cas_skipped_stale_credentials",
					"provider_id", freshProvider.ID,
					"platform", freshProvider.Platform,
				)
				return &OAuthRefreshResult{Provider: currentProvider}, nil
			}
			durableProvider, readErr := api.loadGrokDurableProviderAfterPersist(ctx, cacheKey, freshProvider.ID)
			if readErr != nil || durableProvider == nil {
				if readErr == nil {
					readErr = fmt.Errorf("provider not found after Grok OAuth success CAS")
				}
				return nil, api.options.Platform.ContainmentError(fmt.Errorf("grok OAuth success persisted but durable provider state is unavailable: %w", readErr))
			}
			// CAS 更新凭据并返回最新记录，其他管理和调度字段保持数据库当前值。
			freshProvider = durableProvider
		} else if !freshProvider.IsCredentialShadow() {
			writer, ok := api.providerRepo.(CredentialRefreshWriter)
			if !ok {
				return nil, fmt.Errorf("%w: conditional credential writer is not configured", ErrRefreshCredentialPersist)
			}
			applied, updateErr := writer.UpdateOAuthCredentialsIfUnchanged(ctx, CredentialVersion{ID: attemptedProvider.ID, Platform: attemptedProvider.Platform, Type: attemptedProvider.Type, Status: attemptedProvider.Status, ProxyID: attemptedProvider.ProxyID, Credentials: attemptedProvider.Credentials}, newCredentials)
			if updateErr != nil {
				api.options.Error("oauth_refresh_update_failed", "provider_id", freshProvider.ID, "error", updateErr)
				return nil, fmt.Errorf("%w: %v", ErrRefreshCredentialPersist, updateErr)
			}
			if !applied {
				current, readErr := api.providerRepo.GetByID(ctx, freshProvider.ID)
				if readErr != nil {
					return nil, &RefreshStateUnavailableError{err: readErr}
				}
				if current == nil || current.ID != attemptedProvider.ID {
					return nil, fmt.Errorf("%w: provider not found after credential comparison", ErrRefreshProviderStateChanged)
				}
				// 管理更新发生后返回最新状态，本次交换结果被丢弃。
				if requestPath && (!current.IsActive() || !executor.CanRefresh(current)) {
					return nil, fmt.Errorf("%w: provider changed during refresh", ErrRefreshProviderStateChanged)
				}
				return &OAuthRefreshResult{Provider: current}, nil
			}
			freshProvider.Credentials = CloneValues(newCredentials)
		} else {
			api.options.Warn("skip persisting credentials to spark shadow provider", "provider_id", freshProvider.ID, "parent_id", *freshProvider.ParentProviderID)
		}
	}

	if requestPath && freshProvider.Platform == PlatformGrok {
		if eligibilityErr := api.options.Platform.Eligibility(freshProvider); eligibilityErr != nil {
			return nil, api.options.Platform.SnapshotError(eligibilityErr, freshProvider)
		}
	}

	return &OAuthRefreshResult{
		Refreshed:      true,
		NewCredentials: newCredentials,
		Provider:       freshProvider,
	}, nil
}

func (api *OAuthRefreshAPI) releaseRefreshLock(parent context.Context, cacheKey string) {
	cleanupParent := context.Background()
	if parent != nil {
		cleanupParent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(cleanupParent, defaultRefreshLockReleaseTimeout)
	defer cancel()
	if err := api.tokenCache.ReleaseRefreshLock(ctx, cacheKey); err != nil {
		api.options.Warn("oauth_refresh_lock_release_failed", "cache_key", cacheKey, "error", err)
	}
}

func (api *OAuthRefreshAPI) loadGrokDurableProviderAfterPersist(parent context.Context, cacheKey string, providerID int64) (*Record, error) {
	cleanupParent := context.Background()
	if parent != nil {
		cleanupParent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(cleanupParent, defaultRefreshPostPersistCleanupTimeout)
	defer cancel()

	// 成功轮换后，上游可能撤销缓存中的 access token，因此提交时使用独立预算删除缓存。
	if api.tokenCache != nil {
		if err := api.tokenCache.DeleteAccessToken(ctx, cacheKey); err != nil {
			api.options.Warn("oauth_refresh_post_persist_cache_delete_failed",
				"provider_id", providerID,
				"cache_key", cacheKey,
				"error", err,
			)
		}
	}

	return api.providerRepo.GetByID(ctx, providerID)
}

// IsInvalidGrantError 检查错误是否表示 refresh token 已失效或已被消费。
func IsInvalidGrantError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "refresh_token_reused") ||
		strings.Contains(msg, "refresh token has already been used")
}

// tryRecoverFromRefreshRace 在 refresh token 被拒绝后尝试竞争恢复
// 重新读取 DB，如果 refresh_token 已改变（说明另一个 worker 成功刷新），则返回更新后的 provider。
func (api *OAuthRefreshAPI) tryRecoverFromRefreshRace(ctx context.Context, usedProvider *Record) (*Record, bool) {
	if api.providerRepo == nil {
		return nil, false
	}
	reReadProvider, err := api.providerRepo.GetByID(ctx, usedProvider.ID)
	if err != nil || reReadProvider == nil {
		return nil, false
	}
	usedRT := usedProvider.GetCredential("refresh_token")
	currentRT := reReadProvider.GetCredential("refresh_token")
	if usedRT == "" || currentRT == "" {
		return nil, false
	}
	// refresh_token 不同 → 另一个 worker 已成功刷新
	if usedRT != currentRT {
		return reReadProvider, true
	}
	return nil, false
}

// MergeCredentials 将旧 credentials 中不存在于新 map 的字段保留到新 map 中。
func MergeCredentials(oldCreds, newCreds map[string]any) map[string]any {
	if newCreds == nil {
		newCreds = make(map[string]any)
	}
	for k, v := range oldCreds {
		if _, exists := newCreds[k]; !exists {
			newCreds[k] = v
		}
	}
	return newCreds
}

// snapshotRefreshRecord 固定交换前的身份，nil 凭据按空对象保存。
func snapshotRefreshRecord(value *Record) *Record {
	copy := CloneRecord(value)
	if copy != nil && copy.Credentials == nil {
		copy.Credentials = map[string]any{}
	}
	return copy
}
