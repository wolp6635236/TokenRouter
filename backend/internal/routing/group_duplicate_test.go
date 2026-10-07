package routing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
	"github.com/stretchr/testify/require"
)

type duplicateGroupRepoStub struct {
	routing.GroupRepository

	nextID             int64
	groups             map[int64]*routing.Group
	names              map[string]struct{}
	byOperation        map[string]int64
	sourceBindings     map[int64][]providercore.GroupMembership
	createdBindings    map[int64][]providercore.GroupMembership
	createdFromSources []int64
	atomicCreateErr    error
}

func newDuplicateGroupRepoStub(source *routing.Group) *duplicateGroupRepoStub {
	repo := &duplicateGroupRepoStub{
		nextID:          100,
		groups:          make(map[int64]*routing.Group),
		names:           make(map[string]struct{}),
		byOperation:     make(map[string]int64),
		sourceBindings:  make(map[int64][]providercore.GroupMembership),
		createdBindings: make(map[int64][]providercore.GroupMembership),
	}
	if source != nil {
		repo.groups[source.ID] = source
		repo.names[source.Name] = struct{}{}
	}
	return repo
}

func cloneGroupForDuplicateTest(group *routing.Group) *routing.Group {
	if group == nil {
		return nil
	}
	cloned := *group
	cloned.FallbackGroupID = routing.CloneGroupValuePointer(group.FallbackGroupID)
	cloned.FallbackGroupIDOnInvalidRequest = routing.CloneGroupValuePointer(group.FallbackGroupIDOnInvalidRequest)
	cloned.UnavailableFallbackGroupID = routing.CloneGroupValuePointer(group.UnavailableFallbackGroupID)
	cloned.AdvancedSchedulerOverrides = accessview.CloneGroupAdvancedSchedulerOverrides(group.AdvancedSchedulerOverrides)
	cloned.ModelRouting = routing.CloneGroupModelRouting(group.ModelRouting)
	cloned.SupportedModelScopes = append([]string(nil), group.SupportedModelScopes...)
	cloned.AllowedProtocols = routing.CloneGroupClientProtocols(group.AllowedProtocols)
	cloned.ModelsListConfig.Models = append([]string(nil), group.ModelsListConfig.Models...)
	return &cloned
}

func (r *duplicateGroupRepoStub) GetByID(_ context.Context, id int64) (*routing.Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, routing.ErrGroupNotFound
	}
	cloned := cloneGroupForDuplicateTest(group)
	cloned.Hydrated = true
	return cloned, nil
}

func (r *duplicateGroupRepoStub) FindByDuplicateOperationID(_ context.Context, operationID string) (*routing.Group, error) {
	id := r.byOperation[operationID]
	if id == 0 {
		return nil, nil
	}
	return cloneGroupForDuplicateTest(r.groups[id]), nil
}

func (r *duplicateGroupRepoStub) CreateFromSource(_ context.Context, group *routing.Group, sourceGroupID int64) error {
	if r.atomicCreateErr != nil {
		return r.atomicCreateErr
	}
	if group.DuplicateOperationID != "" {
		if _, exists := r.byOperation[group.DuplicateOperationID]; exists {
			return routing.ErrGroupExists
		}
	}
	if _, exists := r.names[group.Name]; exists {
		return routing.ErrGroupExists
	}
	r.nextID++
	group.ID = r.nextID
	group.CreatedAt = time.Now().UTC()
	group.UpdatedAt = group.CreatedAt
	bindings := append([]providercore.GroupMembership(nil), r.sourceBindings[sourceGroupID]...)
	for i := range bindings {
		bindings[i].GroupID = group.ID
	}
	group.ProviderCount = int64(len(bindings))
	group.ActiveProviderCount = int64(len(bindings))
	r.createdBindings[group.ID] = bindings
	r.createdFromSources = append(r.createdFromSources, sourceGroupID)
	r.names[group.Name] = struct{}{}
	r.groups[group.ID] = cloneGroupForDuplicateTest(group)
	if group.DuplicateOperationID != "" {
		r.byOperation[group.DuplicateOperationID] = group.ID
	}
	return nil
}

func groupDuplicateTestPointer[T any](value T) *T { return &value }

