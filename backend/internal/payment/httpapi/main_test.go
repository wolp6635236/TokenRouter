package httpapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在测试启动前统一设置模式，保留各协议测试的并行执行。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
