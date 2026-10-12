package httpapi

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/server/clientip"
)

func CyberSessionExplicitBlockKey(id int64, c *gin.Context, body []byte) string {
	return session.HashCyberSessionBlockKey(id, ExplicitOpenAISessionID(c, body))
}

// FindCyberSessionForRequest 为会话查询提供延迟读取的客户端标识。
func FindCyberSessionForRequest(ctx context.Context, core *session.CyberBlocks, id int64, c *gin.Context, body []byte, ip, agent string) string {
	return core.Find(ctx, session.CyberLookup{APIKeyID: id, Body: body, ClientIP: ip, UserAgent: agent, ExplicitKey: func() string { return CyberSessionExplicitBlockKey(id, c, body) }})
}

func FindBlockedCyberSession(ctx context.Context, core *session.CyberBlocks, id int64, c *gin.Context, body []byte) string {
	if core == nil {
		return ""
	}
	ip, agent := "", ""
	if c != nil {
		ip = strings.TrimSpace(clientip.GetClientIP(c))
		agent = c.GetHeader("User-Agent")
	}
	return FindCyberSessionForRequest(ctx, core, id, c, body, ip, agent)
}
