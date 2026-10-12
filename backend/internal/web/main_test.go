package web

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在测试运行前设置 Gin 测试模式。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
