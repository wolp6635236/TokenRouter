package httpapi

import (
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"

	"github.com/gin-gonic/gin"
)

func explicitGrokCacheSeed(c *gin.Context, body []byte, explicitKey string) string {
	return grok.ExplicitCacheSeed(grokCacheInput(c, explicitKey, ""), body)
}
