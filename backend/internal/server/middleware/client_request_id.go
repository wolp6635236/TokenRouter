package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/timing"
)

const (
	clientRequestIDHeader         = "X-Client-Request-ID"
	internalRequestIDHeader       = "X-TokenRouter-Request-ID"
	legacyInternalRequestIDHeader = "X-Sub2API-Request-ID"

	maxPersistentRequestIDBytes = 64
)

// ClientRequestID 为请求生成内部关联 ID，并把调用方 ID 单独保存为 parent_client_request_id。
// 外部 ID 用于跨服务排障，结算幂等和身份判断使用内部 ID。
func ClientRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}

		// 入口时间在读取请求体和提供商调度前记录，用于区分上传与应用内耗时。
		ctx := c.Request.Context()
		if _, ok := ctx.Value(telemetry.RequestStartedAt).(time.Time); !ok {
			ctx = context.WithValue(ctx, telemetry.RequestStartedAt, time.Now())
		}
		ctx = timing.WithHTTPTrace(ctx)

		// 已存在的 context 值来自受信任的内部调用，优先用作内部 ID。
		internalID, valid := normalizeCorrelationIDFromContext(ctx, telemetry.ClientRequestID)
		if !valid {
			internalID = telemetry.NewRequestID()
		}
		parentID, _ := normalizeCorrelationID(c.GetHeader(clientRequestIDHeader))
		ctx = context.WithValue(ctx, telemetry.ClientRequestID, internalID)
		if parentID != "" {
			ctx = context.WithValue(ctx, telemetry.ParentClientRequestID, parentID)
		}
		requestLogger := logging.FromContext(ctx).With(zap.String("request_id", internalID))
		if parentID != "" {
			requestLogger = requestLogger.With(zap.String("parent_client_request_id", parentID))
		}
		ctx = logging.IntoContext(ctx, requestLogger)
		c.Request = c.Request.WithContext(ctx)
		// 清除调用方传入的内部关联头，服务生成的内部 ID 写入响应。
		c.Request.Header.Del(internalRequestIDHeader)
		c.Request.Header.Del(legacyInternalRequestIDHeader)
		// 将关联 ID 写入响应，服务生成的内部 ID 用于响应诊断。
		if parentID != "" {
			c.Header(clientRequestIDHeader, parentID)
		} else {
			c.Header(clientRequestIDHeader, internalID)
		}
		// 专用内部头携带下游响应的诊断 ID。
		c.Header(internalRequestIDHeader, internalID)
		// 兼容仍按旧头读取诊断 ID 的客户端，两者不能产生不同身份。
		c.Header(legacyInternalRequestIDHeader, internalID)
		c.Next()
	}
}

func normalizeCorrelationIDFromContext(ctx context.Context, key telemetry.ContextKey) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, _ := ctx.Value(key).(string)
	return normalizeCorrelationID(v)
}

// normalizeCorrelationID 清洗关联 ID 并检查长度和 ASCII 字符范围。
func normalizeCorrelationID(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if value == "" || len(value) > maxPersistentRequestIDBytes {
		return "", false
	}
	// 关联 ID 可能来自不可信请求头，只允许可安全放入日志和 HTTP Header 的 ASCII 字符。
	for _, ch := range []byte(value) {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == ':' {
			continue
		}
		return "", false
	}
	return value, true
}
