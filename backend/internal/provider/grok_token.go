package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	GrokTokenCacheSkew          = 5 * time.Minute
	GrokRequestRefreshTimeout   = 8 * time.Second
	GrokRefreshLockWaitTimeout  = 2 * time.Second
	GrokRefreshLockPollInterval = 25 * time.Millisecond
)

var (
	ErrGrokOAuthRefreshNotConfigured = errors.New("grok oauth refresh is not configured")
	ErrGrokOAuthRefreshTokenMissing  = errors.New("grok oauth refresh token is missing")
	ErrGrokOAuthAccessTokenMissing   = errors.New("grok oauth access token is missing")
	ErrGrokOAuthAccessTokenExpired   = errors.New("grok oauth access token is expired")
	ErrGrokOAuthConfiguredProxyMiss  = errors.New("grok oauth configured proxy is missing")
)

type GrokTokenSource struct {
	Cache      AccessTokenCache
	Repository RefreshRepository
	Policy     ProviderRefreshPolicy
	Refresh    func(context.Context, *Record, time.Duration) (*OAuthRefreshResult, error)
}

// GrokStoredAccessToken 按提供商类型读取已存储的访问令牌。
func GrokStoredAccessToken(value *Record) (string, error) {
	switch value.Type {
	case capability.ProviderTypeOAuth:
		token := value.GetGrokAccessToken()
		if token == "" {
			return "", errors.New("grok access_token not found in credentials")
		}
		return token, nil
	case capability.ProviderTypeSetupToken:
		token := value.GetCredential("access_token")
		if token == "" {
			return "", errors.New("access_token not found in credentials")
		}
		return token, nil
	case capability.ProviderTypeAPIKey:
		token := value.GetCredential("api_key")
		if token == "" {
			return "", errors.New("api_key not found in credentials")
		}
		return token, nil
	case capability.ProviderTypeBedrock:
		return "", nil
	case capability.ProviderTypeServiceAccount:
		return "", fmt.Errorf("unsupported service account platform: %s", value.Platform)
	default:
		return "", fmt.Errorf("unsupported provider type: %s", value.Type)
	}
}

// @project-doc docs/interfaces/grok_upstream.md#grok_account_contract
func (p *GrokTokenSource) GetAccessToken(ctx context.Context, provider *Record) (string, error) {
	if provider == nil {
		return "", errors.New("provider is nil")
	}
	if provider.Platform != PlatformGrok || provider.Type != ProviderTypeOAuth {
		return "", errors.New("not a grok oauth provider")
	}
	selectedProxyID := CloneGrokProxyID(provider.ProxyID)
	if eligibilityErr := GrokOAuthRequestProviderEligibilityError(provider); eligibilityErr != nil {
		return "", WithGrokCredentialFailureSnapshot(eligibilityErr, provider)
	}

	expiresAt := provider.GetCredentialAsTime("expires_at")
	providerAccessToken := strings.TrimSpace(provider.GetGrokAccessToken())
	if providerAccessToken == "" {
		return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthAccessTokenMissing, provider)
	}
	if strings.TrimSpace(provider.GetGrokRefreshToken()) == "" {
		return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthRefreshTokenMissing, provider)
	}
	cacheKey := GrokTokenCacheKey(provider)
	if p.Cache != nil {
		if token, err := p.Cache.GetAccessToken(ctx, cacheKey); err == nil {
			cachedToken := strings.TrimSpace(token)
			if cachedToken != "" && providerAccessToken != "" && cachedToken == providerAccessToken &&
				expiresAt != nil && time.Until(*expiresAt) > GrokTokenRefreshSkew {
				return cachedToken, nil
			}
		}
	}

	needsRefresh := expiresAt == nil || time.Until(*expiresAt) <= GrokTokenRefreshSkew
	if needsRefresh {
		if p.Refresh == nil {
			return "", ErrGrokOAuthRefreshNotConfigured
		}
		refreshCtx, cancel := context.WithTimeout(ctx, GrokRequestRefreshTimeout)
		defer cancel()
		result, err := p.Refresh(WithRefreshRequestPath(refreshCtx), provider, GrokTokenRefreshSkew)
		if err != nil {
			if p.Policy.OnRefreshError == ProviderRefreshErrorReturn {
				return "", err
			}
		} else if result != nil && result.LockHeld {
			if p.Policy.OnLockHeld == ProviderLockHeldWaitForCache {
				token, waitErr := p.WaitForRefreshedToken(refreshCtx, provider, cacheKey)
				return token, WithGrokCredentialFailureSnapshot(waitErr, provider)
			}
			if expiresAt == nil || !time.Now().Before(*expiresAt) {
				return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthAccessTokenExpired, provider)
			}
		} else if result != nil && result.Provider != nil {
			if eligibilityErr := GrokOAuthRequestProviderEligibilityError(result.Provider); eligibilityErr != nil {
				return "", WithGrokCredentialFailureSnapshot(eligibilityErr, result.Provider)
			}
			if !GrokCredentialProxyIDsEqual(result.Provider.ProxyID, selectedProxyID) {
				return "", WithGrokCredentialFailureSnapshot(ErrRefreshProviderStateChanged, result.Provider)
			}
			provider = result.Provider
			expiresAt = provider.GetCredentialAsTime("expires_at")
		}
	}

	accessToken := provider.GetGrokAccessToken()
	if strings.TrimSpace(accessToken) == "" {
		return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthAccessTokenMissing, provider)
	}
	if expiresAt != nil && !time.Now().Before(*expiresAt) {
		return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthAccessTokenExpired, provider)
	}

	if p.Cache != nil {
		latestProvider, isStale := CheckTokenVersion(ctx, provider, p.Repository)
		if isStale && latestProvider != nil {
			if eligibilityErr := GrokOAuthRequestProviderEligibilityError(latestProvider); eligibilityErr != nil {
				return "", WithGrokCredentialFailureSnapshot(eligibilityErr, latestProvider)
			}
			if !GrokCredentialProxyIDsEqual(latestProvider.ProxyID, selectedProxyID) {
				return "", WithGrokCredentialFailureSnapshot(ErrRefreshProviderStateChanged, latestProvider)
			}
			accessToken = latestProvider.GetGrokAccessToken()
			if strings.TrimSpace(accessToken) == "" {
				return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthAccessTokenMissing, latestProvider)
			}
			latestExpiry := latestProvider.GetCredentialAsTime("expires_at")
			if latestExpiry == nil || !time.Now().Before(*latestExpiry) {
				return "", WithGrokCredentialFailureSnapshot(ErrGrokOAuthAccessTokenExpired, latestProvider)
			}
		} else {
			ttl := 30 * time.Minute
			if expiresAt != nil {
				until := time.Until(*expiresAt)
				switch {
				case until > GrokTokenCacheSkew:
					ttl = until - GrokTokenCacheSkew
				case until > 0:
					ttl = until
				default:
					ttl = time.Minute
				}
			}
			_ = p.Cache.SetAccessToken(ctx, cacheKey, accessToken, ttl)
		}
	}

	return accessToken, nil
}

