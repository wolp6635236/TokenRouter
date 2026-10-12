package openaiattempt

import (
	"go.uber.org/zap"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// openAIPassthroughFailoverState 记录请求是否尝试过 OpenAI 透传提供商。
// 之后切换为非透传提供商时，清除上游私有的加密 reasoning。
type openAIPassthroughFailoverState struct {
	passthroughSeen bool
}

// deriveOpenAIForwardAttemptBody 从不可变的 canonical 请求体派生当前提供商的尝试请求体。
// 只有在已经尝试过透传提供商、且当前提供商不是透传提供商时，才删除带 encrypted_content 的
// reasoning 项；同类重试和之后的非透传提供商会持续使用清理后的派生体，canonical 本身不变。
func deriveOpenAIForwardAttemptBody(
	reqLog *zap.Logger,
	canonicalBody []byte,
	provider *gatewayprovider.ExecutionProvider,
	state *openAIPassthroughFailoverState,
) []byte {
	currentPassthrough := provider.View().IsOpenAIPassthroughEnabled()
	if currentPassthrough {
		state.passthroughSeen = true
		return canonicalBody
	}
	if !state.passthroughSeen {
		return canonicalBody
	}

	sanitized, changed, err := openai.SanitizeOpenAICrossModeFailoverReasoning(canonicalBody)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("openai.failover_cross_mode_reasoning_sanitize_failed",
				zap.Int64("provider_id", provider.Record.ID),
				zap.Error(err),
			)
		}
		return canonicalBody
	}
	if !changed {
		return canonicalBody
	}
	if reqLog != nil {
		reqLog.Info("openai.failover_cross_mode_reasoning_stripped",
			zap.Int64("provider_id", provider.Record.ID),
			zap.Bool("provider_passthrough", currentPassthrough),
			zap.Bool("passthrough_seen", true),
		)
	}
	return sanitized
}
