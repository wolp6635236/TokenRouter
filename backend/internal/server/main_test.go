package server

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在运行测试前将 Gin 设为测试模式。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