// GetAccessTokenForManualTest 为管理员发起的“测试连接”返回访问令牌。它与
// GetAccessToken 不同，不应用请求路径的调度资格门（手动调度开关、限流、过载或
// 临时冷却），因为手动测试正是用来检查这些状态中的提供商，与 Codex/OpenAI
// 测试不受调度状态影响的行为一致（#4598）。
//
// 凭据完整性检查仍然生效，包括配置代理缺失、共享刷新锁协议和刷新 API 的提供商重读。
// 非 active（禁用或错误）提供商的凭据轮换仍由 RefreshIfNeeded 拦截，其尚未过期的
// 令牌只用于原样探测。
func (p *GrokTokenSource) GetAccessTokenForManualTest(ctx context.Context, provider *Record) (string, error) {
	if provider == nil {
		return "", errors.New("provider is nil")
	}
	if provider.Platform != PlatformGrok || provider.Type != ProviderTypeOAuth {
		return "", errors.New("not a grok oauth provider")
	}
	if provider.ProxyID != nil && provider.Proxy == nil {
		return "", ErrGrokOAuthConfiguredProxyMiss
	}
	if strings.TrimSpace(provider.GetGrokRefreshToken()) == "" {
		return "", ErrGrokOAuthRefreshTokenMissing
	}

	accessToken := strings.TrimSpace(provider.GetGrokAccessToken())
	expiresAt := provider.GetCredentialAsTime("expires_at")
	tokenValid := accessToken != "" && expiresAt != nil && time.Now().Before(*expiresAt)
	if accessToken != "" && expiresAt != nil && time.Until(*expiresAt) > GrokTokenRefreshSkew {
		return accessToken, nil
	}

	if p.Refresh == nil {
		if tokenValid {
			return accessToken, nil
		}
		return "", ErrGrokOAuthRefreshNotConfigured
	}

	// 刻意不标记为请求路径刷新：该路径会在 RefreshIfNeeded 内再次应用调度资格，
	// 而这正是手动测试需要绕过的门禁。
	refreshCtx, cancel := context.WithTimeout(ctx, GrokRequestRefreshTimeout)
	defer cancel()
	result, err := p.Refresh(refreshCtx, provider, GrokTokenRefreshSkew)
	if err != nil {
		if tokenValid {
			return accessToken, nil
		}
		return "", err
	}
	if result != nil && result.LockHeld {
		if tokenValid {
			return accessToken, nil
		}
		return "", errors.New("token refresh is already in progress on another worker; retry in a few seconds")
	}
	if result != nil && result.Provider != nil {
		provider = result.Provider
	}

	accessToken = strings.TrimSpace(provider.GetGrokAccessToken())
	if accessToken == "" {
		return "", ErrGrokOAuthAccessTokenMissing
	}
	if latestExpiry := provider.GetCredentialAsTime("expires_at"); latestExpiry != nil && !time.Now().Before(*latestExpiry) {
		return "", ErrGrokOAuthAccessTokenExpired
	}
	return accessToken, nil
}

