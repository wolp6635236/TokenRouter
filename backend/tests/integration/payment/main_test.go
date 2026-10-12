//go:build !integration

package payment_test

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestMain 在运行 HTTP 测试前设置 Gin 测试模式和 UTC 时区。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	// 测试进程使用 UTC，保证时间夹具不受本地时区影响。
	time.Local = time.UTC
	m.Run()
}
