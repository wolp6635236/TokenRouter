package provider_test

import (
	"context"
	"log/slog"
	"time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// newGrokTokenSourceForTest 将测试仓库和缓存接入 GrokTokenSource。
func newGrokTokenSourceForTest(repo gatewayprovider.ExecutionProviderStore, cache provider.AccessTokenCache) *provider.GrokTokenSource {
	return &provider.GrokTokenSource{Repository: tokenSourceFixtureRepository(repo), Cache: cache, Policy: provider.GrokProviderRefreshPolicy()}
}

// bindGrokRefreshForTest 将测试刷新器绑定到令牌源。
func bindGrokRefreshForTest(source *provider.GrokTokenSource, refresh *provider.OAuthRefreshAPI, executor provider.OAuthRefreshExecutor) {
	source.Refresh = func(ctx context.Context, record *provider.Record, window time.Duration) (*provider.OAuthRefreshResult, error) {
		return refresh.RefreshIfNeeded(ctx, record, executor, window)
	}
}

// newGrokCredentialRefreshForTest 为测试仓库和缓存构造 OAuth 刷新器。
func newGrokCredentialRefreshForTest(repo gatewayprovider.ExecutionProviderStore, cache provider.AccessTokenCache) *provider.OAuthRefreshAPI {
	return provider.NewOAuthRefreshAPI(tokenSourceFixtureRepository(repo), cache, provider.RefreshOptions{Now: time.Now, Warn: slog.Warn, Info: slog.Info, Error: slog.Error, Platform: provider.ProviderRefreshPlatformPolicy()})
}
