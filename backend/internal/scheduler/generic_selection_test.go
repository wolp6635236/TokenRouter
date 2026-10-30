package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

// generic_selection_test.go exercises generic candidate admission through the
// public selector with observable adapter callbacks. Routed and ordinary pools
// must retain the same short-circuit order, privacy writes, and weighted sticky
// allowances while their candidate order and diagnostics stay local.

// genericPoolFixture records eligibility calls while supplying an advanced
// group whose zero scoring weights select the lowest eligible provider ID.
type genericPoolFixture struct {
	group     *FlowGroup               // admitted test group
	providers []FlowProvider           // ordered source candidate pool
	rejected  map[int64]string         // first denied callback per provider
	settings  policy.EffectiveSettings // request effective scoring policy
	stickyID  int64                    // prefetched affinity provider identity
	// stickyOnly names a cost or RPM callback that admits provider 1 only when
	// the selector passes sticky=true; other candidates ignore this additional
	// requirement and still apply their configured rejection.
	stickyOnly    string   // sticky-only admission callback
	upstreamCheck bool     // upstream restriction stage enabled
	calls         []string // observed eligibility callback sequence
	logs          []string // formatted model routing diagnostics
}

// TestGenericSelectorPoolEligibilityOrder rejects the first provider at each
// eligibility boundary. Both pools must stop that provider's callbacks at the
// rejection and select the next eligible provider after the same checks.
func TestGenericSelectorPoolEligibilityOrder(t *testing.T) {
	for _, routed := range []bool{false, true} {
		for _, rejected := range []string{"", "selection", "privacy", "platform", "model", "upstream", "model_scope", "quota", "window", "rpm"} {
			t.Run(fmt.Sprintf("routed=%t/rejected=%s", routed, rejected), func(t *testing.T) {
				fixture := newGenericPoolFixture(routed)
				fixture.rejected[1] = rejected
				if rejected == "privacy" {
					fixture.providers[0].PrivacySet = false
				}
				selection, err := fixture.selector(t).Select(t.Context(), SelectionInput{
					GroupID: &fixture.group.ID, RequestedModel: "model",
				})
				require.NoError(t, err)
				wantID := int64(2)
				if rejected == "" {
					wantID = 1
				}
				require.Equal(t, wantID, selection.Provider.ID)
				want := genericPoolCalls(1, false)
				if rejected == "privacy" {
					want = []string{"selection:1", "privacy:1"}
				} else if rejected != "" {
					for i, call := range want {
						if call == fmt.Sprintf("%s:1", rejected) || call == fmt.Sprintf("%s:1:false", rejected) {
							want = want[:i+1]
							break
						}
					}
				}
				want = append(want, genericPoolCalls(2, false)...)
				require.Equal(t, want, fixture.calls)
			})
		}
	}
}

// TestGenericSelectorPoolWeightedStickyAdmission supplies sticky-only window
// and RPM limits. Routed and ordinary candidates receive matching sticky flags,
// and a rejected window prevents the later RPM callback from running.
func TestGenericSelectorPoolWeightedStickyAdmission(t *testing.T) {
	for _, routed := range []bool{false, true} {
		for _, weighted := range []bool{false, true} {
			for _, stickyID := range []int64{1, 2} {
				for _, stickyOnly := range []string{"window", "rpm"} {
					t.Run(fmt.Sprintf("routed=%t/weighted=%t/sticky=%d/limit=%s", routed, weighted, stickyID, stickyOnly), func(t *testing.T) {
						fixture := newGenericPoolFixture(routed)
						fixture.settings.StickyWeightedEnabled = weighted
						fixture.stickyID = stickyID
						fixture.stickyOnly = stickyOnly
						selection, err := fixture.selector(t).Select(t.Context(), SelectionInput{
							GroupID: &fixture.group.ID, RequestedModel: "model",
						})
						require.NoError(t, err)
						providerSticky := weighted && stickyID == 1
						wantID := int64(2)
						if providerSticky {
							wantID = 1
						}
						require.Equal(t, wantID, selection.Provider.ID)
						want := genericPoolCalls(1, providerSticky)
						if !providerSticky && stickyOnly == "window" {
							want = want[:len(want)-1]
						}
						want = append(want, genericPoolCalls(2, weighted && stickyID == 2)...)
						require.Equal(t, want, fixture.calls)
					})
				}
			}
		}
	}
}

