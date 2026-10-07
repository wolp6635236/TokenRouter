package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type privacyProxyReader struct{ egress.ProxyRepository }

func (privacyProxyReader) GetByID(context.Context, int64) (*egress.Proxy, error) {
	return nil, errors.New("forced proxy lookup failure")
}

type privacyProviderWriter struct {
	writes atomic.Int32
}

func (r *privacyProviderWriter) UpdatePrivacyModeIfUnchanged(context.Context, provider.UsageObservationVersion, string) (bool, error) {
	r.writes.Add(1)
	return true, nil
}

// TestPrivacyProxyLookupFailureDoesNotConnectDirectly 通过本地 HTTP 检查代理读取失败后请求结束，成功状态保持未写入。
func TestPrivacyProxyLookupFailureDoesNotConnectDirectly(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "ensure", true: "force"}[force], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			writer := &privacyProviderWriter{}
			svc := provider.NewPrivacyService(writer, privacyProxyReader{}, PrivacyOptions(func(string) (*req.Client, error) { return req.C().SetTimeout(time.Second), nil }, openai.PrivacyEndpoints{Settings: server.URL}))
			id := int64(99)
			value := &provider.Record{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ProxyID: &id, Credentials: map[string]any{"access_token": "test-token"}}
			if force {
				svc.ForceOpenAIPrivacy(context.Background(), value)
			} else {
				svc.EnsureOpenAIPrivacy(context.Background(), value)
			}
			require.Zero(t, calls.Load(), "代理回源失败不得连接平台")
			require.Zero(t, writer.writes.Load())
		})
	}
}

// TestRefreshPrivacyProxyLookupFailureDoesNotConnectDirectly 验证后台刷新同样不能因代理仓储缺失或回源失败而退回直连。
func TestRefreshPrivacyProxyLookupFailureDoesNotConnectDirectly(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "lookup_failure", true: "missing_reader"}[missing], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			writer := &privacyProviderWriter{}
			var proxies egress.ProxyRepository = privacyProxyReader{}
			if missing {
				proxies = nil
			}
			svc := provider.NewPrivacyService(writer, proxies, PrivacyOptions(func(string) (*req.Client, error) { return req.C().SetTimeout(time.Second), nil }, openai.PrivacyEndpoints{Settings: server.URL}))
			id := int64(99)
			value := &provider.Record{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ProxyID: &id, Credentials: map[string]any{"access_token": "test-token"}}
			svc.RefreshOpenAIPrivacy(context.Background(), value)
			require.Zero(t, calls.Load(), "后台刷新不得绕过显式代理")
			require.Zero(t, writer.writes.Load())
		})
	}
}
