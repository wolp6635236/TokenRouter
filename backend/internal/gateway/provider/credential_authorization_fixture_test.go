package provider_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/stretchr/testify/require"
)

// newGrokAuthorizationForTest 为凭据测试构造 Grok 授权器和供应商替身。
func newGrokAuthorizationForTest(proxies egress.ProxyRepository, client provider.GrokAuthorizationClient) *provider.GrokAuthorization {
	return provider.NewGrokAuthorization(client, provideradapter.GrokAuthorizationOptions(proxies, nil))
}

func stopGrokAuthorizationForTest(t *testing.T, authorization *provider.GrokAuthorization) {
	t.Helper()
	require.NoError(t, authorization.StopContext(context.Background()))
}
