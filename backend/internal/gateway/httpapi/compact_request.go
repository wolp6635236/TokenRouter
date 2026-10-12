package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	openaierrors "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// OpenAICompactSessionSeedKey 是 HTTP 请求中存放会话种子的键。
const OpenAICompactSessionSeedKey = "openai_compact_session_seed"

// IsOpenAIResponsesCompactPath 识别旧 compact 端点及其允许转发的子路径。
func IsOpenAIResponsesCompactPath(c *gin.Context) bool {
	suffix := strings.TrimSpace(OpenAIResponsesRequestPathSuffix(c))
	return suffix == "/compact" || strings.HasPrefix(suffix, "/compact/")
}

// ResolveOpenAICompactSessionID 依次读取会话头、会话种子，缺失时生成随机标识。
func ResolveOpenAICompactSessionID(c *gin.Context) string {
	if c != nil {
		if sessionID := strings.TrimSpace(c.GetHeader("session_id")); sessionID != "" {
			return sessionID
		}
		if conversationID := strings.TrimSpace(c.GetHeader("conversation_id")); conversationID != "" {
			return conversationID
		}
		if seed, ok := c.Get(OpenAICompactSessionSeedKey); ok {
			if seedStr, ok := seed.(string); ok && strings.TrimSpace(seedStr) != "" {
				return strings.TrimSpace(seedStr)
			}
		}
	}
	return uuid.NewString()
}

// IsBareOpenAIResponsesPath 仅匹配裸 /responses 端点（无 /compact 等子路径），
// body-signal 提升在裸 Responses 路径上执行，/responses/{id}/... 子路径按自身类型处理。
func IsBareOpenAIResponsesPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	switch normalizedPath {
	case EndpointResponses, "/openai/v1/responses", "/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

// IsOpenAIRemoteCompactionV2Request 按 wire 形状识别原生 remote compaction v2 流式协议。
func IsOpenAIRemoteCompactionV2Request(body []byte) bool {
	stream, valid := ParseOpenAICompatibleStream(body)
	return valid && stream && protocolopenai.HasCompactionTriggerInInput(body)
}

// normalizeOpenAIResponsesCompactRequest 对 Codex remote compaction v2 使用流式 /responses，其他 body-signal 形状使用 Compact 桥接。
// 返回规范化后的正文，ok=false 表示错误已写出，调用方结束处理。
func (h *OpenAITextHandler) normalizeOpenAIResponsesCompactRequest(c *gin.Context, reqLog *zap.Logger, body []byte) ([]byte, bool) {
	isCompactRequest := IsOpenAIResponsesCompactPath(c)
	if !isCompactRequest && IsBareOpenAIResponsesPath(c) && protocolopenai.HasCompactionTriggerInInput(body) {
		if normalized, changed, err := protocolopenai.NormalizeCompactionTriggerInputOrder(body); err != nil {
			reqLog.Warn("codex.remote_compact.trigger_order_normalization_failed", zap.Error(err))
		} else if changed {
			body = normalized
		}
		if IsOpenAIRemoteCompactionV2Request(body) {
			// V2 请求在出站前保存协商标记，供请求头生成使用。
			MarkOpenAINativeCompactionV2(c)
			return body, true
		}
		c.Request.URL.Path = strings.TrimRight(c.Request.URL.Path, "/") + "/compact"
		isCompactRequest = true
		clientStream := gjson.GetBytes(body, "stream").Bool()
		if clientStream {
			MarkOpenAICompactClientStream(c)
		}
		reqLog.Info("codex.remote_compact.detected_body_signal", zap.Bool("client_stream", clientStream))
	}
	if !isCompactRequest {
		return body, true
	}
	if compactSeed := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); compactSeed != "" {
		c.Set(OpenAICompactSessionSeedKey, compactSeed)
	}
	normalizedCompactBody, normalizedCompact, compactErr := openaierrors.NormalizeOpenAICompactRequestBody(body)
	if compactErr != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to normalize compact request body")
		return nil, false
	}
	if normalizedCompact {
		body = normalizedCompactBody
	}
	return body, true
}

func (h *OpenAITextHandler) LogRemoteCompactOutcome(c *gin.Context, startedAt time.Time) {
	if !IsOpenAIResponsesCompactPath(c) {
		return
	}

	var (
		ctx    = context.Background()
		path   string
		status int
	)
	if c != nil {
		if c.Request != nil {
			ctx = c.Request.Context()
			if c.Request.URL != nil {
				path = strings.TrimSpace(c.Request.URL.Path)
			}
		}
		if c.Writer != nil {
			status = c.Writer.Status()
		}
	}

	outcome := "failed"
	if status >= 200 && status < 300 {
		outcome = "succeeded"
	}
	// Compact 心跳已提交 200 时，Ops 通过 MarkOpsStreamError 记录 response.failed 表达的失败状态。
	if outcome == "succeeded" && c != nil {
		if _, hasStreamErr := GetOpsStreamError(c); hasStreamErr {
			outcome = "failed"
		}
	}
	latencyMs := max(time.Since(startedAt).Milliseconds(), 0)

	fields := []zap.Field{
		zap.String("component", "handler.openai_gateway.responses"),
		zap.Bool("remote_compact", true),
		zap.String("compact_outcome", outcome),
		zap.Int("status_code", status),
		zap.Int64("latency_ms", latencyMs),
		zap.String("path", path),
		zap.Bool("force_codex_cli", h != nil && h.options.ForceCodexCLI),
	}

	if c != nil {
		if userAgent := strings.TrimSpace(c.GetHeader("User-Agent")); userAgent != "" {
			fields = append(fields, zap.String("request_user_agent", userAgent))
		}
		if v, ok := c.Get(OpsModelKey); ok {
			if model, ok := v.(string); ok && strings.TrimSpace(model) != "" {
				fields = append(fields, zap.String("request_model", strings.TrimSpace(model)))
			}
		}
		if v, ok := c.Get(OpsProviderIDKey); ok {
			if providerID, ok := v.(int64); ok && providerID > 0 {
				fields = append(fields, zap.Int64("provider_id", providerID))
			}
		}
		if c.Writer != nil {
			if upstreamRequestID := strings.TrimSpace(c.Writer.Header().Get("x-request-id")); upstreamRequestID != "" {
				fields = append(fields, zap.String("upstream_request_id", upstreamRequestID))
			} else if upstreamRequestID := strings.TrimSpace(c.Writer.Header().Get("X-Request-Id")); upstreamRequestID != "" {
				fields = append(fields, zap.String("upstream_request_id", upstreamRequestID))
			}
		}
	}

	log := logging.FromContext(ctx).With(fields...)
	if outcome == "succeeded" {
		log.Info("codex.remote_compact.succeeded")
		return
	}
	log.Warn("codex.remote_compact.failed")
}
