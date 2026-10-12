package provider

import (
	"context"
	"strconv"
	"time"
)

// ClaudeTokenRefresher 为统一刷新协调器提供 Claude 凭据资格检查和合并操作。
type ClaudeTokenRefresher struct {
	Authorization *ClaudeAuthorization
}

func (r *ClaudeTokenRefresher) CanRefresh(value *Record) bool {
	return CanRefreshClaude(value)
}

func (r *ClaudeTokenRefresher) NeedsRefresh(value *Record, window time.Duration) bool {
	return NeedsRefreshClaude(value, window)
}

func (r *ClaudeTokenRefresher) CacheKey(value *Record) string {
	return ClaudeTokenCacheKey(value)
}

func (r *ClaudeTokenRefresher) Refresh(ctx context.Context, value *Record) (map[string]any, error) {
	return RefreshClaudeCredentials(ctx, value, r.Authorization.RefreshProviderToken)
}

// CanRefreshClaude 检查是否能处理此提供商
// 处理 anthropic 平台的 oauth 与 setup-token 类型提供商。
// 两者的 access_token 均为短期令牌（expires_in=28800，即 8h），到期都需刷新；
// setup-token 之前被排除会导致其 access_token 过期后请求 401。
// 此处与手动刷新入口（provider.IsOAuth()）保持一致，实际是否刷新由 NeedsRefresh
// 按 expires_at 判断刷新资格，在分布式锁内执行交换。
func CanRefreshClaude(provider *Record) bool {
	return provider.Platform == PlatformAnthropic && provider.IsOAuth()
}

// NeedsRefreshClaude 检查 token 是否需要刷新
// 基于 expires_at 字段判断是否在刷新窗口内。
func NeedsRefreshClaude(provider *Record, refreshWindow time.Duration) bool {
	expiresAt := provider.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return false
	}
	return time.Until(*expiresAt) < refreshWindow
}

// RefreshClaudeCredentials Refresh 执行 token 刷新
// 将 token 字段合并进 credentials，其余字段保持当前值。
func RefreshClaudeCredentials(ctx context.Context, provider *Record, exchange func(context.Context, *Record) (*ClaudeTokenInfo, error)) (map[string]any, error) {
	tokenInfo, err := exchange(ctx, provider)
	if err != nil {
		return nil, err
	}

	newCredentials := BuildClaudeProviderCredentials(tokenInfo)
	newCredentials = MergeCredentials(provider.Credentials, newCredentials)

	return newCredentials, nil
}

// BuildClaudeProviderCredentials 为 Claude 平台构建 OAuth credentials map
// 消除 Claude 平台没有 BuildProviderCredentials 方法的问题。
func BuildClaudeProviderCredentials(tokenInfo *ClaudeTokenInfo) map[string]any {
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"token_type":   tokenInfo.TokenType,
		"expires_in":   strconv.FormatInt(tokenInfo.ExpiresIn, 10),
		"expires_at":   strconv.FormatInt(tokenInfo.ExpiresAt, 10),
	}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.Scope != "" {
		creds["scope"] = tokenInfo.Scope
	}
	return creds
}
