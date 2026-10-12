package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/modeltrace"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	protocolcore "github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestGroupMappingChainIncludesAPIKeyRedirectAndDeduplicatesStages 验证模型映射链包含 Key 重定向，并去除重复阶段。
func TestGroupMappingChainIncludesAPIKeyRedirectAndDeduplicatesStages(t *testing.T) {
	ctx := modeltrace.WithContext(
		context.Background(),
		modeltrace.NewAPIKeyModelRedirectTrace("codex-auto-review", "codex-auto-review", "gpt-5.6-luna"),
	)
	mapping := modeltrace.WithGroupRedirect(routing.GroupMappingResult{
		MappedModel:        "gpt-5.6-luna-channel",
		Mapped:             true,
		BillingModelSource: routing.BillingModelSourceGroupMapped,
	}, ctx, "gpt-5.6-luna")

	fields := mapping.ToUsageFields("gpt-5.6-luna", "gpt-5.6-luna-upstream")
	require.Equal(t, "gpt-5.6-luna", fields.OriginalModel)
	require.Equal(t, "gpt-5.6-luna-channel", fields.GroupMappedModel)
	require.Equal(t, "codex-auto-review→gpt-5.6-luna→gpt-5.6-luna-channel→gpt-5.6-luna-upstream", fields.ModelMappingChain)

	// 映射链里重复出现的模型只记录第一次。
	require.Equal(t, "codex-auto-review→gpt-5.6-luna→gpt-5.6-luna-channel", mapping.BuildModelMappingChain("gpt-5.6-luna", "codex-auto-review"))
	require.Equal(t, []string{"gpt-5.6-luna", "gpt-5.6-luna-channel"}, mustAPIKeyResponseModels(t, ctx))
}

// TestRoutePlannerWithoutGroupDoesNotReadConfiguration 检查未绑定分组时是否返回请求模型，存储读取次数为零。
func TestRoutePlannerWithoutGroupDoesNotReadConfiguration(t *testing.T) {
	planner := NewRoutePlanner(routing.NewPricingConfigService(nil, nil))
	plan := planner.PlanRoute(context.Background(), nil, nil, "request-model")
	require.Equal(t, routing.GroupMappingResult{MappedModel: "request-model"}, plan.Mapping())
	require.Zero(t, plan.GroupID())
}

func TestProtocolRouteNativeFirstAndExplicitFallback(t *testing.T) {
	provider := &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{providercore.UpstreamProtocolsKey: []string{"anthropic_messages", "openai_responses"}, "api_base_urls": map[string]any{"anthropic": "https://relay.example/messages", "responses": "https://relay.example/responses"}}}}
	group := &routing.Group{AllowedProtocols: []protocolcore.ProtocolID{protocolcore.ProtocolAnthropicMessages}, ProtocolFallbacks: map[protocolcore.ProtocolID][]protocolcore.ProtocolID{protocolcore.ProtocolAnthropicMessages: {protocolcore.ProtocolOpenAIResponses}}}
	ctx := requeststate.WithClientProtocol(requeststate.WithGroup(context.Background(), group), protocolcore.ProtocolAnthropicMessages)
	selected, err := ProviderForProtocolAttempt(ctx, provider)
	require.NoError(t, err)
	require.Equal(t, providercore.APIProtocolAnthropic, ExecutionProtocolTarget(selected).GetAPIProtocol())
	require.Empty(t, provider.Route.Protocol())
	require.Equal(t, "https://relay.example/messages", ExecutionProtocolTarget(selected).GetAnthropicProtocolBaseURL())
	provider.Record.Credentials[providercore.UpstreamProtocolsKey] = []string{"openai_responses"}
	selected, err = ProviderForProtocolAttempt(ctx, provider)
	require.NoError(t, err)
	require.Equal(t, providercore.APIProtocolResponses, ExecutionProtocolTarget(selected).GetAPIProtocol())
	require.Equal(t, "https://relay.example/responses", ExecutionProtocolTarget(selected).GetCNProtocolBaseURL(providercore.APIProtocolResponses))
	// 转换目标无需向客户端开放；下一次切号重新使用该候选的集合。
	require.False(t, group.AllowsClientProtocol(protocolcore.ProtocolOpenAIResponses))
	next := *provider
	next.Record.Credentials = map[string]any{providercore.UpstreamProtocolsKey: []string{"openai_chat_completions"}}
	require.False(t, ExecutionModelPolicy(&next).AllowsProtocol(ctx))
	group.ProtocolFallbacks[protocolcore.ProtocolAnthropicMessages] = []protocolcore.ProtocolID{}
	ctx = requeststate.WithGroup(ctx, group)
	require.False(t, ExecutionModelPolicy(provider).AllowsProtocol(ctx))
}

// TestRoutePlanRebuildsCandidateAndKeepsModelReadTiming 验证当前请求计划不能污染共享提供商；模型读取仍按每次原匹配时机获得最新配置。
func TestRoutePlanRebuildsCandidateAndKeepsModelReadTiming(t *testing.T) {
	group := &routing.Group{ID: 7, AllowedProtocols: []protocolcore.ProtocolID{protocolcore.ProtocolAnthropicMessages}, ProtocolFallbacks: map[protocolcore.ProtocolID][]protocolcore.ProtocolID{protocolcore.ProtocolAnthropicMessages: {protocolcore.ProtocolOpenAIResponses}}}
	ctx := requeststate.WithClientProtocol(requeststate.WithGroup(context.Background(), group), protocolcore.ProtocolAnthropicMessages)
	mapping := routing.GroupMappingResult{MappedModel: "group-model", Mapped: true, PricingConfigID: 9, BillingModelSource: "requested"}
	plan := RoutePlanForMapping(ctx, group, &group.ID, "key-model", mapping)
	ctx = requeststate.WithRoutePlan(ctx, plan)
	shared := &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{providercore.UpstreamProtocolsKey: []string{"openai_responses"}, "model_mapping": map[string]any{"group-model": "first"}}}}
	first, err := ProviderForProtocolAttempt(ctx, shared)
	require.NoError(t, err)
	require.NotSame(t, shared, first)
	_, captured := shared.Route.Candidate()
	require.False(t, captured)
	_, captured = first.Route.Candidate()
	require.True(t, captured)
	require.Equal(t, "first", ExecutionModelPolicy(first).Mapped("group-model"))
	first.Record.Credentials = map[string]any{providercore.UpstreamProtocolsKey: []string{"openai_responses"}, "model_mapping": map[string]any{"group-model": "latest"}}
	require.Equal(t, "latest", ExecutionModelPolicy(first).Mapped("group-model"))
	require.Equal(t, "first", ExecutionModelPolicy(shared).Mapped("group-model"))
	first.Record.Credentials = map[string]any{providercore.UpstreamProtocolsKey: []string{"openai_chat_completions"}}
	_, err = ProviderForProtocolAttempt(ctx, first)
	require.Error(t, err, "已有尝试副本也须复核 fresh 能力")
}
