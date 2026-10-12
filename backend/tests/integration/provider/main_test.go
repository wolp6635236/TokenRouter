package provider_test

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

var runProviderTests = func(m *testing.M) int { return m.Run() }

// TestMain 在测试进程启动时统一设置 Gin 模式，保留测试中的并发行为。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(runProviderTests(m))
}