// TestGenericSelectorPoolEmptyModelAndUpstreamShortcut omits the requested
// model and optionally disables upstream restrictions. Model support is skipped,
// while runtime model eligibility and all later quota checks still run.
func TestGenericSelectorPoolEmptyModelAndUpstreamShortcut(t *testing.T) {
	for _, upstreamCheck := range []bool{false, true} {
		t.Run(fmt.Sprintf("upstream=%t", upstreamCheck), func(t *testing.T) {
			fixture := newGenericPoolFixture(false)
			fixture.providers = fixture.providers[:1]
			fixture.upstreamCheck = upstreamCheck
			selection, err := fixture.selector(t).Select(t.Context(), SelectionInput{GroupID: &fixture.group.ID})
			require.NoError(t, err)
			require.Equal(t, int64(1), selection.Provider.ID)
			want := []string{"selection:1", "platform:1"}
			if upstreamCheck {
				want = append(want, "upstream:1")
			}
			want = append(want, "model_scope:1", "quota:1", "window:1:false", "rpm:1:false")
			require.Equal(t, want, fixture.calls)
		})
	}
}

// TestGenericSelectorPoolCandidateOrder skips excluded and missing IDs before
// eligibility callbacks. It protects routed rule order and ordinary source order
// independently of the advanced scorer's eventual provider choice.
func TestGenericSelectorPoolCandidateOrder(t *testing.T) {
	for _, routed := range []bool{false, true} {
		t.Run(fmt.Sprintf("routed=%t", routed), func(t *testing.T) {
			fixture := newGenericPoolFixture(routed)
			fixture.providers = []FlowProvider{
				{ID: 3, PrivacySet: true}, {ID: 1, PrivacySet: true}, {ID: 2, PrivacySet: true},
			}
			fixture.group.ModelRouting["model"] = []int64{2, 404, 1, 3}
			selection, err := fixture.selector(t).Select(t.Context(), SelectionInput{
				GroupID: &fixture.group.ID, RequestedModel: "model", ExcludedIDs: map[int64]struct{}{1: {}},
			})
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Provider.ID)
			order := []int64{3, 2}
			if routed {
				order = []int64{2, 3}
				require.Contains(t, fixture.logs, "[ModelRoutingDebug] routed candidates: group_id=7 model=model routed=4 candidates=2 filtered(excluded=1 missing=1 unsched=0 platform=0 model_scope=0 model_mapping=0 window_cost=0)")
			}
			want := append(genericPoolCalls(order[0], false), genericPoolCalls(order[1], false)...)
			require.Equal(t, want, fixture.calls)
		})
	}
}

// TestGenericSelectorRoutedPoolDiagnostics gives each routed provider one
// distinct rejection. Only historically counted reasons appear in the summary,
// while model runtime rejection also preserves its skipped-provider diagnostic.
func TestGenericSelectorRoutedPoolDiagnostics(t *testing.T) {
	fixture := newGenericPoolFixture(true)
	fixture.providers = nil
	fixture.group.ModelRouting["model"] = []int64{11, 404}
	for i, rejected := range []string{"selection", "privacy", "platform", "model", "upstream", "model_scope", "quota", "window", "rpm", ""} {
		id := int64(i + 1)
		fixture.providers = append(fixture.providers, FlowProvider{ID: id, PrivacySet: rejected != "privacy"})
		fixture.rejected[id] = rejected
		fixture.group.ModelRouting["model"] = append(fixture.group.ModelRouting["model"], id)
	}
	selection, err := fixture.selector(t).Select(t.Context(), SelectionInput{
		GroupID: &fixture.group.ID, RequestedModel: "model", ExcludedIDs: map[int64]struct{}{11: {}},
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), selection.Provider.ID)
	require.Contains(t, fixture.logs, "[ModelRoutingDebug] routed candidates: group_id=7 model=model routed=12 candidates=1 filtered(excluded=1 missing=1 unsched=1 platform=1 model_scope=1 model_mapping=1 window_cost=1)")
	require.Contains(t, fixture.logs, "[ModelRoutingDebug] model_rate_limited providers skipped: group_id=7 model=model provider_ids=[6]")
}

