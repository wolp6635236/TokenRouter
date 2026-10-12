package app

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在测试开始前设置 Gin 全局模式，供全部测试和并行夹具使用。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
