package lifecycle

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServeReturnsListenerFailure 检查监听地址被占用时返回错误。
func TestServeReturnsListenerFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	require.Error(t, Serve(context.Background(), &http.Server{Addr: ln.Addr().String()}))
}
