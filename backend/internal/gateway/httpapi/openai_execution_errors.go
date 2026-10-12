package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
)

// WriteFastPolicyBlockedResponse 记录策略拒绝，并按当前响应状态写出 JSON 或 SSE 错误。
func WriteFastPolicyBlockedResponse(c *gin.Context, err *tierpolicy.BlockedError) {
	if c == nil || err == nil {
		return
	}
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	WriteForwardFastPolicyBlocked(c, err.Message, StopOpenAICompactSSEKeepaliveCommitted, func(c *gin.Context, status int, kind, message string) {
		WriteOpenAICompactSSEFailureMessage(c, status, kind, message, MarkOpsStreamError)
	})
}

// WriteForwardAnthropicError 写出 Anthropic JSON 错误。
func WriteForwardAnthropicError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// WriteForwardAnthropicErrorBody 将有效的 error 对象包装为 Anthropic 错误，否则原样写出正文。
func WriteForwardAnthropicErrorBody(c *gin.Context, statusCode int, body []byte) {
	errorObject := gjson.GetBytes(body, "error")
	if !errorObject.Exists() || !gjson.Valid(errorObject.Raw) {
		c.Data(statusCode, "application/json; charset=utf-8", body)
		return
	}
	wrapped := []byte(`{"type":"error","error":` + errorObject.Raw + `}`)
	c.Data(statusCode, "application/json; charset=utf-8", wrapped)
}

// BuildForwardAnthropicStreamError 构造 Anthropic SSE error 事件。
func BuildForwardAnthropicStreamError(errType, message string) string {
	payload, err := json.Marshal(gin.H{
		"type": "error",
		"error": gin.H{
			"type":    strings.TrimSpace(errType),
			"message": strings.TrimSpace(message),
		},
	})
	if err != nil {
		return `event: error` + "\n" + `data: {"type":"error","error":{"type":"invalid_request_error","message":"Request blocked by upstream cyber-security policy"}}` + "\n\n"
	}
	return "event: error\ndata: " + string(payload) + "\n\n"
}

// WriteForwardChatError 标记响应提交并写出 Chat JSON 错误。
func WriteForwardChatError(c *gin.Context, statusCode int, errType, message string) {
	MarkResponseCommitted(c)
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// WriteForwardChatErrorBody 标记响应提交并写出 JSON 错误正文。
func WriteForwardChatErrorBody(c *gin.Context, statusCode int, body []byte) {
	MarkResponseCommitted(c)
	c.Data(statusCode, "application/json; charset=utf-8", body)
}

// BuildForwardChatStreamError 构造 Chat SSE 错误事件。
func BuildForwardChatStreamError(code, message string) string {
	payload, err := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    strings.TrimSpace(code),
			"message": strings.TrimSpace(message),
		},
	})
	if err != nil {
		return `data: {"error":{"type":"invalid_request_error","code":"cyber_policy","message":"Request blocked by upstream cyber-security policy"}}` + "\n\n"
	}
	return "data: " + string(payload) + "\n\n"
}

// WriteForwardResponsesFallbackError 写出 Responses JSON 错误。
func WriteForwardResponsesFallbackError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// WriteForwardPassthroughErrorHeaders 设置错误响应头并透传有效的 Retry-After。
func WriteForwardPassthroughErrorHeaders(dst, src http.Header) {
	if dst == nil {
		return
	}
	dst.Set("Content-Type", "application/json; charset=utf-8")
	dst.Set("Cache-Control", "no-store")
	dst.Del("Retry-After")
	if src == nil {
		return
	}
	rawRetryAfter := strings.TrimSpace(src.Get("Retry-After"))
	if ValidForwardPassthroughRetryAfter(rawRetryAfter, time.Now()) {
		dst.Set("Retry-After", rawRetryAfter)
	}
}

// ValidForwardPassthroughRetryAfter 检查 Retry-After 是否为正秒数或未来时间。
func ValidForwardPassthroughRetryAfter(raw string, now time.Time) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	delaySeconds := true
	for i := range len(raw) {
		if raw[i] < '0' || raw[i] > '9' {
			delaySeconds = false
			break
		}
	}
	if delaySeconds {
		seconds, err := strconv.ParseUint(raw, 10, 64)
		return err == nil && seconds > 0
	}
	parsed, err := http.ParseTime(raw)
	return err == nil && parsed.After(now)
}

// WriteSanitizedForwardPassthroughError 按上游状态生成客户端错误消息，认证和权限错误返回 502。
func WriteSanitizedForwardPassthroughError(c *gin.Context, upstreamStatus int, upstreamHeaders http.Header, compact func(*gin.Context, int, []byte) bool) {
	downstreamStatus := upstreamStatus
	message := "Upstream request failed"
	switch upstreamStatus {
	case http.StatusUnauthorized:
		downstreamStatus = http.StatusBadGateway
		message = "Upstream authentication failed"
	case http.StatusForbidden:
		downstreamStatus = http.StatusBadGateway
		message = "Upstream access denied"
	default:
		if upstreamStatus >= http.StatusInternalServerError {
			message = "Upstream service temporarily unavailable"
		}
	}
	WriteForwardPassthroughErrorEnvelope(c, downstreamStatus, upstreamHeaders, message, compact)
}

// WriteForwardPassthroughErrorEnvelope 优先写出 Compact 错误事件，其他请求写出 JSON 错误。
func WriteForwardPassthroughErrorEnvelope(c *gin.Context, downstreamStatus int, upstreamHeaders http.Header, message string, compact func(*gin.Context, int, []byte) bool) {
	if c == nil {
		return
	}
	body, _ := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "upstream_error",
			"message": message,
		},
	})
	if compact(c, downstreamStatus, body) {
		return
	}
	WriteForwardPassthroughErrorHeaders(c.Writer.Header(), upstreamHeaders)
	c.Data(downstreamStatus, "application/json; charset=utf-8", body)
}

// WriteForwardFastPolicyBlocked 保留 compact 心跳已提交时的终态 SSE，否则返回原 403 信封。
func WriteForwardFastPolicyBlocked(c *gin.Context, message string, stop func(*gin.Context) bool, failure func(*gin.Context, int, string, string)) {
	if stop(c) {
		failure(c, http.StatusForbidden, "permission_error", message)
		return
	}
	c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "permission_error", "message": message}})
}

// WriteOpenAIForwardRejection 按 OpenAI 错误格式写出拒绝原因及可选参数名。
func WriteOpenAIForwardRejection(c *gin.Context, status int, kind, message, param string) {
	if c == nil {
		return
	}
	payload := gin.H{"type": kind, "message": message}
	if param != "" {
		payload["param"] = param
	}
	c.JSON(status, gin.H{"error": payload})
}