func TestDuplicateGroupCopiesConfigurationDeeplyAndResetsRuntimeState(t *testing.T) {
	createdAt := time.Date(2026, time.July, 1, 2, 3, 4, 0, time.UTC)
	source := &routing.Group{
		ID:            41,
		Name:          "高级订阅",
		Description:   "configuration",
		SchedulerType: routing.GroupSchedulerTypeAdvanced,
		AdvancedSchedulerOverrides: routing.GroupAdvancedSchedulerOverrides{
			StickyWeightedEnabled: groupDuplicateTestPointer(true),
			LBTopK:                groupDuplicateTestPointer(3),
			WeightPriority:        groupDuplicateTestPointer(4.5),
		},
		DisplayBrand:                    "OpenAI",
		RateMultiplier:                  1.75,
		IsExclusive:                     true,
		Status:                          billing.StatusActive,
		Hydrated:                        true,
		SessionIsolationEnabled:         true,
		AllowImageGeneration:            true,
		AllowBatchImageGeneration:       true,
		ClaudeCodeOnly:                  true,
		FallbackGroupID:                 groupDuplicateTestPointer(int64(7)),
		FallbackGroupIDOnInvalidRequest: groupDuplicateTestPointer(int64(8)),
		UnavailableFallbackGroupID:      groupDuplicateTestPointer(int64(9)),
		ModelRouting:                    map[string][]int64{"gpt-*": {13, 17}},
		ModelRoutingEnabled:             true,
		MCPXMLInject:                    true,
		SupportedModelScopes:            []string{"claude", "gemini_text"},
		SortOrder:                       9,
		AllowedProtocols: []protocol.ProtocolID{
			protocol.ProtocolAnthropicMessages,
			protocol.ProtocolOpenAIResponses,
			protocol.ProtocolOpenAIChatCompletions,
		},
		AllowMessagesDispatch:       true,
		AllowLive:                   true,
		ForceOpenAIFast:             true,
		RequireOAuthOnly:            true,
		RequirePrivacySet:           true,
		DefaultMappedModel:          "gpt-5.4",
		ModelsListConfig:            routing.GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5.4", "gpt-5-mini"}},
		AvailabilityProbeConfig:     routing.GroupAvailabilityProbeConfig{Enabled: true, ModelID: "gpt-5.4", Prompt: "ping", TimeoutSeconds: 15},
		RPMLimit:                    99,
		MaxReasoningEffort:          "medium",
		MaxReasoningEffortOverLimit: routing.ReasoningEffortOverLimitDeny,
		ReasoningEffortMappings:     []routing.ReasoningEffortMapping{{From: "max", To: "xhigh"}},
		CreatedAt:                   createdAt,
		UpdatedAt:                   createdAt,
		ProviderCount:               12,
		ActiveProviderCount:         8,
		RateLimitedProviderCount:    2,
		DuplicateOperationID:        "old-operation-must-not-copy",
	}
	repo := newDuplicateGroupRepoStub(source)
	repo.sourceBindings[source.ID] = []providercore.GroupMembership{
		{ProviderID: 13, GroupID: source.ID},
		{ProviderID: 17, GroupID: source.ID},
	}
	svc := newGroupAdminForTest(repo, repo, nil)

	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:7", "stable-key")

	require.NoError(t, err)
	require.NotEqual(t, source.ID, duplicate.ID)
	require.Equal(t, "高级订阅 (Copy)", duplicate.Name)
	require.Equal(t, routing.DuplicateGroupInactiveStatus, duplicate.Status)
	require.True(t, duplicate.Hydrated, "the duplicate response is reloaded with derived counts")
	require.Equal(t, source.Description, duplicate.Description)
	require.Equal(t, routing.GroupSchedulerTypeAdvanced, duplicate.SchedulerType)
	require.Equal(t, 3, *duplicate.AdvancedSchedulerOverrides.LBTopK)
	require.NotSame(t, source.AdvancedSchedulerOverrides.LBTopK, duplicate.AdvancedSchedulerOverrides.LBTopK)
	require.Equal(t, source.DisplayBrand, duplicate.DisplayBrand)
	require.Equal(t, source.SessionIsolationEnabled, duplicate.SessionIsolationEnabled)
	require.Equal(t, source.RateMultiplier, duplicate.RateMultiplier)
	require.Equal(t, source.ForceOpenAIFast, duplicate.ForceOpenAIFast)
	require.Equal(t, source.FallbackGroupID, duplicate.FallbackGroupID)
	require.Equal(t, source.UnavailableFallbackGroupID, duplicate.UnavailableFallbackGroupID)
	require.Equal(t, source.ModelRouting, duplicate.ModelRouting)
	require.Equal(t, source.AllowedProtocols, duplicate.AllowedProtocols)
	require.Equal(t, source.ModelsListConfig, duplicate.ModelsListConfig)
	require.Equal(t, source.AvailabilityProbeConfig, duplicate.AvailabilityProbeConfig)
	require.Equal(t, source.RPMLimit, duplicate.RPMLimit)
	require.Equal(t, source.MaxReasoningEffort, duplicate.MaxReasoningEffort)
	require.Equal(t, source.MaxReasoningEffortOverLimit, duplicate.MaxReasoningEffortOverLimit)
	require.Equal(t, source.ReasoningEffortMappings, duplicate.ReasoningEffortMappings)
	require.EqualValues(t, 2, duplicate.ProviderCount)
	require.EqualValues(t, 2, duplicate.ActiveProviderCount)
	require.NotEmpty(t, duplicate.DuplicateOperationID)
	require.Equal(t, []int64{source.ID}, repo.createdFromSources)
	require.Equal(t, []providercore.GroupMembership{
		{ProviderID: 13, GroupID: duplicate.ID},
		{ProviderID: 17, GroupID: duplicate.ID},
	}, repo.createdBindings[duplicate.ID])

	duplicate.ModelRouting["gpt-*"][0] = 999
	duplicate.SupportedModelScopes[0] = "changed"
	duplicate.AllowedProtocols[0] = protocol.ProtocolOpenAIResponses
	duplicate.ModelsListConfig.Models[0] = "changed"
	duplicate.ReasoningEffortMappings[0].To = "changed"
	*duplicate.UnavailableFallbackGroupID = 999
	*duplicate.AdvancedSchedulerOverrides.LBTopK = 99
	*duplicate.AdvancedSchedulerOverrides.WeightPriority = 99
	require.Equal(t, int64(13), source.ModelRouting["gpt-*"][0])
	require.Equal(t, "claude", source.SupportedModelScopes[0])
	require.Equal(t, protocol.ProtocolAnthropicMessages, source.AllowedProtocols[0])
	require.Equal(t, "gpt-5.4", source.ModelsListConfig.Models[0])
	require.Equal(t, "xhigh", source.ReasoningEffortMappings[0].To)
	require.Equal(t, int64(9), *source.UnavailableFallbackGroupID)
	require.Equal(t, 3, *source.AdvancedSchedulerOverrides.LBTopK)
	require.Equal(t, 4.5, *source.AdvancedSchedulerOverrides.WeightPriority)
}

