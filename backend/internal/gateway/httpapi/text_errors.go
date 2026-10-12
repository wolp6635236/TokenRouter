package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

func WriteAnthropicStreamError(c *gin.Context, status int, errType, code, message string, streamStarted bool, observe func(*gin.Context, string, string, int)) {
	if streamStarted {
		// ping 或部分数据 Flush 后，状态码已提交为 200，错误通过 SSE 返回。
		// 标记流内错误，ops_error_logger 据此补记 status < 400 的失败，例如并发限流。
		if observe != nil {
			observe(c, errType, message, status)
		}

		// Codex CLI 接受 response.completed、failed、incomplete 或 cancelled 终止事件，Anthropic 上游的 Responses 也按此格式输出。
		if InboundIsResponses(c) {
			if WriteResponsesFailedSSE(c, errType, code, message, ErrorRequestID(c), ErrorRequestModel(c)) {
				return
			}
		}
		// Stream already started, send error as SSE event then close
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			// SSE 错误事件使用固定 schema，通过 Quote 拼接 JSON。
			errorEvent := `data: {"type":"error","error":{"type":` + strconv.Quote(errType) + `,"message":` + strconv.Quote(message) + `}}` + "\n\n"
			if code != "" {
				errorObject := gin.H{"type": errType, "code": code, "message": message}
				payload, err := json.Marshal(gin.H{"type": "error", "error": errorObject})
				if err == nil {
					errorEvent = "data: " + string(payload) + "\n\n"
				}
			}
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}

	// Normal case: return JSON response with proper status code
	if code == "" {
		WriteAnthropicError(c, status, errType, "", message)
	} else {
		WriteAnthropicError(c, status, errType, code, message)
	}
}

func WriteAnthropicError(c *gin.Context, status int, errType, code, message string) {
	errorObject := gin.H{"type": errType, "message": message}
	if code != "" {
		errorObject["code"] = code
	}
	c.JSON(status, gin.H{
		"type":  "error",
		"error": errorObject,
	})
}

func BillingErrorDetails(err error) (status int, code, message string, retryAfter int) {
	if errors.Is(err, billing.ErrBillingServiceUnavailable) {
		msg := apperror.Message(err)
		if msg == "" {
			msg = "Billing service temporarily unavailable. Please retry later."
		}
		return http.StatusServiceUnavailable, "billing_service_error", msg, 0
	}
	if errors.Is(err, billing.ErrAPIKeyRateLimit5hExceeded) {
		msg := apperror.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	if errors.Is(err, billing.ErrAPIKeyRateLimit1dExceeded) {
		msg := apperror.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	if errors.Is(err, billing.ErrAPIKeyRateLimit7dExceeded) {
		msg := apperror.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	// 用户/分组 RPM 超限统一映射为 HTTP 429；保留与其它 rate_limit 一致的错误码便于客户端分类。
	// 返回 Retry-After 秒数（当前分钟剩余秒数），让 SDK 自动退避。
	if errors.Is(err, scheduler.ErrGroupRPMExceeded) || errors.Is(err, scheduler.ErrUserRPMExceeded) {
		msg := apperror.Message(err)
		retrySeconds := 60 - int(time.Now().Unix()%60)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, retrySeconds
	}

	msg := apperror.Message(err)
	if msg == "" {
		logging.L().With(
			zap.String("component", "handler.gateway.billing"),
			zap.Error(err),
		).Warn("gateway.billing_error_missing_message")
		msg = "Billing error"
	}
	return http.StatusForbidden, "billing_error", msg, 0
}

// ErrorRequestID、ErrorRequestModel 读取原观测字段，不推断或恢复业务状态。
func ErrorRequestID(c *gin.Context) string {
	if c != nil && c.Request != nil {
		value, _ := c.Request.Context().Value(telemetry.RequestID).(string)
		return value
	}
	return ""
}

func ErrorRequestModel(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.GetString("ops_model"))
}
