package provider

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	protocolcore "github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

// AllowsCompatibleCompact 保留 Grok 与 OpenAI 的 Compact 资格差异。
func AllowsCompatibleCompact(provider *ExecutionProvider) bool {
	return provider != nil && (provider.View().IsGrok() || provider.View().AllowsOpenAICompact())
}

// CompatibleProviderEligible 判断 OpenAI 兼容提供商是否满足本次请求的调度条件。
// 检查内容包括：平台匹配、提供商可用性、quota 自动暂停、spark 路由限制、模型支持及端点能力。
//
// 注意：对 spark 影子提供商，调用方还须额外调用 parentHealthyForShadow(provider, lookup)
// 检查母提供商凭据可用性；该检查未内置于本函数，以避免注入 DB 依赖。
func CompatibleProviderEligible(ctx context.Context, provider *ExecutionProvider, platform string, requestedModel string, requireCompact bool, requiredCapability providercore.OpenAIEndpointCapability) bool {
	return CompatibleEligibilityReason(ctx, provider, platform, requestedModel, requireCompact, requiredCapability) == ""
}

// CompatibleEligibilityReason 返回兼容性判断结果和首个拦截原因。
// 负载批处理根据拒绝原因生成服务端的无可用提供商诊断。
// @project-doc docs/architecture/provider_scheduling_and_cache.md#advanced_scheduler_selection
func CompatibleEligibilityReason(ctx context.Context, provider *ExecutionProvider, platform string, requestedModel string, requireCompact bool, requiredCapability providercore.OpenAIEndpointCapability) string {
	platform = strings.TrimSpace(platform)
	if provider == nil {
		return "provider_nil"
	}
	if platform != "" && provider.Record.Platform != platform {
		return "platform_mismatch"
	}
	if !ExecutionModelPolicy(provider).Schedulable(ctx, requestedModel) {
		if provider.View().IsSchedulable() {
			return "model_rate_limited"
		}
		return "not_schedulable"
	}
	if provider.View().IsOpenAI() {
		if paused, reason := OpenAIQuotaPause(ctx, provider); paused {
			slog.Debug("provider_auto_paused_by_quota",
				"provider_id", provider.Record.ID,
				"window", reason.Window,
				"threshold", reason.Threshold,
				"utilization", reason.Utilization,
			)
			if reason.Window != "" {
				return "quota_auto_pause_" + reason.Window
			}
			return "quota_auto_pause"
		}
	}
	if provider.View().IsGrok() {
		if paused, reason := GrokQuotaPause(provider); paused {
			slog.Debug("grok_provider_auto_paused_by_quota",
				"provider_id", provider.Record.ID,
				"window", reason.Window,
				"threshold", reason.Threshold,
				"utilization", reason.Utilization,
			)
			if reason.Window != "" {
				return "quota_auto_pause_" + reason.Window
			}
			return "quota_auto_pause"
		}
	}
	if !ExecutionModelPolicy(provider).SupportsCompatibleRouting(ctx, requestedModel) {
		return "model_not_supported"
	}
	if !provider.View().IsSchedulable() || provider.View().IsQuotaExceeded() {
		return "provider_quota_exhausted"
	}
	if !SupportsRequestCapability(ctx, provider, requiredCapability) {
		if provider.View().IsGrok() && requiredCapability == providercore.OpenAIEndpointCapabilityGrokMediaGeneration {
			_, reason := providercore.GrokMediaGenerationEligibility(ExecutionRecord(provider), provideradapter.GrokTierRules())
			slog.Debug("grok_media_provider_ineligible", "provider_id", provider.Record.ID, "reason", reason)
		}
		return "capability_mismatch"
	}
	if requireCompact && !AllowsCompatibleCompact(provider) {
		return "compact_unsupported"
	}
	return ""
}

