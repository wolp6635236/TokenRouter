package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// openAIRawStreamTruncatedUpstreamMessage 是 raw CC 直转路径上游截断的 Ops 消息。
const openAIRawStreamTruncatedUpstreamMessage = "Upstream Chat Completions stream ended before any terminal chunk"

// NewOpenAIRawTruncationFailure 处理"上游截断且尚未向客户端写出任何
// 字节"的情况：响应头还没提交，可以透明换号重试，客户端不会看到半截流。
func NewOpenAIRawTruncationFailure(
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	upstreamRequestID string,
	cause error,
) *forwardcore.UpstreamFailoverError {
	RecordOpenAIRawTruncation(c, provider, upstreamRequestID, cause, "failover")

	headers := http.Header{}
	if id := strings.TrimSpace(upstreamRequestID); id != "" {
		headers.Set("x-request-id", id)
	}
	return &forwardcore.UpstreamFailoverError{
		StatusCode:      http.StatusBadGateway,
		ResponseBody:    openAIRawStreamTruncatedErrorBody(cause),
		ResponseHeaders: headers,
	}
}

// RecordOpenAIRawTruncation 将上游截断写入 Ops 上下文，供错误日志和提供商健康统计使用。
func RecordOpenAIRawTruncation(
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	upstreamRequestID string,
	cause error,
	kind string,
) {
	if c == nil {
		return
	}
	message := openAIRawStreamTruncatedMessage(cause)
	platform := capability.PlatformOpenAI
	providerID := int64(0)
	providerName := ""
	if provider != nil {
		platform = provider.Record.Platform
		providerID = provider.Record.ID
		providerName = provider.Record.Name
	}
	SetOpsUpstreamError(c, http.StatusBadGateway, message, "")
	AppendOpsUpstreamError(c, ops.OpsUpstreamErrorEvent{
		Platform:           platform,
		ProviderID:         providerID,
		ProviderName:       providerName,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               kind,
		Message:            message,
	})
}

// openAIRawStreamTruncatedMessage 拼出 Ops 消息：干净 EOF 没有底层错误可带，
// 传输层错误（connection reset / http2 stream error）则保留原因以便定位。
func openAIRawStreamTruncatedMessage(cause error) string {
	if cause == nil || errors.Is(cause, openai.ErrOpenAIUpstreamStreamTruncated) {
		return openAIRawStreamTruncatedUpstreamMessage
	}
	return openAIRawStreamTruncatedUpstreamMessage + ": " + cause.Error()
}

// openAIRawStreamTruncatedErrorBody 构造 failover 错误体，code/message 与
// 写出后走 openAIUpstreamStreamReadError 的客户端分类保持一致。
func openAIRawStreamTruncatedErrorBody(cause error) []byte {
	code, message := openai.ClassifyUpstreamStreamReadError(cause)
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    "upstream_error",
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"upstream_error","code":"` + openai.OpenAIUpstreamStreamTruncatedCode +
			`","message":"Upstream response stream ended before completion"}}`)
	}
	return body
}
