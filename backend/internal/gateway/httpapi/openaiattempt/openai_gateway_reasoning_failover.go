package openaiattempt

import (
	"go.uber.org/zap"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// openAIFailoverReasoningState 记录本次请求第一个尝试的提供商。
// 客户端回带的加密 reasoning 通常由这个提供商背后的上游账号签发，换到其他提供商后无法验证。
type openAIFailoverReasoningState struct {
	firstProviderID int64
	started         bool
}

// deriveOpenAIForwardAttemptBody 从不可变的 canonical 请求体派生当前提供商的尝试请求体。
// 第一个提供商上的尝试（含同提供商重试）使用 canonical 原文。换到其他提供商后，
// 删除带 encrypted_content 的 reasoning 项，canonical 本身不变。
// 同一中转站的两个提供商也按不同提供商处理，代价是这一轮看不到之前的推理过程。
func deriveOpenAIForwardAttemptBody(
	reqLog *zap.Logger,
	canonicalBody []byte,
	provider *gatewayprovider.ExecutionProvider,
	state *openAIFailoverReasoningState,
) []byte {
	providerID := provider.Record.ID
	if !state.started {
		state.started = true
		state.firstProviderID = providerID
	}
	if providerID == state.firstProviderID {
		return canonicalBody
	}

	sanitized, changed, err := openai.SanitizeOpenAICrossModeFailoverReasoning(canonicalBody)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("openai.failover_cross_provider_reasoning_sanitize_failed",
				zap.Int64("provider_id", providerID),
				zap.Error(err),
			)
		}
		return canonicalBody
	}
	if !changed {
		return canonicalBody
	}
	if reqLog != nil {
		reqLog.Info("openai.failover_cross_provider_reasoning_stripped",
			zap.Int64("provider_id", providerID),
			zap.Int64("first_provider_id", state.firstProviderID),
		)
	}
	return sanitized
}
