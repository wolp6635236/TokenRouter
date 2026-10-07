package provider

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

func newClaudeAuthorizationForTest(proxies *mockProxyRepoForOAuth, client provider.ClaudeOAuthClient) *provider.ClaudeAuthorization {
	options := ClaudeAuthorizationOptions(func(ctx context.Context, id int64) (string, bool) {
		proxy, err := proxies.GetByID(ctx, id)
		if err != nil || proxy == nil {
			return "", false
		}
		return proxy.URL(), true
	})
	return provider.NewClaudeAuthorization(client, options)
}

func stopClaudeAuthorization(t *testing.T, value *provider.ClaudeAuthorization) {
	t.Helper()
	if err := value.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
