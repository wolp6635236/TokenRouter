package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/server/clientip"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

const (
	codexCLIOnlyHeaderValueMaxBytes = 256

	// openAIUpstreamClientErrorFallbackType 是上游缺少 error.type 时使用的类型。
	openAIUpstreamClientErrorFallbackType = "invalid_request_error"

	// openAIUpstreamClientErrorFallbackMessage 是上游缺少可用消息时使用的文案。
	openAIUpstreamClientErrorFallbackMessage = "Upstream rejected the request"
)

// codex_cli_only 拒绝时，诊断日志记录以下白名单中的请求头。
var codexCLIOnlyDebugHeaderWhitelist = []string{
	"User-Agent",
	"Content-Type",
	"Accept",
	"Accept-Language",
	"OpenAI-Beta",
	"Originator",
	"Session_ID",
	"Conversation_ID",
	"X-Request-ID",
	"X-Client-Request-ID",
	"X-Forwarded-For",
	"X-Real-IP",
}

func AppendCodexRejectedRequestFields(fields []zap.Field, c *gin.Context, body []byte) []zap.Field {
	if c == nil || c.Request == nil {
		return fields
	}

	req := c.Request
	requestModel, requestStream, promptCacheKey := requeststate.OpenAIRequestMetaFromBody(body)
	fields = append(fields,
		zap.String("request_method", strings.TrimSpace(req.Method)),
		zap.String("request_path", strings.TrimSpace(req.URL.Path)),
		zap.String("request_query", strings.TrimSpace(req.URL.RawQuery)),
		zap.String("request_host", strings.TrimSpace(req.Host)),
		zap.String("request_client_ip", strings.TrimSpace(clientip.GetClientIP(c))),
		zap.String("request_remote_addr", strings.TrimSpace(req.RemoteAddr)),
		zap.String("request_user_agent", strings.TrimSpace(req.Header.Get("User-Agent"))),
		zap.String("request_content_type", strings.TrimSpace(req.Header.Get("Content-Type"))),
		zap.Int64("request_content_length", req.ContentLength),
		zap.Bool("request_stream", requestStream),
	)
	if requestModel != "" {
		fields = append(fields, zap.String("request_model", requestModel))
	}
	if promptCacheKey != "" {
		fields = append(fields, zap.String("request_prompt_cache_key_sha256", upstream.HashSensitiveValueForLog(promptCacheKey)))
	}

	if headers := SnapshotCodexRejectedHeaders(req.Header); len(headers) > 0 {
		fields = append(fields, zap.Any("request_headers", headers))
	}
	fields = append(fields, zap.Int("request_body_size", len(body)))
	return fields
}

func SnapshotCodexRejectedHeaders(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	result := make(map[string]string, len(codexCLIOnlyDebugHeaderWhitelist))
	for _, key := range codexCLIOnlyDebugHeaderWhitelist {
		value := strings.TrimSpace(header.Get(key))
		if value == "" {
			continue
		}
		result[strings.ToLower(key)] = logredact.TruncateUTF8(value, codexCLIOnlyHeaderValueMaxBytes)
	}
	return result
}

// IsOpenAIDeterministicClientError 判断错误是否为不可通过换号或重试恢复的客户端请求错误。
// 普通提供商与池模式使用不同故障转移规则，判断时同时检查分类结果。
func IsOpenAIDeterministicClientError(statusCode int, shouldFailover bool) bool {
	return statusCode == http.StatusBadRequest && !shouldFailover
}

// WriteOpenAIUpstreamClientError 以 OpenAI 错误信封回写确定性客户端错误。
// message 已由调用方完成身份脱敏和敏感查询参数清洗；这里只保留诊断必需字段，
// 不直接透传上游完整正文，避免把扩展字段或内部调试信息暴露给客户端。
func WriteOpenAIUpstreamClientError(c *gin.Context, statusCode int, body []byte, upstreamMsg string) {
	errorPayload := gin.H{"type": openAIUpstreamClientErrorFallbackType}
	if errType := strings.TrimSpace(gjson.GetBytes(body, "error.type").String()); errType != "" {
		errorPayload["type"] = errType
	}
	if code := strings.TrimSpace(upstream.ExtractErrorCode(body)); code != "" {
		errorPayload["code"] = code
	}
	if param := strings.TrimSpace(gjson.GetBytes(body, "error.param").String()); param != "" {
		errorPayload["param"] = param
	}
	message := strings.TrimSpace(upstreamMsg)
	if message == "" {
		message = openAIUpstreamClientErrorFallbackMessage
	}
	errorPayload["message"] = message

	c.JSON(statusCode, gin.H{"error": errorPayload})
}
