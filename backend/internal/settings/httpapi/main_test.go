package httpapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在测试进程启动时将 Gin 设置为测试模式。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