// OpenAIQuotaPause 使用当前时刻和本次请求阈值读取提供商派生状态。
func OpenAIQuotaPause(ctx context.Context, provider *ExecutionProvider) (bool, providercore.QuotaAutoPauseDecision) {
	return evaluateOpenAIQuotaPause(ctx, provider, time.Now())
}

// evaluateOpenAIQuotaPause 将提供商记录和请求阈值交给提供商模块判断自动暂停。
func evaluateOpenAIQuotaPause(ctx context.Context, v *ExecutionProvider, now time.Time) (bool, providercore.QuotaAutoPauseDecision) {
	if v == nil {
		return false, providercore.QuotaAutoPauseDecision{}
	}
	return providercore.EvaluateQuotaAutoPause(v.Record.Platform, v.Record.Extra, QuotaAutoPauseSettings(ctx), now)
}

// WithQuotaAutoPauseSettings 把 OpenAI 配额自动暂停全局设置放进 context，
// 让调度、展示和容量统计复用完全一致的阈值解析逻辑。
func WithQuotaAutoPauseSettings(ctx context.Context, settings providercore.QuotaAutoPauseSettings) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	hints := requeststate.ExecutionHintsFromContext(ctx)
	hints.QuotaAutoPauseThreshold5h = settings.DefaultThreshold5h
	hints.QuotaAutoPauseThreshold7d = settings.DefaultThreshold7d
	return requeststate.WithExecutionHints(ctx, hints)
}

// QuotaAutoPauseSettings 读取选择和固定提供商复核共享的请求快照。
func QuotaAutoPauseSettings(ctx context.Context) providercore.QuotaAutoPauseSettings {
	hints := requeststate.ExecutionHintsFromContext(ctx)
	return providercore.QuotaAutoPauseSettings{DefaultThreshold5h: hints.QuotaAutoPauseThreshold5h, DefaultThreshold7d: hints.QuotaAutoPauseThreshold7d}
}

// GrokQuotaPause 调用 provider 的 Grok 配额窗口规则。
func GrokQuotaPause(value *ExecutionProvider) (bool, providercore.QuotaAutoPauseDecision) {
	return providercore.EvaluateGrokQuotaAutoPause(ExecutionRecord(value), time.Now)
}

// SupportsRequestCapability 依次检查 WS、Compact 和普通 HTTP 的协议资格。
func SupportsRequestCapability(ctx context.Context, provider *ExecutionProvider, capability providercore.OpenAIEndpointCapability) bool {
	if provider == nil {
		return false
	}
	source, _ := requeststate.ClientProtocolFromContext(ctx)
	if !provider.View().IsOpenAICompatible() && (capability == "" || capability == providercore.OpenAIEndpointCapabilityTextGeneration || capability == providercore.OpenAIEndpointCapabilityResponses) {
		policy := ExecutionModelPolicy(provider)
		if source != "" {
			return policy.AllowsProtocol(ctx)
		}
		group, _ := requeststate.GroupFromContext(ctx)
		for _, entry := range []protocolcore.ProtocolID{protocolcore.ProtocolAnthropicMessages, protocolcore.ProtocolOpenAIResponses, protocolcore.ProtocolOpenAIChatCompletions} {
			if _, allowed := policy.ProtocolRoute(group, entry); allowed {
				return true
			}
		}
		return false
	}
	if source == protocolcore.ProtocolResponsesWebSocket || source == protocolcore.ProtocolResponsesCompact {
		if capability == providercore.OpenAIEndpointCapabilityTextGeneration || capability == providercore.OpenAIEndpointCapabilityResponses {
			return ExecutionModelPolicy(provider).AllowsProtocol(ctx)
		}
		if capability == providercore.OpenAIEndpointCapabilityRemoteCompactionV2 {
			return ExecutionModelPolicy(provider).AllowsProtocol(ctx) && provider.View().AllowsOpenAINativeCompactionV2()
		}
	}
	return provideradapter.SupportsOpenAIEndpoint(ExecutionProtocolRecord(provider), capability)
}
