package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
)

// OpenAICompatibleRequestPlatform 不从分组推断平台，空值表示按提供商能力选择。
func OpenAICompatibleRequestPlatform(_ *apikey.APIKey) string { return "" }

// EffectiveAPIKeyPlatform 返回当前 API key 在 handler 层应使用的平台。
// 强制平台路由由中间件单独处理；未强制平台时由候选提供商决定。
func EffectiveAPIKeyPlatform(c *gin.Context, apiKey *apikey.APIKey) string {
	if c != nil {
		if forced, ok := keyhttp.GetForcePlatformFromContext(c); ok && strings.TrimSpace(forced) != "" {
			return strings.TrimSpace(forced)
		}
	}
	return OpenAICompatibleRequestPlatform(apiKey)
}
