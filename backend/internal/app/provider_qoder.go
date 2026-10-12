package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	egressprovider "github.com/TokenFlux/TokenRouter/internal/egress/provider"
	providerauth "github.com/TokenFlux/TokenRouter/internal/provider"
	providerhttp "github.com/TokenFlux/TokenRouter/internal/provider/httpapi"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
)

func provideQoderAuthorizationHTTP(source *provideradapter.QoderAuthorization) *providerhttp.QoderOAuthHandler {
	return providerhttp.NewQoderOAuthHandler(source)
}

// provideQoderAuthorization 绑定代理读取接口，授权组件和供应商执行器分别管理会话与请求。
func provideQoderAuthorization(proxies egress.ProxyRepository) *provideradapter.QoderAuthorization {
	return provideradapter.NewQoderAuthorization(func(ctx context.Context, id *int64) (string, error) {
		if id == nil {
			return "", nil
		}
		if proxies == nil {
			return "", errors.New("proxy repository is not configured")
		}
		proxy, err := proxies.GetByID(ctx, *id)
		if err != nil {
			return "", fmt.Errorf("get proxy: %w", err)
		}
		if proxy == nil {
			return "", errors.New("proxy not found")
		}
		return proxy.URL(), nil
	}, nil)
}

// provideQoderTokens 构造唯一提供商会话缓存，站点交换由供应商构建器负责。
func provideQoderTokens(transport provideradapter.QoderTransport, profiles *egressprovider.TLSProfiles) *provideradapter.QoderTokenProvider {
	source := provideradapter.NewQoderTokenProvider(qoder.SessionBuilder{})
	source.SetHTTPUpstream(transport, profiles)
	return source
}

// provideTokenCacheInvalidator 为失效操作绑定共享的会话和 token 缓存。
func provideTokenCacheInvalidator(cache providerauth.AccessTokenCache, sessions *provideradapter.QoderTokenProvider) providerauth.TokenCacheInvalidator {
	return providerauth.NewCompositeTokenCacheInvalidator(cache, sessions, slog.Warn)
}
