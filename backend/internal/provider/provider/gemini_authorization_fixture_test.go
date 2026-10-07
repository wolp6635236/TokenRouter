package provider

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini/codeassist"
)

func newGeminiAuthorizationForTest(proxies *mockGeminiProxyRepo, client provider.GeminiOAuthClient, discovery provider.GeminiCodeAssistClient, drive provider.GeminiDriveClient, cfg *codeassist.OAuthConfig) *provider.GeminiAuthorization {
	var resolve func(context.Context, int64) (string, bool)
	if proxies != nil {
		resolve = func(ctx context.Context, id int64) (string, bool) {
			proxy, err := proxies.GetByID(ctx, id)
			if err != nil || proxy == nil {
				return "", false
			}
			return proxy.URL(), true
		}
	}
	return provider.NewGeminiAuthorization(client, discovery, drive, GeminiAuthorizationOptions(func() codeassist.OAuthConfig { return *cfg }, resolve))
}

func stopGeminiAuthorization(t *testing.T, value *provider.GeminiAuthorization) {
	t.Helper()
	if err := value.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
