package provider

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	antigravityProjectIDFallbackCredentialKey = "antigravity_project_id"

	antigravityTokenRefreshSkew = 3 * time.Minute
	antigravityTokenCacheSkew   = 5 * time.Minute
	antigravityBackfillCooldown = 5 * time.Minute
	// antigravityRequestRefreshTimeout 请求路径上 token 刷新的最大等待时间。
	// 超过此时间直接放弃刷新、标记提供商临时不可调度并触发 failover，
	// 让后台 TokenRefreshService 在下个周期继续重试。
	antigravityRequestRefreshTimeout = 8 * time.Second
)

type AntigravityTokenState struct{ backfillCooldown sync.Map }

// AntigravityTokenSource 独占原 project 回填冷却，消费者共享同一实例。
type AntigravityTokenSource struct {
	state   AntigravityTokenState
	Options AntigravityTokenOptions
}

type AntigravityTokenOptions struct {
	Cache                AccessTokenCache
	Repository           RefreshRepository
	Policy               ProviderRefreshPolicy
	Refresh              func(context.Context, *Record, time.Duration) (*OAuthRefreshResult, error)
	FillProject          func(context.Context, *Record, string) (string, error)
	Persist              func(context.Context, *Record, map[string]any) error
	SetTempUnschedulable func(context.Context, int64, time.Time, string) error
	TempUnschedCache     TempUnschedCache
	Warn, Debug          func(string, ...any)
}

func ResolveAntigravityProjectID(provider *Record, missing error) (string, error) {
	if provider == nil {
		return "", missing
	}
	if projectID := strings.TrimSpace(provider.GetCredential("project_id")); projectID != "" {
		return projectID, nil
	}
	if projectID := strings.TrimSpace(provider.GetCredential(antigravityProjectIDFallbackCredentialKey)); projectID != "" {
		return projectID, nil
	}
	if projectID := strings.TrimSpace(provider.GetExtraString(antigravityProjectIDFallbackCredentialKey)); projectID != "" {
		return projectID, nil
	}
	return "", missing
}

func (s *AntigravityTokenSource) GetAccessToken(ctx context.Context, value *Record) (string, error) {
	return s.state.GetAccessToken(ctx, value, s.Options)
}

// GetAccessToken returns a valid access_token.
// @project-doc docs/interfaces/antigravity_upstream.md#antigravity_account_contract
func (p *AntigravityTokenState) GetAccessToken(ctx context.Context, provider *Record, options AntigravityTokenOptions) (string, error) {
	if provider == nil {
		return "", errors.New("provider is nil")
	}
	if provider.Platform != PlatformAntigravity {
		return "", errors.New("not an antigravity provider")
	}

	if provider.Type != ProviderTypeOAuth {
		return "", errors.New("not an antigravity oauth provider")
	}

	cacheKey := AntigravityTokenCacheKey(provider)

	// 1) Try cache first.
	if options.Cache != nil {
		if token, err := options.Cache.GetAccessToken(ctx, cacheKey); err == nil && strings.TrimSpace(token) != "" {
			return token, nil
		}
	}

	// 2) Refresh if needed (pre-expiry skew).
	expiresAt := provider.GetCredentialAsTime("expires_at")
	needsRefresh := expiresAt == nil || time.Until(*expiresAt) <= antigravityTokenRefreshSkew
	if needsRefresh && options.Refresh != nil {
		// 请求路径使用短超时，代理连接失败时尽快返回，后台刷新会继续重试。
		refreshCtx, cancel := context.WithTimeout(ctx, antigravityRequestRefreshTimeout)
		defer cancel()
		result, err := options.Refresh(refreshCtx, provider, antigravityTokenRefreshSkew)
		if err != nil {
			// 标记提供商临时不可调度，后续请求会跳过它。
			p.markTempUnschedulable(provider, err, options)
			if options.Policy.OnRefreshError == ProviderRefreshErrorReturn {
				return "", err
			}
		} else if result.LockHeld {
			if options.Policy.OnLockHeld == ProviderLockHeldWaitForCache && options.Cache != nil {
				if token, cacheErr := options.Cache.GetAccessToken(ctx, cacheKey); cacheErr == nil && strings.TrimSpace(token) != "" {
					return token, nil
				}
			}
			// default policy: continue with existing token.
		} else {
			provider = result.Provider
			expiresAt = provider.GetCredentialAsTime("expires_at")
		}
	} else if needsRefresh && options.Cache != nil {
		// Backward-compatible test path when refreshAPI is not injected.
		locked, err := options.Cache.AcquireRefreshLock(ctx, cacheKey, 30*time.Second)
		if err == nil && locked {
			defer func() { _ = options.Cache.ReleaseRefreshLock(ctx, cacheKey) }()
		}
	}

	accessToken := provider.GetCredential("access_token")
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("access_token not found in credentials")
	}

	// Backfill project_id online when missing, with cooldown to avoid hammering.
	if strings.TrimSpace(provider.GetCredential("project_id")) == "" && options.FillProject != nil {
		if p.shouldAttemptBackfill(provider.ID) {
			p.markBackfillAttempted(provider.ID)
			if projectID, err := options.FillProject(ctx, provider, accessToken); err == nil && projectID != "" {
				provider.Credentials["project_id"] = projectID
				if updateErr := options.Persist(ctx, provider, provider.Credentials); updateErr != nil {
					options.Warn("antigravity_project_id_backfill_persist_failed",
						"provider_id", provider.ID,
						"error", updateErr,
					)
				}
			}
		}
	}

	// 3) Populate cache with TTL.
	if options.Cache != nil {
		latestProvider, isStale := CheckTokenVersion(ctx, provider, options.Repository, options.Debug)
		if isStale && latestProvider != nil {
			options.Debug("antigravity_token_version_stale_use_latest", "provider_id", provider.ID)
			accessToken = latestProvider.GetCredential("access_token")
			if strings.TrimSpace(accessToken) == "" {
				return "", errors.New("access_token not found after version check")
			}
		} else {
			ttl := 30 * time.Minute
			if expiresAt != nil {
				until := time.Until(*expiresAt)
				switch {
				case until > antigravityTokenCacheSkew:
					ttl = until - antigravityTokenCacheSkew
				case until > 0:
					ttl = until
				default:
					ttl = time.Minute
				}
			}
			_ = options.Cache.SetAccessToken(ctx, cacheKey, accessToken, ttl)
		}
	}

	return accessToken, nil
}

