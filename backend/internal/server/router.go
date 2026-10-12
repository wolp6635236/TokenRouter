package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RouterRuntime 包含 app 提供的中间件、前端处理器和路由注册函数。
type RouterRuntime struct {
	Middleware []gin.HandlerFunc
	Frontend   gin.HandlerFunc
	Register   []func(*gin.Engine)
}

// SetupRouter 依次挂载全局中间件、前端处理器和路由。
func SetupRouter(r *gin.Engine, runtime *RouterRuntime) *gin.Engine {
	r.Use(runtime.Middleware...)
	if runtime.Frontend != nil {
		r.Use(runtime.Frontend)
	}
	RegisterCommonRoutes(r)
	for _, register := range runtime.Register {
		register(r)
	}
	return r
}

// RegisterCommonRoutes 注册健康检查、遥测接收和初始化状态路由。
func RegisterCommonRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Claude Code 遥测请求直接返回 200。
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 正常运行时返回已完成的初始化状态，前端据此判断初始化后服务已重启。
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}
