package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/identity"
)

// wechat_oauth_test.go checks public callback address reconstruction and explicit
// redirect precedence. Handler fixtures supply channel credentials without contacting
// WeChat, while request contexts exercise direct and proxy-reconstructed public
// origins independently of frontend site or model-client endpoint configuration.

// TestResolveWeChatOAuthAbsoluteURL checks direct and forwarded public request
// origins for login and payment callbacks. Missing requests, hosts, or callback
// paths cannot produce a usable absolute callback address.
func TestResolveWeChatOAuthAbsoluteURL(t *testing.T) {
	for _, callback := range []string{"/api/v1/auth/oauth/wechat/callback", "/api/v1/auth/oauth/wechat/payment/callback"} {
		for _, tc := range []struct {
			name           string // named request scenario
			request        string // incoming request URL
			forwardedHost  string // public proxy host
			forwardedProto string // public proxy scheme
			wantBase       string // expected callback origin
		}{
			{name: "HTTPS", request: "https://api.example.com/start", wantBase: "https://api.example.com"},
			{name: "HTTP development", request: "http://localhost:8080/start", wantBase: "http://localhost:8080"},
			{name: "reverse proxy", request: "http://backend.internal/start", forwardedHost: "api.example.com", forwardedProto: "https", wantBase: "https://api.example.com"},
		} {
			t.Run(tc.name+callback, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodGet, tc.request, nil)
				c.Request.Header.Set("X-Forwarded-Host", tc.forwardedHost)
				c.Request.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
				require.Equal(t, tc.wantBase+callback, ResolveWeChatOAuthAbsoluteURL(c, callback))
			})
		}
	}
	require.Empty(t, ResolveWeChatOAuthAbsoluteURL(nil, "/callback"))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Empty(t, ResolveWeChatOAuthAbsoluteURL(c, "/callback"))
	c.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/start", nil)
	require.Empty(t, ResolveWeChatOAuthAbsoluteURL(c, " "))
	c.Request.Host = ""
	require.Empty(t, ResolveWeChatOAuthAbsoluteURL(c, "/callback"))
}

// TestWeChatGetConfigPreservesExplicitCallback supplies an optional explicit
// redirect alongside the incoming request. Explicit login callbacks remain authoritative;
// otherwise the handler builds its callback on the request origin.
func TestWeChatGetConfigPreservesExplicitCallback(t *testing.T) {
	for _, explicit := range []string{"", " https://custom.example/wechat-return "} {
		t.Run(explicit, func(t *testing.T) {
			handler := NewWeChatHandler(nil, nil, nil, WeChatHTTPOptions{
				LoadConfig: func(context.Context, string) (identity.WeChatOAuthOptions, error) {
					return identity.WeChatOAuthOptions{AppID: "app", AppSecret: "secret", OpenEnabled: true, RedirectURI: explicit}, nil
				},
			})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "https://request.example/start", nil)
			cfg, err := handler.GetConfig(t.Context(), "open", c)
			require.NoError(t, err)
			want := "https://request.example/api/v1/auth/oauth/wechat/callback"
			if explicit != "" {
				want = "https://custom.example/wechat-return"
			}
			require.Equal(t, want, cfg.RedirectURI)
		})
	}
}
