package httpapi

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
)

// ForwardQoderAttempt 执行一次请求并同步输出响应，外层循环负责切换提供商。
// 保留旧入口的部分结果资格和结果字段，不额外填充首次输出或估算用量。
func ForwardQoderAttempt(ctx context.Context, c *gin.Context, runtime *gatewayadapter.QoderRuntime, value *provider.Record, body []byte, wire protocol.ProtocolID, responseModels ...string) (*forward.MessagesResult, error) {
	responseModel := qoder.FirstNonEmptyQoder(responseModels...)
	if responseModel == "" {
		responseModel = strings.TrimSpace(qoder.GjsonString(body, "model"))
	}
	executor, input := runtime.PrepareQoderTarget(QoderRequestMetadata(c), value, body, wire, responseModel)
	result, err := executor.Execute(ctx, input, ResponseSink{Writer: c.Writer})
	if err != nil {
		runtime.ObserveQoderFailure(ctx, value, err)
		if !result.Served || !result.HasUsage {
			return nil, err
		}
	}
	return &forward.MessagesResult{RequestID: result.RequestID, Model: result.Model, UpstreamModel: result.UpstreamModel, Usage: result.Usage, Stream: result.Stream, Duration: result.Duration, ClientDisconnect: result.ClientDisconnect}, err
}

// QoderRequestMetadata 为一次平台执行复制请求头并传递现有客户端标记。
func QoderRequestMetadata(c *gin.Context) qoder.RequestMetadata {
	result := qoder.RequestMetadata{APIKeyID: APIKeyIDFromContext(c)}
	if c != nil && c.Request != nil {
		result.Headers = c.Request.Header.Clone()
		result.ClaudeCode = requeststate.IsClaudeCodeClient(c.Request.Context())
	}
	return result
}
