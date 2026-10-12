package provider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
)

type remoteClient struct {
	httpClient *http.Client
}

// remoteClientError 代理初始化失败时的错误占位客户端
// 所有请求直接返回初始化错误，禁止回退到直连。
type remoteClientError struct {
	err error
}

func (c *remoteClientError) FetchCatalog(_ context.Context, _, _ string) ([]byte, string, bool, error) {
	return nil, "", false, c.err
}

// NewRemoteClient 创建统一模型目录的远程客户端
// proxyURL 为空时直连，支持 http/https/socks5/socks5h 协议
// 代理配置失败时行为由 allowDirectOnProxyError 控制：
//   - false（默认）：返回错误占位客户端，禁止回退到直连
//   - true：回退到直连（仅限管理员显式开启）
func NewRemoteClient(proxyURL string, allowDirectOnProxyError bool) RemoteClient {
	// 安全说明：httpclient.GetClient 的错误链（url.Parse / proxyutil）不含明文代理凭据，
	// 此信息通过 slog 写入服务端日志。
	sharedClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  30 * time.Second,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy client init failed, all requests will fail", "service", "pricing", "error", err)
			return &remoteClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		sharedClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &remoteClient{
		httpClient: sharedClient,
	}
}

// FetchCatalog 使用 ETag 节省目录下载，304 不发布新快照。
func (c *remoteClient) FetchCatalog(ctx context.Context, url, etag string) ([]byte, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024+1))
	if len(body) > 32*1024*1024 {
		return nil, "", false, fmt.Errorf("model catalog exceeds 32 MiB")
	}
	return body, resp.Header.Get("ETag"), false, err
}