func TestDuplicateGroupRecoversSameOperationAndScopesByAdmin(t *testing.T) {
	source := &routing.Group{ID: 9, Name: "team", Status: billing.StatusActive}
	repo := newDuplicateGroupRepoStub(source)
	svc := newGroupAdminForTest(repo, repo, nil)
	ctx := context.Background()

	first, err := svc.DuplicateGroup(ctx, source.ID, "admin:7", "same-key")
	require.NoError(t, err)
	retry, err := svc.DuplicateGroup(ctx, source.ID, "admin:7", "same-key")
	require.NoError(t, err)
	recovered, err := svc.RecoverDuplicateGroup(ctx, source.ID, "admin:7", "same-key")
	require.NoError(t, err)
	otherAdmin, err := svc.DuplicateGroup(ctx, source.ID, "admin:8", "same-key")
	require.NoError(t, err)

	require.Equal(t, first.ID, retry.ID)
	require.Equal(t, first.ID, recovered.ID)
	require.NotEqual(t, first.ID, otherAdmin.ID)
	require.Equal(t, "team (Copy 2)", otherAdmin.Name)
}

func TestDuplicateGroupAdvancesNameAndTruncatesUnicodeByRunes(t *testing.T) {
	source := &routing.Group{ID: 12, Name: "team", Status: billing.StatusActive}
	repo := newDuplicateGroupRepoStub(source)
	repo.names["team (Copy)"] = struct{}{}
	svc := newGroupAdminForTest(repo, repo, nil)

	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:1", "")
	require.NoError(t, err)
	require.Equal(t, "team (Copy 2)", duplicate.Name)

	unicodeName := routing.DuplicateGroupName(strings.Repeat("组", 100), 23)
	require.Equal(t, routing.MaxGroupNameRunes, utf8.RuneCountInString(unicodeName))
	require.True(t, strings.HasSuffix(unicodeName, " (Copy 23)"))
}

func TestDuplicateGroupAtomicCreateFailureReturnsNoCopy(t *testing.T) {
	source := &routing.Group{ID: 15, Name: "team", Status: billing.StatusActive}
	repo := newDuplicateGroupRepoStub(source)
	repo.atomicCreateErr = errors.New("binding insert failed")
	svc := newGroupAdminForTest(repo, repo, nil)

	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:1", "key")

	require.ErrorContains(t, err, "binding insert failed")
	require.Nil(t, duplicate)
	require.Len(t, repo.groups, 1)
	require.Empty(t, repo.byOperation)
}
