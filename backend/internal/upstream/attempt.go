package upstream

import (
	"context"
	"net/http"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
)

// ExecutionTarget 是已选目标的受控句柄，具体凭据只对对应平台执行器可见。
type ExecutionTarget interface{ TargetID() int64 }

// AttemptInput 保存一次尝试的协议、报文和目标。
type AttemptInput struct {
	Protocol      protocol.ProtocolID
	Body          []byte
	ResponseModel string
	Stream        bool
	Target        ExecutionTarget
}

// AttemptResult 与 error 独立返回；失败也可以携带已发生服务及上游已观测的用量。
type AttemptResult struct {
	// UpstreamResponseModel 是协议转换前的上游模型声明；空值表示未声明。
	UpstreamResponseModel string
	// HTTPCommitted 和 RetryCommitted 分别记录 HTTP 提交和重试窗口关闭状态。
	HTTPCommitted, RetryCommitted bool
	RequestID                     string
	// Responses 响应附带的标识、搜索次数和图片用量。
	ResponseID                                 string
	SearchCount, ImageInputTokens              int
	ImageOutputSizes                           []string
	UpstreamHeaders                            http.Header
	Model, UpstreamModel                       string
	Usage                                      TokenUsage
	HasUsage, Served, Stream, ClientDisconnect bool
	// FailureClass 记录失败类型，资金和重试决策由调用方负责。
	// EstimatedTokenCount 是 countTokens 的本地估算，结算使用 Usage。
	EstimatedTokenCount *int
	// ObservedImages 记录已观测的图片张数，价格与回退规则由调用方决定。
	ObservedImages int
	// AudioUsage 记录语音用量，价格和完成处理由调用方负责。
	AudioUsage *protocol.AudioUsage
	// MediaBody 是本次有界读取并输出的媒体 JSON，任务完成与资金规则由调用方处理。
	MediaBody    []byte
	FailureClass string
	Cancelled    bool
	Duration     time.Duration
	// FirstSemanticOutput 记录首段内容的耗时，前导进度事件不计入。
	FirstSemanticOutput          *time.Duration
	FirstTokenMs                 *int
	ServiceTier, ReasoningEffort string
}

// Executor 执行一次上游请求，提供商切换和资金完成处理由网关负责。
// @project-doc docs/architecture/gateway_request_lifecycle.md#upstream_attempt_ownership
type Executor interface {
	Execute(context.Context, AttemptInput, OutputSink) (AttemptResult, error)
}

// TokenUsage 是协议解析器统计的令牌用量。
type TokenUsage = protocol.TokenUsage
