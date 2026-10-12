package provider

import (
	"context"
	"strings"
	"time"
)

type OpenAIRefreshTokenService interface {
	RefreshProviderToken(context.Context, *Record) (*OpenAITokenInfo, error)
}
type OpenAITokenRefresher struct{ Authorization OpenAIRefreshTokenService }

// CacheKey 返回用于分布式锁的缓存键。
func (r *OpenAITokenRefresher) CacheKey(provider *Record) string {
	return OpenAITokenCacheKey(provider)
}

// CanRefresh 检查是否能处理此提供商。
func (r *OpenAITokenRefresher) CanRefresh(provider *Record) bool {
	if provider.IsCredentialShadow() {
		return false
	}
	return provider.Platform == PlatformOpenAI && provider.Type == ProviderTypeOAuth
}

// NeedsRefresh 检查 token 是否需要刷新
// expires_at 缺失且处于限流状态时需要刷新，防止限流期间 token 静默过期。
func (r *OpenAITokenRefresher) NeedsRefresh(provider *Record, refreshWindow time.Duration) bool {
	if provider.IsOpenAIPersonalAccessToken() {
		return false
	}
	if strings.TrimSpace(provider.GetOpenAIRefreshToken()) == "" {
		return false
	}
	expiresAt := provider.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return provider.IsRateLimited()
	}

	return time.Until(*expiresAt) < refreshWindow
}

// Refresh 执行 token 刷新
// 将 token 字段合并进 credentials，其余字段保持当前值。
func (r *OpenAITokenRefresher) Refresh(ctx context.Context, provider *Record) (map[string]any, error) {
	tokenInfo, err := r.Authorization.RefreshProviderToken(ctx, provider)
	if err != nil {
		return nil, err
	}

	// 构建刷新后的凭据，再补齐已有的其他字段。
	newCredentials := BuildOpenAIProviderCredentials(tokenInfo)
	newCredentials = MergeCredentials(provider.Credentials, newCredentials)
	newCredentials = NormalizeOpenAIPersonalAccessTokenCredentials(provider, tokenInfo, newCredentials)

	return newCredentials, nil
}