// shouldAttemptBackfill checks backfill cooldown.
func (p *AntigravityTokenState) shouldAttemptBackfill(providerID int64) bool {
	if v, ok := p.backfillCooldown.Load(providerID); ok {
		if lastAttempt, ok := v.(time.Time); ok {
			return time.Since(lastAttempt) > antigravityBackfillCooldown
		}
	}
	return true
}

// markTempUnschedulable 在请求路径上 token 刷新失败时标记提供商临时不可调度。
// 同时写入数据库和 Redis，调度器据此跳过该提供商。
// 使用 background context 因为请求 context 可能已超时。
func (p *AntigravityTokenState) markTempUnschedulable(provider *Record, refreshErr error, options AntigravityTokenOptions) {
	if options.SetTempUnschedulable == nil || provider == nil {
		return
	}
	now := time.Now()
	until := now.Add(TokenRefreshTempUnschedDuration)
	reason := "token refresh failed on request path: " + refreshErr.Error()
	bgCtx := context.Background()
	if err := options.SetTempUnschedulable(bgCtx, provider.ID, until, reason); err != nil {
		options.Warn("antigravity_token_provider.set_temp_unschedulable_failed",
			"provider_id", provider.ID,
			"error", err,
		)
		return
	}
	options.Warn("antigravity_token_provider.temp_unschedulable_set",
		"provider_id", provider.ID,
		"until", until.Format(time.RFC3339),
		"reason", reason,
	)
	// 同步写 Redis 缓存，调度器立即生效
	if options.TempUnschedCache != nil {
		state := &TempUnschedState{
			UntilUnix:       until.Unix(),
			TriggeredAtUnix: now.Unix(),
			ErrorMessage:    reason,
		}
		if err := options.TempUnschedCache.SetTempUnsched(bgCtx, provider.ID, state); err != nil {
			options.Warn("antigravity_token_provider.temp_unsched_cache_set_failed",
				"provider_id", provider.ID,
				"error", err,
			)
		}
	}
}

func (p *AntigravityTokenState) markBackfillAttempted(providerID int64) {
	p.backfillCooldown.Store(providerID, time.Now())
}

func AntigravityTokenCacheKey(provider *Record) string {
	projectID := strings.TrimSpace(provider.GetCredential("project_id"))
	if projectID != "" {
		return "ag:" + projectID
	}
	return "ag:provider:" + strconv.FormatInt(provider.ID, 10)
}
