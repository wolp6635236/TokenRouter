package provider

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsProxyError_Nil(t *testing.T) {
	require.False(t, isProxyError(nil))
}

func TestIsProxyError_ConnectionRefused(t *testing.T) {
	require.True(t, isProxyError(fmt.Errorf("dial tcp: connection refused")))
}

func TestIsProxyError_Timeout(t *testing.T) {
	require.True(t, isProxyError(fmt.Errorf("i/o timeout while connecting to proxy")))
}

func TestIsProxyError_SOCKS(t *testing.T) {
	require.True(t, isProxyError(fmt.Errorf("socks connect failed")))
}

func TestIsProxyError_TLSHandshake(t *testing.T) {
	require.True(t, isProxyError(fmt.Errorf("tls handshake timeout")))
}

func TestIsProxyError_APIError_NotProxy(t *testing.T) {
	require.False(t, isProxyError(fmt.Errorf("API rate limit exceeded")))
}

func TestNewHTTPClient_NoProxy(t *testing.T) {
	c, err := newHTTPClient("")
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewHTTPClient_InvalidProxy(t *testing.T) {
	_, err := newHTTPClient("://bad-url")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid proxy URL")
}

func TestNewHTTPClient_ValidHTTPProxy(t *testing.T) {
	c, err := newHTTPClient("http://proxy.example.com:8080")
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewHTTPClient_ValidSOCKS5Proxy(t *testing.T) {
	c, err := newHTTPClient("socks5://proxy.example.com:1080")
	require.NoError(t, err)
	require.NotNil(t, c)
}