// TestGenericSelectorRoutedPoolRechecksOrdinaryFallback rejects the entire
// preferred pool. Ordinary selection evaluates the same provider again, retaining
// callback side effects and final exhaustion after both passes.
func TestGenericSelectorRoutedPoolRechecksOrdinaryFallback(t *testing.T) {
	fixture := newGenericPoolFixture(true)
	fixture.providers = fixture.providers[:1]
	fixture.providers[0].PrivacySet = false
	fixture.group.ModelRouting["model"] = []int64{1}
	selection, err := fixture.selector(t).Select(t.Context(), SelectionInput{
		GroupID: &fixture.group.ID, RequestedModel: "model",
	})
	require.ErrorIs(t, err, ErrNoAvailableProviders)
	require.Nil(t, selection)
	require.Equal(t, []string{"selection:1", "privacy:1", "selection:1", "privacy:1"}, fixture.calls)
}

// newGenericPoolFixture supplies two eligible providers in an advanced group.
// Enabling routed selection configures the same order as the ordinary source;
// tests may replace either order before creating the selector.
func newGenericPoolFixture(routed bool) *genericPoolFixture {
	return &genericPoolFixture{
		group: &FlowGroup{Group: routing.Group{
			ID: 7, Name: "pool", RequirePrivacySet: true, SchedulerType: routing.GroupSchedulerTypeAdvanced,
			ModelRoutingEnabled: routed, ModelRouting: map[string][]int64{"model": {1, 2}},
		}},
		providers: []FlowProvider{{ID: 1, PrivacySet: true}, {ID: 2, PrivacySet: true}},
		rejected:  make(map[int64]string), settings: policy.EffectiveSettings{TopK: 1}, upstreamCheck: true,
	}
}

