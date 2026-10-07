package httpapi

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/stretchr/testify/require"
)

// newGrokAuthorizationForTest 为 HTTP 测试组合授权和响应处理，使用应用参数。
func newGrokAuthorizationForTest(proxies egress.ProxyRepository, client provider.GrokAuthorizationClient, enabled ...bool) *provider.GrokAuthorization {
	return provider.NewGrokAuthorization(client, provideradapter.GrokAuthorizationOptions(proxies, func() bool {
		return len(enabled) > 0 && enabled[0]
	}))
}

func stopGrokAuthorizationForTest(t *testing.T, authorization *provider.GrokAuthorization) {
	t.Helper()
	require.NoError(t, authorization.StopContext(context.Background()))
}
