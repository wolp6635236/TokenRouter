package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
)

// Key 上下文包含认证字段和排障用的 fallback 字段，认证结果由认证流程给出。
const (
	ContextKeyAPIKey            = "api_key"
	ContextKeyOpsFallbackAPIKey = "ops_fallback_api_key"
	ContextKeyForcePlatform     = "force_platform"
)

// APIKeyAuthMiddleware 是 API Key 认证的 Gin 中间件。
type APIKeyAuthMiddleware gin.HandlerFunc

// GetAPIKeyFromContext 从上下文中获取 API key。
func GetAPIKeyFromContext(c *gin.Context) (*apikey.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*apikey.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时回退使用。
// 与 ContextKeyAPIKey 区分：写入它不代表请求已通过鉴权，因此不影响 handler、
// 审计日志等对“已鉴权”的判断。
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *apikey.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey 读取 Ops 错误日志专用的回退 API Key。
func GetOpsFallbackAPIKey(c *gin.Context) (*apikey.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*apikey.APIKey)
	return apiKey, ok
}

// ForcePlatform 返回设置强制平台的中间件
// 同时设置 request.Context（供 Service 使用）和 gin.Context（供 Handler 快速检查）。
func ForcePlatform(platform string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 设置到 request.Context，使用 requeststate.ForcePlatform 供 Service 层读取
		ctx := apikey.WithForcePlatform(c.Request.Context(), platform)
		c.Request = c.Request.WithContext(ctx)
		// 同时设置到 gin.Context，供 Handler 快速检查
		c.Set(string(ContextKeyForcePlatform), platform)
		c.Next()
	}
}

// HasForcePlatform 检查是否有强制平台（用于 Handler 跳过分组检查）。
func HasForcePlatform(c *gin.Context) bool {
	_, exists := c.Get(string(ContextKeyForcePlatform))
	return exists
}

// GetForcePlatformFromContext 从 gin.Context 获取强制平台。
func GetForcePlatformFromContext(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(ContextKeyForcePlatform))
	if !exists {
		return "", false
	}
	platform, ok := value.(string)
	return platform, ok
}
