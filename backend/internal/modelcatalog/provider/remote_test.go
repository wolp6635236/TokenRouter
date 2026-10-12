package provider

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	canListenOnce sync.Once
	canListen     bool
	canListenErr  error
)

// TestFetchCatalogHTTP 检查 HTTP 客户端的条件请求、状态码和下载上限。
func TestFetchCatalogHTTP(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		unchanged bool
		wantError bool
	}{
		{"success", http.StatusOK, `{"providers":{}}`, false, false},
		{"unchanged", http.StatusNotModified, "", true, false},
		{"server error", http.StatusInternalServerError, "", false, true},
		{"too large", http.StatusOK, strings.Repeat("x", 32*1024*1024+1), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, `"v1"`, r.Header.Get("If-None-Match"))
				w.Header().Set("ETag", `"v2"`)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := &remoteClient{httpClient: server.Client()}
			body, etag, unchanged, err := client.FetchCatalog(context.Background(), server.URL, `"v1"`)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.unchanged, unchanged)
			if unchanged {
				require.Empty(t, body)
				require.Equal(t, `"v1"`, etag)
			} else {
				require.Equal(t, tc.body, string(body))
				require.Equal(t, `"v2"`, etag)
			}
		})
	}
}

// TestFetchCatalogCancellation 检查取消信号传递给 HTTP 请求及非法地址处理。
func TestFetchCatalogCancellation(t *testing.T) {
	started := make(chan struct{})
	server := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &remoteClient{httpClient: server.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, _, err := client.FetchCatalog(ctx, server.URL, "")
		done <- err
	}()
	<-started
	cancel()
	require.Error(t, <-done)
	_, _, _, err := client.FetchCatalog(context.Background(), "://invalid-url", "")
	require.Error(t, err)
}

// TestNewRemoteClient_InvalidProxy_NoFallback 验证代理错误不会触发未授权直连。
func TestNewRemoteClient_InvalidProxy_NoFallback(t *testing.T) {
	client := NewRemoteClient("://bad", false)
	require.IsType(t, &remoteClientError{}, client)
	_, _, _, err := client.FetchCatalog(context.Background(), "https://example.com", "")
	require.ErrorContains(t, err, "proxy client init failed")
}

// TestNewRemoteClient_InvalidProxy_WithFallback 检查允许直连回退时的代理错误处理。
func TestNewRemoteClient_InvalidProxy_WithFallback(t *testing.T) {
	require.IsType(t, &remoteClient{}, NewRemoteClient("://bad", true))
}

func localListenerAvailable() bool {
	canListenOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			canListenErr = err
			canListen = false
			return
		}
		_ = ln.Close()
		canListen = true
	})
	return canListen
}

func newLocalTestServer(tb testing.TB, handler http.Handler) *httptest.Server {
	tb.Helper()
	if !localListenerAvailable() {
		tb.Skipf("local listeners are not permitted in this environment: %v", canListenErr)
	}
	return httptest.NewServer(handler)
}
