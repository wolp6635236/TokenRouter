package forward

import (
	"encoding/json"
	"time"

	protocolcore "github.com/TokenFlux/TokenRouter/internal/protocol"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

// OpenAIResult 保存 OpenAI 兼容执行的观测结果，恢复报文仍由本次执行私有持有。
type OpenAIResult struct {
	// LocalWarmup 表示网关本地完成的预热，完成处理跳过计费和上游健康更新。
	LocalWarmup bool `json:"-"`
	// UpstreamResponseModel 是协议转换前的上游模型声明；空值表示未声明。
	UpstreamResponseModel string
	// NativeUsage 保存非 OpenAI 执行器各自独立的输入分桶，桥接结果直接使用这些值。
	NativeUsage *protocolcore.TokenUsage
	RequestID   string
	ResponseID  string
	// UpstreamHeaders 是直接上游的响应头，用于按提供商配置解析上游请求标识。
	UpstreamHeaders map[string][]string
	Usage           protocolopenai.ForwardUsage
	Model           string // 原始模型（用于响应和日志显示）
	// BillingModel is the model used for cost calculation.
	// When non-empty, CalculateCost uses this instead of Model.
	// This is set by the Anthropic Messages conversion path where
	// the mapped upstream model differs from the client-facing model.
	BillingModel string
	// UpstreamModel is the actual model sent to the upstream provider after mapping.
	// Empty when no mapping was applied (requested model was used as-is).
	UpstreamModel string
	// UpstreamResponseServiceTier 是上游响应声明的实际服务档位，供计费只降档使用。
	UpstreamResponseServiceTier string
	// UpstreamEndpoint 记录本次使用的上游 API 路径，同一下游协议可以选择多个上游端点。
	UpstreamEndpoint string
	// ServiceTier is the final tier sent upstream after policy rewriting.
	// The upstream response declaration remains separate above and is reconciled
	// at usage-recording time, where the credential protocol is available.
	ServiceTier *string
	// ReasoningEffort 是最终上游请求中的推理档位；nil 表示未提供或不适用。
	ReasoningEffort *string
	// RequestedReasoningEffort 是策略与模型映射前客户端请求的推理档位。
	RequestedReasoningEffort *string
	Stream                   bool
	OpenAIWSMode             bool
	// UpstreamTerminalEvent 记录 Responses WebSocket 请求观测到的规范化终止事件；
	// 空值表示成功，适用于非 WebSocket 调用。
	UpstreamTerminalEvent string
	ResponseHeaders       map[string][]string
	Duration              time.Duration
	FirstTokenMs          *int
	ClientDisconnect      bool
	ImageCount            int
	ImageSize             string
	ImageInputSize        string
	ImageOutputSize       string
	ImageOutputSizes      []string
	ImageSizeSource       string
	ImageSizeBreakdown    map[string]int
	// UpstreamWarning 仅在上游成功完成传输但 terminal 事件携带风控拒绝时填充。
	UpstreamWarning *UpstreamWarning
	VideoCount      int
	VideoResolution string
	// VideoState 保存异步视频任务的状态，取值为 running、completed、failed 或 canceled。
	VideoState string
	// VideoDurationSeconds 是提交时请求的生成时长（xAI 按输出秒数计费），已归一化到 1-15 秒。
	VideoDurationSeconds int
	// WebSearchCalls 是 Codex alpha/search 网页搜索调用次数（每次成功请求为 1）。
	// 上游不返回 usage 字段，>0 时走按次计费（分组单价 × 次数 × 倍率）。
	WebSearchCalls int
	// SearchCount 是 Grok 原生 web_search 或工具搜索调用次数，按每千次计价。
	SearchCount int
	// AudioUsage 在有值时携带 Voice 计费单位。
	AudioUsage *protocolcore.AudioUsage

	wsReplayInput                 []json.RawMessage
	wsReplayInputExists           bool
	wsProviderFailoverReplayInput []json.RawMessage
}

// SucceededForScheduling 判断转发结果能否作为上游调度成功，并清除模型级短暂状态。
// 零值表示非 WebSocket 调用成功。
func (r *OpenAIResult) SucceededForScheduling() bool {
	if r != nil && r.LocalWarmup {
		return false
	}
	if r == nil || !r.OpenAIWSMode || r.UpstreamTerminalEvent == "" {
		return true
	}
	switch r.UpstreamTerminalEvent {
	case "response.completed", "response.done":
		return true
	default:
		return false
	}
}

// SetWSReplayInput 保存调用方为本轮取得的规范化恢复输入快照。
func (r *OpenAIResult) SetWSReplayInput(input []json.RawMessage, exists bool) {
	r.wsReplayInput = input
	r.wsReplayInputExists = exists
}

// WSReplayInput 返回本次执行拥有的恢复输入，不重新推断是否存在 input 字段。
func (r *OpenAIResult) WSReplayInput() ([]json.RawMessage, bool) {
	return r.wsReplayInput, r.wsReplayInputExists
}

// SetWSProviderFailoverReplayInput 保存跨提供商恢复收集器的既有结果。
func (r *OpenAIResult) SetWSProviderFailoverReplayInput(input []json.RawMessage) {
	r.wsProviderFailoverReplayInput = input
}

// WSProviderFailoverReplayInput 返回当前轮恢复所需的重放输入。
func (r *OpenAIResult) WSProviderFailoverReplayInput() []json.RawMessage {
	return r.wsProviderFailoverReplayInput
}
