//go:build integration

package provider_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider/rediscache"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/testutil/rediscontainer"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

type antigravityFixtureTransport struct {
	target    *url.URL
	transport http.RoundTripper
}

func (f antigravityFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	target := *request.URL
	target.Scheme = f.target.Scheme
	target.Host = f.target.Host
	copy.URL = &target
	copy.Host = f.target.Host
	return f.transport.RoundTrip(copy)
}

func TestAntigravityNativeRefreshUsesOriginalCAS(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("administrator=%v", changed), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			client := testEntClient(t)
			row, err := client.Provider.Create().SetName(fmt.Sprintf("test-antigravity-%d", time.Now().UnixNano())).SetPlatform(capability.PlatformAntigravity).SetType(capability.ProviderTypeOAuth).SetCredentials(map[string]any{"access_token": "expired", "refresh_token": "original", "expires_at": time.Now().Add(-time.Hour).Unix(), "project_id": "fixture-project", "email": "fixture@example.invalid"}).Save(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Provider.DeleteOneID(row.ID).Exec(context.Background())) })
			repo := newProviderStoreContract(client, integrationDB, nil)
			value, err := repo.GetByID(ctx, row.ID)
			require.NoError(t, err)
			entered, resume := make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					require.NoError(t, r.ParseForm())
					require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
					require.Equal(t, "original", r.Form.Get("refresh_token"))
					if calls.Add(1) == 1 {
						close(entered)
					}
					select {
					case <-resume:
					case <-r.Context().Done():
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"access_token":"refreshed","refresh_token":"rotated","token_type":"Bearer","expires_in":3600}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, ":loadCodeAssist") {
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"cloudaicompanionProject":"fixture-project","paidTier":{"id":"g1-pro-tier"}}`)
					return
				}
				t.Errorf("非夹具请求 %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			defer resumeOnce.Do(func() { close(resume) })
			localURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			localClient := &http.Client{Transport: antigravityFixtureTransport{target: localURL, transport: server.Client().Transport}, Timeout: 10 * time.Second}
			oauth := providercore.NewAntigravityAuthorization(provideradapter.AntigravityAuthorizationOptions(nil))
			defer func() { require.NoError(t, oauth.StopContext(context.Background())) }()
			oauth.Options.NewClient = func(proxy string) (providercore.AntigravityAuthorizationClient, error) {
				return antigravity.NewClientWithOptions(proxy, antigravity.ClientOptions{HTTPClient: localClient})
			}
			cache := rediscache.NewOAuthTokenCache(rediscontainer.New(t))
			store := repo
			refresh := providercore.NewOAuthRefreshAPI(store, cache, providercore.RefreshOptions{
				Now: time.Now, Warn: slog.Warn, Info: slog.Info, Error: slog.Error,
				Platform: providercore.ProviderRefreshPlatformPolicy(),
			})
			t.Cleanup(func() { require.NoError(t, refresh.StopContext(context.Background())) })
			executor := &providercore.AntigravityRefreshRules{
				RefreshProviderToken: oauth.RefreshProviderToken, BuildProviderCredentials: oauth.BuildProviderCredentials,
				Printf: func(format string, args ...any) { _, _ = fmt.Printf(format, args...) }, Logf: log.Printf,
			}
			tokenSource := &providercore.AntigravityTokenSource{Options: providercore.AntigravityTokenOptions{
				Repository: store, Cache: cache, Policy: providercore.AntigravityProviderRefreshPolicy(),
				Debug: slog.Debug, Warn: slog.Warn,
				Refresh: func(ctx context.Context, record *providercore.Record, window time.Duration) (*providercore.OAuthRefreshResult, error) {
					return refresh.RefreshIfNeeded(ctx, record, executor, window)
				},
			}}
			type result struct {
				token string
				err   error
			}
			done := make(chan result, 1)
			go func() {
				token, err := tokenSource.GetAccessToken(ctx, providercore.CloneRecord(value))
				done <- result{token, err}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("原生交换未进入")
			}
			expected := "refreshed"
			if changed {
				expected = "administrator"
				require.NoError(t, repo.UpdateCredentials(ctx, row.ID, map[string]any{"access_token": expected, "refresh_token": "administrator-refresh", "expires_at": time.Now().Add(time.Hour).Unix(), "project_id": "fixture-project", "email": "fixture@example.invalid"}))
			}
			resumeOnce.Do(func() { close(resume) })
			select {
			case got := <-done:
				require.NoError(t, got.err)
				require.Equal(t, expected, got.token)
			case <-ctx.Done():
				t.Fatal("刷新未退出")
			}
			require.EqualValues(t, 1, calls.Load())
			persisted, err := repo.GetByID(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, expected, persisted.GetCredential("access_token"))
			cached, err := cache.GetAccessToken(ctx, providercore.AntigravityTokenCacheKey(value))
			require.NoError(t, err)
			require.Equal(t, expected, cached)
			require.NoError(t, cache.DeleteAccessToken(ctx, providercore.AntigravityTokenCacheKey(value)))
		})
	}
}
