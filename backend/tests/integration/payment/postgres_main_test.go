//go:build integration

package payment_test

import (
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestMain 设置 Gin 测试模式和 UTC 时区，再启动 PostgreSQL 运行全部测试。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	time.Local = time.UTC
	os.Exit(runPostgresTests(m))
}
