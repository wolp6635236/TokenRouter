package googleforward

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在测试运行前设置 Gin 模式，夹具共享该设置。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}
