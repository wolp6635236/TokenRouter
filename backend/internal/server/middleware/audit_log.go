package middleware

import (
	"github.com/gin-gonic/gin"

	audit "github.com/TokenFlux/TokenRouter/internal/audit/httpapi"
)

type AuditLogMiddleware = audit.AuditLogMiddleware

func SetAuditAction(c *gin.Context, s string)              { audit.SetAuditAction(c, s) }
func SetAuditActor(c *gin.Context, id int64, email string) { audit.SetAuditActor(c, id, email) }