// selector binds recorded eligibility decisions to the public generic flow.
// Acquisition and hydration succeed locally, and privacy persistence deliberately
// fails so tests verify that its error never admits a rejected provider.
func (f *genericPoolFixture) selector(t *testing.T) *GenericSelector {
	t.Helper()
	ports := GenericSelectionPorts{
		SchedulingConfig: func() FlowOptions { return FlowOptions{} },
		CheckClaudeCodeRestriction: func(ctx context.Context, groupID *int64) (*FlowGroup, *int64, error) {
			return f.group, groupID, nil
		},
		WithGroupContext: func(ctx context.Context, _ *FlowGroup) context.Context { return ctx },
		AdvancedSchedulerEffectiveSettingsForRequest: func(context.Context, *int64) policy.EffectiveSettings { return f.settings },
		AdvancedSchedulerStats:                       func() *RuntimeStats { return nil },
		CheckGroupModelRestriction:                   func(context.Context, *int64, string) bool { return false },
		PrefetchedSticky:                             func(context.Context, *int64) int64 { return f.stickyID },
		DebugModelRoutingEnabled:                     func() bool { return true },
		ResolvePlatform:                              func(context.Context, *int64, *FlowGroup) (string, bool, error) { return "test-platform", true, nil },
		ListSchedulableProviders: func(context.Context, *int64, string, bool) ([]FlowProvider, bool, error) {
			return f.providers, true, nil
		},
		WithWindowCostPrefetch:             func(ctx context.Context, _ []FlowProvider) context.Context { return ctx },
		WithRPMPrefetch:                    func(ctx context.Context, _ []FlowProvider) context.Context { return ctx },
		NeedsUpstreamGroupRestrictionCheck: func(context.Context, *int64) bool { return f.upstreamCheck },
		GroupMappedModelForProviderLayer:   func(_ context.Context, model string) string { return model },
		IsProviderSchedulableForSelection:  func(provider *FlowProvider) bool { return f.check("selection", provider) },
		SetProviderError: func(_ context.Context, id int64, message string) error {
			require.Equal(t, "Privacy not set, required by group [pool]", message)
			f.calls = append(f.calls, fmt.Sprintf("privacy:%d", id))
			return errors.New("privacy write failed")
		},
		IsProviderAllowedForPlatform: func(provider *FlowProvider, platform string, mixed bool) bool {
			require.Equal(t, "test-platform", platform)
			require.True(t, mixed)
			return f.check("platform", provider)
		},
		IsModelSupportedByProviderWithContext: func(_ context.Context, provider *FlowProvider, _ string) bool {
			return f.check("model", provider)
		},
		IsUpstreamModelRestrictedByGroup: func(_ context.Context, groupID int64, provider *FlowProvider, _ string) bool {
			require.Equal(t, f.group.ID, groupID)
			return !f.check("upstream", provider)
		},
		IsProviderSchedulableForModelSelection: func(_ context.Context, provider *FlowProvider, _ string) bool {
			return f.check("model_scope", provider)
		},
		IsProviderSchedulableForQuota: func(provider *FlowProvider) bool { return f.check("quota", provider) },
		IsProviderSchedulableForWindowCost: func(_ context.Context, provider *FlowProvider, sticky bool) bool {
			return f.checkSticky("window", provider, sticky)
		},
		IsProviderSchedulableForRPM: func(_ context.Context, provider *FlowProvider, sticky bool) bool {
			return f.checkSticky("rpm", provider, sticky)
		},
		GroupModelUnsupportedErrorIfApplicable: func(context.Context, []FlowProvider, string, string, map[int64]struct{}, bool, *int64, *FlowGroup) error {
			return nil
		},
		TryAcquireProviderSlot: func(context.Context, int64, int) (*AcquireResult, error) {
			return &AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
		},
		CheckAndRegisterSession: func(context.Context, *FlowProvider, string) bool { return true },
		NewSelectionResult: func(_ context.Context, provider *FlowProvider, acquired bool, release func(), wait *ProviderWaitPlan) (*FlowSelection, error) {
			return &FlowSelection{Provider: provider, Acquired: acquired, ReleaseFunc: release, WaitPlan: wait}, nil
		},
	}
	return NewGenericSelector(ports, nil, nil, Diagnostics{Logf: func(_ string, format string, args ...any) {
		f.logs = append(f.logs, fmt.Sprintf(format, args...))
	}}, time.Now)
}

// check records one callback and reports whether that provider passes its gate.
// An absent or different rejection allows the callback, and calls retain every
// invocation in order for the test's assertions.
func (f *genericPoolFixture) check(gate string, provider *FlowProvider) bool {
	f.calls = append(f.calls, fmt.Sprintf("%s:%d", gate, provider.ID))
	return f.rejected[provider.ID] != gate
}

// checkSticky records the allowance supplied to a quota-related callback.
// Provider 1 additionally requires sticky admission when this gate matches
// stickyOnly; configured unconditional rejection still takes precedence.
func (f *genericPoolFixture) checkSticky(gate string, provider *FlowProvider, sticky bool) bool {
	f.calls = append(f.calls, fmt.Sprintf("%s:%d:%t", gate, provider.ID, sticky))
	return f.rejected[provider.ID] != gate && (provider.ID != 1 || f.stickyOnly != gate || sticky)
}

// genericPoolCalls describes the externally observed callbacks for one eligible
// candidate. Tests truncate this sequence at a rejected callback and compare
// against the actual public selector invocation, including sticky arguments.
func genericPoolCalls(id int64, sticky bool) []string {
	return []string{
		fmt.Sprintf("selection:%d", id), fmt.Sprintf("platform:%d", id), fmt.Sprintf("model:%d", id),
		fmt.Sprintf("upstream:%d", id), fmt.Sprintf("model_scope:%d", id), fmt.Sprintf("quota:%d", id),
		fmt.Sprintf("window:%d:%t", id, sticky), fmt.Sprintf("rpm:%d:%t", id, sticky),
	}
}
