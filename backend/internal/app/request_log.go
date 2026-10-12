package app

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/requestlog"
	"github.com/TokenFlux/TokenRouter/internal/requestlog/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/requestlog/postgres"
)

// provideRequestLog 为请求记录绑定数据库和后台批写的启停。
func provideRequestLog(db *sql.DB, cfg *config.Config, manager *lifecycle.Manager) *requestlog.Service {
	service := requestlog.NewService(postgres.NewStore(db), cfg.RequestLog.RetentionDays,
		func(format string, args ...any) { logging.LegacyPrintf("request.records", format, args...) })
	manager.Register(lifecycle.Hook{Name: "RequestRecords", StartOrder: 185, StopOrder: 790, Start: service.Start, Stop: service.Stop})
	return service
}

// registerRequestLogRoutes 将诊断查询注册在管理员认证和审计中间件之后。
func registerRequestLogRoutes(v1 *gin.RouterGroup, requests *requestlog.Service, adminAuth, auditLog, limiter gin.HandlerFunc) {
	handler := httpapi.NewHandler(requests)
	group := v1.Group("/admin/requests", adminAuth, auditLog, limiter)
	group.GET("", handler.Find)
	group.GET("/health", handler.Health)
	group.GET("/:request_id", handler.Find)
}