func (p *GrokTokenSource) WaitForRefreshedToken(ctx context.Context, provider *Record, cacheKey string) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, GrokRefreshLockWaitTimeout)
	defer cancel()

	initialToken := strings.TrimSpace(provider.GetGrokAccessToken())
	initialVersion := provider.GetCredentialAsInt64("_token_version")
	selectedProxyID := CloneGrokProxyID(provider.ProxyID)
	sawAuthoritativeState := false
	var lastProviderReadErr error
	ticker := time.NewTicker(GrokRefreshLockPollInterval)
	defer ticker.Stop()

	for {
		cachedToken := ""
		if p.Cache != nil {
			if token, err := p.Cache.GetAccessToken(waitCtx, cacheKey); err == nil {
				cachedToken = strings.TrimSpace(token)
			}
		}

		if p.Repository != nil {
			latest, err := p.Repository.GetByID(waitCtx, provider.ID)
			if err != nil {
				lastProviderReadErr = err
			} else if latest == nil {
				return "", ErrRefreshProviderStateChanged
			} else {
				sawAuthoritativeState = true
				if eligibilityErr := GrokOAuthRequestProviderEligibilityError(latest); eligibilityErr != nil {
					return "", WithGrokCredentialFailureSnapshot(eligibilityErr, latest)
				}
				if !GrokCredentialProxyIDsEqual(latest.ProxyID, selectedProxyID) {
					return "", WithGrokCredentialFailureSnapshot(ErrRefreshProviderStateChanged, latest)
				}
				token := strings.TrimSpace(latest.GetGrokAccessToken())
				version := latest.GetCredentialAsInt64("_token_version")
				expiresAt := latest.GetCredentialAsTime("expires_at")
				changed := token != initialToken || (version > 0 && version > initialVersion)
				valid := expiresAt != nil && time.Now().Before(*expiresAt)
				if token != "" && changed && valid {
					// 数据库凭据版本较新时，使用数据库 token 并尝试更新缓存。
					if cachedToken != "" && cachedToken != token {
						ttl := time.Until(*expiresAt)
						if ttl > GrokTokenCacheSkew {
							ttl -= GrokTokenCacheSkew
						}
						_ = p.Cache.SetAccessToken(waitCtx, cacheKey, token, ttl)
					}
					return token, nil
				}
			}
		}

		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if !sawAuthoritativeState {
				if lastProviderReadErr == nil {
					lastProviderReadErr = waitCtx.Err()
				}
				return "", fmt.Errorf("%w: %v", ErrRefreshProviderRereadFailed, lastProviderReadErr)
			}
			// 另一个 worker 仍持有刷新权且权威提供商行未变，不隔离旧凭据；
			// 对方可能在本次有界等待结束后立即提交刷新结果。
			return "", ErrRefreshProviderStateChanged
		case <-ticker.C:
		}
	}
}

func GrokOAuthRequestProviderEligibilityError(provider *Record) error {
	if provider == nil || !provider.IsGrokOAuth() || !provider.IsSchedulable() {
		return ErrRefreshProviderStateChanged
	}
	if provider.ProxyID != nil && provider.Proxy == nil {
		return ErrGrokOAuthConfiguredProxyMiss
	}
	return nil
}

func CloneGrokProxyID(proxyID *int64) *int64 {
	if proxyID == nil {
		return nil
	}
	value := *proxyID
	return &value
}

func (p *GrokTokenSource) InvalidateToken(ctx context.Context, provider *Record) error {
	if p == nil || p.Cache == nil || provider == nil {
		return nil
	}
	return p.Cache.DeleteAccessToken(ctx, GrokTokenCacheKey(provider))
}

func GrokTokenCacheKey(provider *Record) string {
	if provider == nil {
		return "grok:provider:0"
	}
	return "grok:provider:" + strconv.FormatInt(provider.ID, 10)
}
