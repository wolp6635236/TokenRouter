package app

import (
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/selection"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// provideOpenAIResponseHealth 复用提供商健康、选号状态和延迟写入的生产实例。
func provideOpenAIResponseHealth(observer *provideradapter.UpstreamHealth, blocks *provider.RuntimeBlockState, models *provider.ModelTransientState, deferred *provider.DeferredService) *provideradapter.OpenAIResponseHealth {
	return &provideradapter.OpenAIResponseHealth{Health: observer, Runtime: blocks, ModelTransient: models, Deferred: deferred}
}

// provideOpenAIResponseOutput 为响应输出组件绑定静态参数和依赖接口。
func provideOpenAIResponseOutput(cfg *config.Config, health *provideradapter.OpenAIResponseHealth, grok *provideradapter.GrokHealth, observer *provideradapter.UpstreamHealth, headers *egress.CompiledHeaderFilter, turns *gatewayhttp.CodexTurnStateHeaders, circuit *egress.ProxyStreamCircuit, readers *gatewayadapter.RuntimeReaders, responses session.OpenAIWSStateStore, choices *selection.Compatible, history *session.ReasoningHistory, identity *gatewayadapter.ExecutionAgentIdentity) *gatewayhttp.OpenAIResponseOutput {
	output := &gatewayhttp.OpenAIResponseOutput{
		Reasoning: history, Redact: identity.Redact, Health: health, GrokHealth: grok, Observer: observer, Headers: headers, Turns: turns,
		Corrector: openai.NewCodexToolCorrector(), ProxyCircuit: circuit, Responses: responses,
		ResponseTTL: choices.OpenAIHTTPResponseStickyTTL,
		Options:     gatewayhttp.OpenAIResponseOptions{ReadLimit: config.DefaultUpstreamResponseReadMaxBytes},
	}
	if readers != nil {
		output.TTFT = readers.Gateway.GetOpenAITTFTMode
	}
	if cfg != nil {
		v := cfg.Gateway
		output.Options.Configured = true
		output.Options.MaxLineSize = v.MaxLineSize
		output.Options.StreamDataIntervalTimeout = v.StreamDataIntervalTimeout
		output.Options.StreamKeepaliveInterval = v.StreamKeepaliveInterval
		output.Options.ImageStreamDataIntervalTimeout = v.ImageStreamDataIntervalTimeout
		output.Options.ImageStreamKeepaliveInterval = v.ImageStreamKeepaliveInterval
		output.Options.OpenAIFirstOutputTimeoutSeconds = v.OpenAIFirstOutputTimeoutSeconds
		output.Options.OpenAIHighEffortFirstOutputTimeoutSeconds = v.OpenAIHighEffortFirstOutputTimeoutSeconds
		output.Options.LogUpstreamErrorBody = v.LogUpstreamErrorBody
		output.Options.LogUpstreamErrorBodyMaxBytes = v.LogUpstreamErrorBodyMaxBytes
		output.Options.ResponseHeadersEnabled = cfg.Security.ResponseHeaders.Enabled
		if v.UpstreamResponseReadMaxBytes > 0 {
			output.Options.ReadLimit = v.UpstreamResponseReadMaxBytes
		}
	}
	return output
}

// provideReasoningHistory 从已有缓存取得可选的推理历史读写接口。
func provideReasoningHistory(cache session.GatewayCache) *session.ReasoningHistory {
	store, _ := cache.(session.ReasoningContentCache)
	return &session.ReasoningHistory{Cache: store, Warn: gatewayadapter.WarnReasoningCacheFailure}
}

// provideOpenAIResponseState 构造由 app 管理的共享会话存储。
func provideOpenAIResponseState(cache session.GatewayCache) session.OpenAIWSStateStore {
	return session.NewOpenAIWSStateStore(cache, gatewayadapter.LogOpenAIWSModeInfo)
}

func provideResponseHeaderFilter(cfg *config.Config) *egress.CompiledHeaderFilter {
	if cfg == nil {
		return nil
	}
	return egress.CompileHeaderFilter(egress.ResponseHeaderOptions{Enabled: cfg.Security.ResponseHeaders.Enabled, AdditionalAllowed: cfg.Security.ResponseHeaders.AdditionalAllowed, ForceRemove: cfg.Security.ResponseHeaders.ForceRemove})
}
