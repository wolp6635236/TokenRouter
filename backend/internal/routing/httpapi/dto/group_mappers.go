package dto

import (
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

func GroupFromRoutingBase(g *routing.Group) Group {
	return Group{
		Models: append([]string{}, g.Models...), ModelProtocols: g.ModelProtocols,
		ID:                              g.ID,
		Name:                            g.Name,
		DisplayName:                     g.DisplayName,
		Description:                     g.Description,
		DisplayBrand:                    g.DisplayBrand,
		RateMultiplier:                  g.RateMultiplier,
		IsExclusive:                     g.IsExclusive,
		Status:                          g.Status,
		SessionIsolationEnabled:         g.SessionIsolationEnabled,
		AllowImageGeneration:            g.AllowImageGeneration,
		AllowBatchImageGeneration:       g.AllowBatchImageGeneration,
		ClaudeCodeOnly:                  g.ClaudeCodeOnly,
		FallbackGroupID:                 g.FallbackGroupID,
		FallbackGroupIDOnInvalidRequest: g.FallbackGroupIDOnInvalidRequest,
		UnavailableFallbackGroupID:      g.UnavailableFallbackGroupID,
		AllowedProtocols:                g.EffectiveAllowedProtocols(),
		ProtocolFallbacks:               g.ProtocolFallbacks,
		ResponsesImagePolicy:            g.ResponsesImagePolicy,
		AllowMessagesDispatch:           g.AllowsClientProtocol(protocol.ProtocolAnthropicMessages),
		AllowLive:                       g.AllowLive,
		RequireOAuthOnly:                g.RequireOAuthOnly,
		RequirePrivacySet:               g.RequirePrivacySet,
		RPMLimit:                        g.RPMLimit,
		MaxReasoningEffort:              g.MaxReasoningEffort,
		MaxReasoningEffortOverLimit:     g.MaxReasoningEffortOverLimit,
		ReasoningEffortMappings:         g.ReasoningEffortMappings,
		CreatedAt:                       g.CreatedAt,
		UpdatedAt:                       g.UpdatedAt,
	}
}

// AdminGroupFromRouting 将分组转换为管理员 DTO，包含 model_routing 和 provider_count 等内部字段。
func AdminGroupFromRouting[A any](g *routing.Group) *AdminGroup[A] {
	if g == nil {
		return nil
	}
	out := &AdminGroup[A]{
		Localization:               routing.GroupLocalization(routing.GroupContent(g)),
		Group:                      GroupFromRoutingBase(g),
		ForceOpenAIFast:            g.ForceOpenAIFast,
		OpenAIFastPolicy:           g.EffectiveOpenAIFastPolicy(),
		SchedulerType:              string(g.SchedulerType),
		AdvancedSchedulerOverrides: policy.CloneGroupAdvancedSchedulerOverrides(g.AdvancedSchedulerOverrides),
		RoutingPolicy:              g.RoutingPolicy.Clone(),
		ModelRouting:               g.ModelRouting,
		ModelRoutingEnabled:        g.ModelRoutingEnabled,
		MCPXMLInject:               g.MCPXMLInject,
		DefaultMappedModel:         g.DefaultMappedModel,
		ModelsListConfig:           g.ModelsListConfig,
		AvailabilityProbeConfig:    g.AvailabilityProbeConfig,
		SupportedModelScopes:       g.SupportedModelScopes,
		ProviderCount:              g.ProviderCount,
		ActiveProviderCount:        g.ActiveProviderCount,
		RateLimitedProviderCount:   g.RateLimitedProviderCount,
		SortOrder:                  g.SortOrder,
	}

	return out
}

// GroupFromRouting 将分组转换为用户或管理员的 DTO 字段。
func GroupFromRouting(g *routing.Group, language ...string) *Group {
	if g == nil {
		return nil
	}
	out := GroupFromRoutingBase(g)
	if len(language) > 0 {
		copy, actual := routing.GroupDisplay(g, language[0])
		out.LocalizationResolution = &actual
		out.SearchTerms = routing.GroupSearchTexts(g)
		out.DisplayName = copy.DisplayName
		out.Description = copy.Description
	}
	return &out
}

// GroupCapacityFromSummary 将容量摘要转换为响应字段，nil 返回 nil，零值保持为零。
func GroupCapacityFromSummary(v *routing.GroupCapacitySummary) *GroupCapacity {
	if v == nil {
		return nil
	}
	return &GroupCapacity{ConcurrencyUsed: v.ConcurrencyUsed, ConcurrencyMax: v.ConcurrencyMax, SessionsUsed: v.SessionsUsed, SessionsMax: v.SessionsMax, RPMUsed: v.RPMUsed, RPMMax: v.RPMMax}
}
