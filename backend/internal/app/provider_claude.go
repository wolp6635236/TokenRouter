package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	providerauth "github.com/TokenFlux/TokenRouter/internal/provider"
	providerhttp "github.com/TokenFlux/TokenRouter/internal/provider/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

// provideClaudeAuthorizationHTTP 为 Claude 授权 HTTP 入口绑定授权用例。
func provideClaudeAuthorizationHTTP(source *providerauth.ClaudeAuthorization) *providerhttp.ClaudeOAuthHandler {
	return providerhttp.NewClaudeOAuthHandler(source)
}

func provideClaudeAuthorization(proxies egress.ProxyRepository, client providerauth.ClaudeOAuthClient) *providerauth.ClaudeAuthorization {
	options := provideradapter.ClaudeAuthorizationOptions(func(ctx context.Context, id int64) (string, bool) {
		proxy, err := proxies.GetByID(ctx, id)
		if err != nil || proxy == nil {
			return "", false
		}
		return proxy.URL(), true
	})
	return providerauth.NewClaudeAuthorization(client, options)
}

// provideClaudeTokens 绑定 OAuth 与 Vertex 凭据源，各来源按调用时机使用缓存。
func provideClaudeTokens(store *postgres.ProviderStore, cache providerauth.AccessTokenCache, authorization *providerauth.ClaudeAuthorization, refresh *providerauth.OAuthRefreshAPI) *providerauth.ClaudeTokenSource {
	executor := &providerauth.ClaudeTokenRefresher{Authorization: authorization}
	return &providerauth.ClaudeTokenSource{Options: providerauth.ClaudeTokenOptions{
		Cache: cache, Repository: store, Policy: providerauth.ClaudeProviderRefreshPolicy(),
		Debug: slog.Debug, Warn: slog.Warn,
		Vertex: func(ctx context.Context, value *providerauth.Record) (string, error) {
			return provideradapter.VertexServiceAccountAccessToken(ctx, cache, value)
		},
		Refresh: func(ctx context.Context, value *providerauth.Record, window time.Duration) (*providerauth.OAuthRefreshResult, error) {
			return refresh.RefreshIfNeeded(ctx, value, executor, window)
		},
	}}
}

// provideMessageCredentials 复用 Claude 和 Vertex 凭据源，其他平台读取已保存的凭据。
func provideMessageCredentials(claude *providerauth.ClaudeTokenSource) *providerauth.MessageCredentialSource {
	result := &providerauth.MessageCredentialSource{}
	if claude != nil {
		result.Claude = claude.GetAccessToken
	}
	return result
}
