package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// OpenAIStreamDataStartsTTFT 按管理员设置选择事件类型或可见内容作为 TTFT 起点。
func OpenAIStreamDataStartsTTFT(data, eventType string, forceOutput bool, mode string) bool {
	if gateway.NormalizeOpenAITTFTMode(mode) == gateway.OpenAITTFTModeVisible {
		return protocolopenai.StreamDataStartsVisibleOutput(data, eventType)
	}
	return forceOutput || openai.OpenAIStreamDataStartsSemanticTTFT(data, eventType)
}
