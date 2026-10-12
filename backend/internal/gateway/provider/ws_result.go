package provider

import (
	"net/http"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	openaiws "github.com/TokenFlux/TokenRouter/internal/upstream/openai/ws"
)

// ProjectWSResult 整理本次 WS turn 的观测结果和恢复输入。
func ProjectWSResult(r *forwardcore.OpenAIResult) *gatewayws.ForwardResult {
	if r == nil {
		return nil
	}
	replay, replayExists := r.WSReplayInput()
	out := &gatewayws.ForwardResult{
		LocalWarmup:                 r.LocalWarmup,
		RequestID:                   r.RequestID,
		ResponseID:                  r.ResponseID,
		UpstreamHeaders:             r.UpstreamHeaders,
		Usage:                       r.Usage,
		Model:                       r.Model,
		BillingModel:                r.BillingModel,
		UpstreamModel:               r.UpstreamModel,
		UpstreamResponseServiceTier: r.UpstreamResponseServiceTier,
		UpstreamResponseModel:       r.UpstreamResponseModel,
		UpstreamEndpoint:            r.UpstreamEndpoint,
		ServiceTier:                 r.ServiceTier,
		ReasoningEffort:             r.ReasoningEffort,
		RequestedReasoningEffort:    r.RequestedReasoningEffort,
		Stream:                      r.Stream,
		OpenAIWSMode:                r.OpenAIWSMode,
		UpstreamTerminalEvent:       r.UpstreamTerminalEvent,
		ResponseHeaders:             r.ResponseHeaders,
		Duration:                    r.Duration,
		FirstTokenMs:                r.FirstTokenMs,
		ClientDisconnect:            r.ClientDisconnect,
		ImageCount:                  r.ImageCount,
		ImageSize:                   r.ImageSize,
		ImageInputSize:              r.ImageInputSize,
		ImageOutputSize:             r.ImageOutputSize,
		ImageOutputSizes:            r.ImageOutputSizes,
		ImageSizeSource:             r.ImageSizeSource,
		ImageSizeBreakdown:          r.ImageSizeBreakdown,
		VideoCount:                  r.VideoCount,
		VideoResolution:             r.VideoResolution,
		VideoDurationSeconds:        r.VideoDurationSeconds,
		WebSearchCalls:              r.WebSearchCalls,
		SearchCount:                 r.SearchCount,
		AudioUsage:                  r.AudioUsage,
		WSReplayInput:               replay, WSReplayInputExists: replayExists, WSProviderFailoverReplayInput: r.WSProviderFailoverReplayInput(),
		ResponseTurnState: http.Header(r.ResponseHeaders).Get(openaiws.WSTurnStateHeader),
	}
	if r.UpstreamWarning != nil {
		out.UpstreamWarning = &forwardcore.UpstreamWarning{StatusCode: r.UpstreamWarning.StatusCode, ResponseBody: r.UpstreamWarning.ResponseBody, Message: r.UpstreamWarning.Message}
	}
	return out
}

// ForwardResultFromWS 将 WS 结果转换为完成记录和健康观测使用的 HTTP 结果格式。
func ForwardResultFromWS(r *gatewayws.ForwardResult) *forwardcore.OpenAIResult {
	if r == nil {
		return nil
	}
	out := &forwardcore.OpenAIResult{
		LocalWarmup:                 r.LocalWarmup,
		RequestID:                   r.RequestID,
		ResponseID:                  r.ResponseID,
		UpstreamHeaders:             r.UpstreamHeaders,
		Usage:                       r.Usage,
		Model:                       r.Model,
		BillingModel:                r.BillingModel,
		UpstreamModel:               r.UpstreamModel,
		UpstreamResponseServiceTier: r.UpstreamResponseServiceTier,
		UpstreamResponseModel:       r.UpstreamResponseModel,
		UpstreamEndpoint:            r.UpstreamEndpoint,
		ServiceTier:                 r.ServiceTier,
		ReasoningEffort:             r.ReasoningEffort,
		RequestedReasoningEffort:    r.RequestedReasoningEffort,
		Stream:                      r.Stream,
		OpenAIWSMode:                r.OpenAIWSMode,
		UpstreamTerminalEvent:       r.UpstreamTerminalEvent,
		ResponseHeaders:             r.ResponseHeaders,
		Duration:                    r.Duration,
		FirstTokenMs:                r.FirstTokenMs,
		ClientDisconnect:            r.ClientDisconnect,
		ImageCount:                  r.ImageCount,
		ImageSize:                   r.ImageSize,
		ImageInputSize:              r.ImageInputSize,
		ImageOutputSize:             r.ImageOutputSize,
		ImageOutputSizes:            r.ImageOutputSizes,
		ImageSizeSource:             r.ImageSizeSource,
		ImageSizeBreakdown:          r.ImageSizeBreakdown,
		VideoCount:                  r.VideoCount,
		VideoResolution:             r.VideoResolution,
		VideoDurationSeconds:        r.VideoDurationSeconds,
		WebSearchCalls:              r.WebSearchCalls,
		SearchCount:                 r.SearchCount,
		AudioUsage:                  r.AudioUsage,
	}
	out.SetWSReplayInput(r.WSReplayInput, r.WSReplayInputExists)
	out.SetWSProviderFailoverReplayInput(r.WSProviderFailoverReplayInput)
	if r.UpstreamWarning != nil {
		out.UpstreamWarning = &forwardcore.UpstreamWarning{StatusCode: r.UpstreamWarning.StatusCode, ResponseBody: r.UpstreamWarning.ResponseBody, Message: r.UpstreamWarning.Message}
	}
	return out
}
