package routing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// Retained group plans isolate client policy from later administrative edits
// while provider attempts use current snapshots. These cases check copied
// protocol settings and fixed model-stage values, then compare an attempt's
// one-hop mapping with a freshly captured provider mapping.

// TestRoutePlanCandidateRecalculationAndIsolation mutates group conversion
// settings and a returned admission slice after planning. The existing plan
// must retain its route, while a newly built plan observes the updated
// conversion target.
func TestRoutePlanCandidateRecalculationAndIsolation(t *testing.T) {
	group := &Group{
		ID: 7, SchedulerType: GroupSchedulerTypeAdvanced,
		AllowedProtocols:  []capability.ProtocolID{capability.ProtocolAnthropicMessages},
		ProtocolFallbacks: map[capability.ProtocolID][]capability.ProtocolID{capability.ProtocolAnthropicMessages: {capability.ProtocolOpenAIResponses}},
	}
	plan := Plan(PlanInput{Group: group, ClientProtocol: capability.ProtocolAnthropicMessages})
	responses := provider.ProviderSnapshot{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, EnabledProtocols: []capability.ProtocolID{capability.ProtocolOpenAIResponses}}
	chat := provider.ProviderSnapshot{ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, EnabledProtocols: []capability.ProtocolID{capability.ProtocolOpenAIChatCompletions}}
	first, ok := plan.ResolveCandidate(responses)
	require.True(t, ok)
	require.Equal(t, int64(1), first.ProviderID)
	require.Equal(t, capability.ProtocolOpenAIResponses, first.UpstreamProtocol)
	_, ok = plan.ResolveCandidate(chat)
	require.False(t, ok)
	group.ProtocolFallbacks[capability.ProtocolAnthropicMessages][0] = capability.ProtocolOpenAIChatCompletions
	plan.AllowedProtocols()[0] = capability.ProtocolLive
	again, ok := plan.ResolveCandidate(responses)
	require.True(t, ok)
	require.Equal(t, first, again)
	fresh := Plan(PlanInput{Group: group, ClientProtocol: capability.ProtocolAnthropicMessages})
	second, ok := fresh.ResolveCandidate(chat)
	require.True(t, ok)
	require.Equal(t, int64(2), second.ProviderID)
	require.Equal(t, capability.ProtocolOpenAIChatCompletions, second.UpstreamProtocol)
	require.Equal(t, GroupSchedulerTypeAdvanced, plan.SchedulerType())
	require.Equal(t, int64(7), plan.GroupID())
	require.Equal(t, []capability.ProtocolID{capability.ProtocolAnthropicMessages}, plan.AllowedProtocols())
}

// TestRoutePlanModelChainAndAttemptSnapshots changes input group IDs and model
// mappings after snapshot creation. Retained plan identities must stay fixed,
// and each candidate copy must use one provider mapping from its supplied
// snapshot.
func TestRoutePlanModelChainAndAttemptSnapshots(t *testing.T) {
	groupID := int64(7)
	mapping := GroupMappingResult{MappedModel: "group-model", PricingConfigID: 9, Mapped: true, BillingModelSource: "requested", RestrictModels: true, RestrictionModelSource: BillingModelSourceUpstream, ClientModel: "prefix/client-model", APIKeyRedirected: true}
	plan := Plan(PlanInput{GroupID: &groupID, RequestedModel: "key-model", GroupMapping: mapping, ClientProtocol: capability.ProtocolOpenAIResponses})
	groupID = 8
	require.Equal(t, int64(7), plan.GroupID())
	require.Equal(t, mapping, plan.Mapping())
	require.Equal(t, "prefix/client-model", plan.Models().ClientModel)
	require.Equal(t, "key-model", plan.Models().RequestedModel)
	rules := map[string]string{"group-model": "upstream-one", "upstream-one": "must-not-recurse"}
	snapshot := provider.ProviderSnapshot{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, EnabledProtocols: []capability.ProtocolID{capability.ProtocolOpenAIResponses}, ModelPolicy: provider.NewModelRoutingSnapshot(rules)}
	rules["group-model"] = "upstream-two"
	first, ok := plan.ResolveCandidate(snapshot)
	require.True(t, ok)
	require.True(t, first.ModelMappingMatched)
	require.Equal(t, "upstream-one", first.Models.ProviderMappedModel)
	require.True(t, first.Models.RestrictModels)
	require.Equal(t, BillingModelSourceUpstream, first.Models.RestrictionModelSource)
	require.Empty(t, plan.Models().ProviderMappedModel)
	snapshot.ModelPolicy = provider.NewModelRoutingSnapshot(rules)
	second, ok := plan.ResolveCandidate(snapshot)
	require.True(t, ok)
	require.True(t, second.ModelMappingMatched)
	require.Equal(t, "upstream-two", second.Models.ProviderMappedModel)
}

// TestOpenCodeCandidateResolvesMappedModelProtocol supplies aliases and distinct
// native encodings for one model across service modes. Unknown models, disabled
// targets, and prohibited conversions cannot enter the candidate pool.
func TestOpenCodeCandidateResolvesMappedModelProtocol(t *testing.T) {
	record := &provider.Record{ID: 7, Platform: capability.PlatformOpenCode, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "test", "model_mapping": map[string]any{"alias": "minimax-m3", "minimax-m3": "unknown-second-hop"}}}
	plan := Plan(PlanInput{RequestedModel: "alias", ClientProtocol: capability.ProtocolOpenAIChatCompletions})
	candidate, ok := plan.ResolveCandidate(record.RoutingSnapshot())
	require.True(t, ok)
	require.Equal(t, capability.ProtocolOpenAIChatCompletions, candidate.UpstreamProtocol)
	require.Equal(t, "minimax-m3", candidate.Models.ProviderMappedModel)
	record.Credentials["provider_mode"] = provider.ProviderModeGo
	candidate, ok = plan.ResolveCandidate(record.RoutingSnapshot())
	require.True(t, ok)
	require.Equal(t, capability.ProtocolAnthropicMessages, candidate.UpstreamProtocol)
	blocked := Plan(PlanInput{RequestedModel: "alias", ClientProtocol: capability.ProtocolOpenAIChatCompletions, Group: &Group{ProtocolFallbacks: map[capability.ProtocolID][]capability.ProtocolID{capability.ProtocolOpenAIChatCompletions: {}}}})
	_, ok = blocked.ResolveCandidate(record.RoutingSnapshot())
	require.False(t, ok)
	record.Credentials[provider.UpstreamProtocolsKey] = []string{}
	_, ok = plan.ResolveCandidate(record.RoutingSnapshot())
	require.False(t, ok)
	record.Credentials[provider.UpstreamProtocolsKey] = []string{string(capability.ProtocolAnthropicMessages)}
	unknown := Plan(PlanInput{RequestedModel: "unknown", ClientProtocol: capability.ProtocolAnthropicMessages})
	_, ok = unknown.ResolveCandidate(record.RoutingSnapshot())
	require.False(t, ok)
}
