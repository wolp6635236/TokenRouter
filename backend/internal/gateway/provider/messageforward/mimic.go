package messageforward

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

// mimicAttempt 的 systemRaw 接收 JSON 字符串或数组格式的系统提示。
type mimicAttempt struct {
	*attempt
	systemRaw any
}

// shouldNormalizeDateline 日期指纹只在 Anthropic OAuth/SetupToken 的开关开启时处理。
func (r *Runtime) shouldNormalizeDateline(ctx context.Context, target *gatewayadapter.ExecutionProvider) bool {
	return target != nil && target.View().IsAnthropicOAuthOrSetupToken() && r.dependencies.Settings != nil && r.dependencies.Settings.IsClientDatelineNormalizationEnabled(ctx)
}

func (r *Runtime) normalizeDateline(ctx context.Context, target *gatewayadapter.ExecutionProvider, body []byte) ([]byte, bool) {
	if !r.shouldNormalizeDateline(ctx, target) {
		return nil, false
	}
	next, _, changed := anthropic.NormalizeDateline(body)
	if !changed {
		return nil, false
	}
	return next, true
}

func (a *mimicAttempt) RewriteMimicSystem(body []byte, model, prompt, blocks string) []byte {
	blocks = anthropic.ClaudeOAuthSystemPromptBlocksForModel(model, blocks)
	return anthropic.RewriteSystemForNonClaudeCodeWithPromptBlocks(body, anthropic.NormalizeSystemParam(a.systemRaw), prompt, blocks)
}

func (a *mimicAttempt) MimicMetadata(ctx context.Context, body []byte) string {
	if a.s.dependencies.Fingerprint == nil || !a.c.RequestPresent() {
		return ""
	}
	fp, err := a.s.dependencies.Fingerprint.GetOrCreateFingerprint(ctx, a.provider.Record.ID, a.c.RequestHeaders())
	if err != nil || fp == nil {
		return ""
	}
	mimic := false
	if a.s.dependencies.Settings != nil {
		_, mimic, _ = a.s.dependencies.Settings.GetGatewayForwardingSettings(ctx)
	}
	if mimic {
		return ""
	}
	return metadataUserIDFromBody(ctx, a.provider, fp, body)
}

// mimic 处理兼容协议请求体，与当前转换 attempt 共用状态。
func (r *Runtime) mimic(ctx context.Context, output HTTPBoundary, state *AttemptState, target *gatewayadapter.ExecutionProvider, body []byte, system any, model string) []byte {
	a := newAttempt(r, output, target)
	a.state = state
	adapter := &mimicAttempt{attempt: a, systemRaw: system}
	return forward.Mimic(ctx, adapter, target != nil && target.View().IsOAuth(), body, model)
}
