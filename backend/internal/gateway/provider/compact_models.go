package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/gateway/compact"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// CompactModels 保存压缩回退配置，按需读取提供商映射和默认模型。
type CompactModels struct{ Default string }

type compactAttemptModels struct {
	defaults CompactModels
	target   *ExecutionProvider
}

func (p compactAttemptModels) ProviderModel(model string) (string, bool) {
	if p.target == nil {
		return "", false
	}
	return p.target.View().ResolveCompactMappedModel(model)
}
func (p compactAttemptModels) GlobalModel() string { return p.defaults.Default }
func (p compactAttemptModels) ResolveGlobalModel(model string) string {
	return ExecutionModelPolicy(p.target).OpenAIUpstream(model, false)
}

func (p CompactModels) Recovery(target *ExecutionProvider) compact.Recovery {
	return compact.Recovery{Models: compactAttemptModels{defaults: p, target: target}, ContextWindow: openai.IsOpenAIContextWindowError, RewriteModel: protocolopenai.ReplaceModelInBody}
}
