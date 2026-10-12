package httpapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在测试进程开始时设置 Gin 模式，后续测试共用该设置。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
