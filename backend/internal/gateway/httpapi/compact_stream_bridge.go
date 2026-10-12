package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/TokenFlux/TokenRouter/internal/gateway/compact"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// openAICompactClientStreamKey 标记 body-signal Compact 原始正文中的 stream:true（Codex remote compact v2，#3777）。
// 规范化删除 stream 后，上游使用 unary /responses/compact JSON。客户端需要 SSE 中恰好一个 compaction 类型的 output_item.done 和 response.completed。
// 缺少事件时 Codex 报 stream closed before response.completed 并重连（#3875）。
const openAICompactClientStreamKey = "openai_compact_client_stream"

// CompactStreamErrorObserver 保留客户端流错误的原观测时点和分类。
type CompactStreamErrorObserver func(*gin.Context, string, string, int)

// 明确的终止帧字段保留 created_at、空 output 及原 JSON 键顺序。
type compactFailedEvent struct {
	Response compactFailedResponse `json:"response"`
	Type     string                `json:"type"`
}
type compactFailedResponse struct {
	CreatedAt int64              `json:"created_at"`
	Error     compactFailedError `json:"error"`
	ID        string             `json:"id"`
	Object    string             `json:"object"`
	Output    []json.RawMessage  `json:"output"`
	Status    string             `json:"status"`
}
type compactFailedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// MarkOpenAICompactClientStream 由 handler 在 body-signal 提升时调用，记录
// 客户端的原始 stream 意图，供响应写回阶段决定是否合成 SSE。
func MarkOpenAICompactClientStream(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(openAICompactClientStreamKey, true)
}

func OpenAICompactClientWantsStream(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(openAICompactClientStreamKey)
	if !ok {
		return false
	}
	wants, _ := value.(bool)
	return wants
}

// WriteOpenAICompactSSEBridge 将 Compact JSON 转换为 Codex remote compact v2 的 Responses SSE。
// 客户端标记流式、状态码为 2xx 且正文为 JSON 对象时写出事件，其他情况返回 false，调用方自行写回。
// 心跳已提交 200 时，此函数接管响应，非 2xx 或转换失败通过 response.failed 返回。
func WriteOpenAICompactSSEBridge(c *gin.Context, statusCode int, finalResponse []byte, observe CompactStreamErrorObserver) bool {
	if c == nil || !OpenAICompactClientWantsStream(c) {
		return false
	}
	// 先停心跳再写回，避免注释行与最终事件交错；停止后经互斥锁与心跳
	// goroutine 建立 happens-before，可安全接管 ResponseWriter。
	committed := StopOpenAICompactSSEKeepaliveCommitted(c)
	if statusCode < 200 || statusCode >= 300 {
		if committed {
			WriteOpenAICompactSSEFailure(c, statusCode, finalResponse, observe)
			return true
		}
		return false
	}
	payload, ok := BuildOpenAICompactSSEPayload(finalResponse)
	if !ok {
		if committed {
			WriteOpenAICompactSSEFailure(c, http.StatusBadGateway, finalResponse, observe)
			return true
		}
		return false
	}
	if !committed {
		header := c.Writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(statusCode)
	}
	_, _ = c.Writer.Write(payload)
	c.Writer.Flush()
	return true
}

// WriteOpenAICompactSSEFailure 从上游错误 body 提取错误消息后，以
// response.failed 终止事件回传。仅用于心跳已提交 200、无法再按 HTTP 状态码
// 回传错误的场景。
func WriteOpenAICompactSSEFailure(c *gin.Context, statusCode int, errorBody []byte, observe CompactStreamErrorObserver) {
	message := ""
	if len(errorBody) > 0 {
		message = logredact.SanitizeUpstreamQueries(strings.TrimSpace(upstream.ExtractErrorMessage(errorBody)))
	}
	if message == "" {
		message = "Upstream compact request failed with HTTP " + strconv.Itoa(statusCode)
	}
	WriteOpenAICompactSSEFailureMessage(c, statusCode, "upstream_error", message, observe)
}

// WriteOpenAICompactSSEFailureMessage 写出 response.failed 终止事件。Codex 对
// 流式 Responses 请求把 response.failed 作为合法终止事件处理（普通 error 帧
// 不被识别，会退化为 "stream closed before response.completed" 盲重连）。
// 同时标记流内错误，保证挂在 200 流上的失败仍进入 ops 错误看板。
func WriteOpenAICompactSSEFailureMessage(c *gin.Context, statusCode int, errType, message string, observe CompactStreamErrorObserver) {
	if c == nil {
		return
	}
	if observe != nil {
		observe(c, errType, message, statusCode)
	}
	responseID := newCompactResponseID()
	payload, err := json.Marshal(compactFailedEvent{
		Response: compactFailedResponse{
			CreatedAt: time.Now().Unix(),
			Error:     compactFailedError{Code: errType, Message: message},
			ID:        responseID,
			Object:    "response",
			Output:    []json.RawMessage{},
			Status:    "failed",
		},
		Type: "response.failed",
	})
	if err != nil {
		return
	}
	_, _ = c.Writer.Write([]byte("event: response.failed\ndata: "))
	_, _ = c.Writer.Write(payload)
	_, _ = c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}

func newCompactResponseID() string {
	return "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// BuildOpenAICompactSSEPayload 向协议转换函数传入响应 ID 生成器。
func BuildOpenAICompactSSEPayload(body []byte) ([]byte, bool) {
	return compact.StreamPayload(body, newCompactResponseID)
}
