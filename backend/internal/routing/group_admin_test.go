package routing

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// group_admin_test.go checks administrative membership copying through group
// creation and update. Repository spies record source reads, membership order,
// authentication filtering, and writes so failures expose the exact point
// where preparation stops before the destination changes.
//
// A nonempty copy request replaces existing bindings even when every source
// membership is filtered out; omitted and empty source lists retain bindings.
// Mutation callbacks supply a separate context, allowing the tests to check
// that settings and memberships share that context and cache invalidation
// follows the callback. These spies observe service calls and arguments;
// database transaction behavior requires separate tests using persistent storage.

// groupRepoStubForAdmin captures raw group writes and supplies configured
// reads for administrative tests.
type groupRepoStubForAdmin struct {
	// created and updated retain the supplied write pointers. getByID supplies
	// the read pointer, while getErr takes precedence over that result in both
	// ID readers.
	created *Group // recorded group creation argument
	updated *Group // recorded group update argument
	getByID *Group // supplied group read result
	// getErr makes both ID readers return nil and the supplied failure, taking
	// precedence over the configured getByID pointer.
	getErr error // injected group read error

	listWithFiltersCalls    int                         // recorded filtered listing count
	listWithFiltersParams   pagination.PaginationParams // captured filtered page options
	listWithFiltersPlatform string                      // captured platform filter text
	listWithFiltersStatus   string                      // captured status filter text
	listWithFiltersSearch   string                      // captured search filter text
	// listWithFiltersIsExclusive retains the caller's pointer for later
	// assertions; tests keep that pointed value stable until the listing request
	// has been inspected.
	listWithFiltersIsExclusive *bool // captured optional exclusivity filter
	// listWithFiltersGroups is returned directly. Callers observe these fixture
	// rows through the surrounding service or adapter and configure their
	// contents before use.
	listWithFiltersGroups []Group // supplied filtered listing rows
	// listWithFiltersResult overrides synthesized pagination when nonnil.
	// Otherwise ListWithFilters derives the total from listWithFiltersGroups and
	// copies page values from the request.
	listWithFiltersResult *pagination.PaginationResult // optional supplied page metadata
	// listWithFiltersErr suppresses rows and pagination after query arguments
	// have been recorded, allowing assertions to inspect a failed listing's
	// request.
	listWithFiltersErr      error // injected filtered listing error
	groupSortOrderLockCalls int   // recorded ordering lock requests
}

// groupProviderCopyRepoStub supplies source groups and records destination
// membership replacement calls.
type groupProviderCopyRepoStub struct {
	*groupRepoStubForAdmin // base administrative repository fixture
	// GroupProviders and GroupAdminInvalidator supply unused methods through nil
	// interfaces. Unexpected catalog or credential invalidation calls therefore
	// fail instead of silently accepting an unconfigured dependency.
	GroupProviders        // unused provider reader methods
	GroupAdminInvalidator // unused credential invalidation methods
	// groupsByID returns retained pointers for both source and target reads.
	// UpdateGroup therefore mutates the target object observed by the test's
	// identity assertion.
	groupsByID        map[int64]*Group // supplied source and destination groups
	sourceProviderIDs []int64          // supplied source provider memberships
	deletedGroupID    int64            // captured membership deletion target
	boundGroupID      int64            // captured membership binding target
	// boundProviderIDs copies the binding argument so later caller slice changes
	// cannot alter the membership values inspected by the test.
	boundProviderIDs []int64           // copied bound provider IDs
	createdID        int64             // assigned destination creation ID
	events           []string          // ordered administrative dependency calls
	writeContexts    []context.Context // contexts supplied to persistence calls
	// sourceErrors takes precedence over groupsByID for lightweight reads. Each
	// configured error makes validation stop at that source before membership reads.
	sourceErrors map[int64]error // configured source lookup failures
	// membershipError is returned with the configured source IDs, allowing tests
	// to verify that a failed read discards any accompanying membership result.
	membershipError error // injected source membership failure
	// providerError is returned with providers after recording the lookup, so a
	// failed authentication read can expose partial results without binding them.
	providerError error // injected provider hydration failure
	// providers is returned directly by GetByIDs, including with providerError.
	// Tests keep these records stable while administration filters their identities.
	providers []GroupProvider // configured provider authentication records
}

// TestAdminServiceUpdateGroupCopiesMembershipWithoutAssociationPriority
// updates a destination with memberships from one source group. It checks the
// same destination pointer is returned, existing memberships are cleared, and
// the two supplied provider IDs are rebound.
func TestAdminServiceUpdateGroupCopiesMembershipWithoutAssociationPriority(t *testing.T) {
	target := &Group{ID: 1701, Name: "target", Status: StatusActive}
	source := &Group{ID: 1702, Name: "source", Status: StatusActive}
	base := &groupRepoStubForAdmin{}
	repo := &groupProviderCopyRepoStub{
		groupRepoStubForAdmin: base,
		groupsByID:            map[int64]*Group{target.ID: target, source.ID: source},
		sourceProviderIDs:     []int64{71, 72},
	}
	svc := &GroupAdmin{groupRepo: repo, options: GroupAdminOptions{Mutate: func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }}}

	updated, err := svc.UpdateGroup(context.Background(), target.ID, &UpdateGroupInput{
		CopyProvidersFromGroupIDs: []int64{source.ID},
	})

	require.NoError(t, err)
	require.Same(t, target, updated)
	require.Equal(t, target.ID, repo.deletedGroupID)
	require.Equal(t, target.ID, repo.boundGroupID)
	require.Equal(t, []int64{71, 72}, repo.boundProviderIDs)
}

// TestGroupAdminCopyProvidersCharacterization exercises creation and updates
// with repeated sources, read failures, and authentication restrictions. It
// protects membership order, error identity, write contexts, and cache timing.
func TestGroupAdminCopyProvidersCharacterization(t *testing.T) {
	readFailure := errors.New("copy read failed")
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name            string          // subtest scenario name
				sources         []int64         // requested source group IDs
				members         []int64         // source membership read result
				oauthOnly       bool            // destination authentication restriction flag
				providers       []GroupProvider // provider hydration read result
				sourceErrors    map[int64]error // source validation error inputs
				membershipError error           // source membership error input
				providerError   error           // provider hydration error input
				wantReads       []string        // expected ordered read calls
				wantBound       []int64         // expected destination binding argument
				wantError       string          // expected public error string
			}{
				{
					name:      "repeated sources preserve membership order",
					sources:   []int64{20, 10, 20, 10},
					members:   []int64{73, 71, 72, 71},
					wantReads: []string{"source:20", "source:10", "members:[20 10]"},
					wantBound: []int64{73, 71, 72, 71},
				},
				{
					name:      "oauth excludes only api keys and missing records",
					sources:   []int64{20, 10, 20},
					members:   []int64{73, 71, 72, 71, 74, 99},
					oauthOnly: true,
					providers: []GroupProvider{
						{ID: 74, Type: "other"},
						{ID: 72, Type: capability.ProviderTypeAPIKey},
						{ID: 71, Type: capability.ProviderTypeOAuth},
						{ID: 73, Type: capability.ProviderTypeSetupToken},
					},
					wantReads: []string{"source:20", "source:10", "members:[20 10]", "providers:[73 71 72 71 74 99]"},
					wantBound: []int64{73, 71, 71, 74},
				},
				{
					name:      "all providers filtered",
					sources:   []int64{20},
					members:   []int64{72},
					oauthOnly: true,
					providers: []GroupProvider{{ID: 72, Type: capability.ProviderTypeAPIKey}},
					wantReads: []string{"source:20", "members:[20]", "providers:[72]"},
				},
				{
					name:      "nil sources preserve current memberships",
					oauthOnly: true,
				},
				{
					name:      "empty sources preserve current memberships",
					sources:   []int64{},
					oauthOnly: true,
				},
				{
					name:      "empty source memberships skip provider read",
					sources:   []int64{20},
					oauthOnly: true,
					wantReads: []string{"source:20", "members:[20]"},
				},
				{
					name:         "first unreadable source wins",
					sources:      []int64{20, 10, 20},
					sourceErrors: map[int64]error{20: readFailure, 10: errors.New("later failure")},
					wantReads:    []string{"source:20"},
					wantError:    "source group 20 not found: copy read failed",
				},
				{
					name:         "later source stops before membership read",
					sources:      []int64{20, 20, 10, 30},
					sourceErrors: map[int64]error{10: readFailure},
					wantReads:    []string{"source:20", "source:10"},
					wantError:    "source group 10 not found: copy read failed",
				},
				{
					name:            "membership read failure prevents provider filtering",
					sources:         []int64{20, 10},
					members:         []int64{71},
					oauthOnly:       true,
					membershipError: readFailure,
					wantReads:       []string{"source:20", "source:10", "members:[20 10]"},
					wantError:       "failed to get providers from source groups: copy read failed",
				},
				{
					name:          "provider read failure prevents mutation",
					sources:       []int64{20},
					members:       []int64{71},
					oauthOnly:     true,
					providers:     []GroupProvider{{ID: 71, Type: capability.ProviderTypeOAuth}},
					providerError: readFailure,
					wantReads:     []string{"source:20", "members:[20]", "providers:[71]"},
					wantError:     "failed to fetch providers for oauth filter: copy read failed",
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					repo, svc, mutationCtx := newGroupCopyCharacterization(t)
					repo.sourceProviderIDs = tc.members
					repo.providers = tc.providers
					repo.sourceErrors = tc.sourceErrors
					repo.membershipError = tc.membershipError
					repo.providerError = tc.providerError
					var group *Group
					var err error
					var wantEvents []string
					if operation == "create" {
						sortOrder := 0
						group, err = svc.CreateGroup(t.Context(), &CreateGroupInput{
							Name:                      "destination",
							RateMultiplier:            1,
							SortOrder:                 &sortOrder,
							CopyProvidersFromGroupIDs: tc.sources,
							RequireOAuthOnly:          tc.oauthOnly,
						})
					} else {
						group, err = svc.UpdateGroup(t.Context(), 1701, &UpdateGroupInput{
							CopyProvidersFromGroupIDs: tc.sources,
							RequireOAuthOnly:          &tc.oauthOnly,
						})
						wantEvents = append(wantEvents, "get:1701")
					}
					wantEvents = append(wantEvents, tc.wantReads...)
					if tc.wantError != "" {
						require.EqualError(t, err, tc.wantError)
						require.ErrorIs(t, err, readFailure)
						require.Nil(t, group)
						require.Empty(t, repo.writeContexts)
					} else {
						require.NoError(t, err)
						require.Equal(t, int64(1701), group.ID)
						wantEvents = append(wantEvents, "mutate:start", operation)
						if operation == "update" && len(tc.sources) > 0 {
							wantEvents = append(wantEvents, "clear:1701")
						}
						if len(tc.wantBound) > 0 {
							wantEvents = append(wantEvents, fmt.Sprintf("bind:1701:%v", tc.wantBound))
						}
						wantEvents = append(wantEvents, "mutate:done")
						if operation == "update" {
							wantEvents = append(wantEvents, "invalidate:1701")
						} else {
							require.Equal(t, int64(len(tc.wantBound)), group.ProviderCount)
						}
						for _, ctx := range repo.writeContexts {
							require.Same(t, mutationCtx, ctx)
						}
					}
					require.Equal(t, tc.wantBound, repo.boundProviderIDs)
					require.Equal(t, wantEvents, repo.events)
				})
			}
		})
	}
}

// TestGroupAdminCopyProvidersValidationOrder combines rejected probe settings
// with copy reads. Creation resolves sources before probe validation, while
// updates validate probes before reading any source memberships.
func TestGroupAdminCopyProvidersValidationOrder(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			repo, svc, _ := newGroupCopyCharacterization(t)
			repo.sourceProviderIDs = []int64{71}
			probe := GroupAvailabilityProbeConfig{Enabled: true}
			oauthOnly := true
			var err error
			var wantEvents []string
			if operation == "create" {
				_, err = svc.CreateGroup(t.Context(), &CreateGroupInput{
					Name:                      "destination",
					RateMultiplier:            1,
					RequireOAuthOnly:          oauthOnly,
					CopyProvidersFromGroupIDs: []int64{20},
					AvailabilityProbeConfig:   probe,
				})
				wantEvents = []string{"source:20", "members:[20]"}
			} else {
				_, err = svc.UpdateGroup(t.Context(), 1701, &UpdateGroupInput{
					RequireOAuthOnly:          &oauthOnly,
					CopyProvidersFromGroupIDs: []int64{20},
					AvailabilityProbeConfig:   &probe,
				})
				wantEvents = []string{"get:1701"}
			}
			require.EqualError(t, err, `error: code=400 reason="INVALID_AVAILABILITY_PROBE_CONFIG" message="availability_probe_config.model_id is required when enabled" metadata=map[]`)
			require.Equal(t, wantEvents, repo.events)
		})
	}
}

// TestGroupAdminCopyProvidersRejectsSelfBeforeSourceReads places an unreadable
// source before the destination ID. Self-copy rejection must win without
// reading either source or starting a mutation.
func TestGroupAdminCopyProvidersRejectsSelfBeforeSourceReads(t *testing.T) {
	repo, svc, _ := newGroupCopyCharacterization(t)
	repo.sourceErrors = map[int64]error{20: errors.New("source unavailable")}
	_, err := svc.UpdateGroup(t.Context(), 1701, &UpdateGroupInput{
		CopyProvidersFromGroupIDs: []int64{20, 20, 1701},
	})
	require.EqualError(t, err, "cannot copy providers from self")
	require.Equal(t, []string{"get:1701"}, repo.events)
}

// newGroupCopyCharacterization supplies two sources and a retained destination.
// Its mutation callback records entry and return while giving all writes one
// context distinct from the read context; test cleanup cancels it.
func newGroupCopyCharacterization(t *testing.T) (*groupProviderCopyRepoStub, *GroupAdmin, context.Context) {
	t.Helper()
	mutationCtx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	repo := &groupProviderCopyRepoStub{
		groupRepoStubForAdmin: &groupRepoStubForAdmin{},
		createdID:             1701,
		groupsByID: map[int64]*Group{
			1701: {ID: 1701, Name: "destination", Status: StatusActive},
			20:   {ID: 20, Name: "first source", Status: StatusActive},
			10:   {ID: 10, Name: "second source", Status: StatusActive},
		},
	}
	svc := &GroupAdmin{
		groupRepo:            repo,
		providerRepo:         repo,
		authCacheInvalidator: repo,
		options: GroupAdminOptions{Mutate: func(_ context.Context, fn func(context.Context) error) error {
			repo.events = append(repo.events, "mutate:start")
			err := fn(mutationCtx)
			repo.events = append(repo.events, "mutate:done")
			return err
		}},
	}
	return repo, svc, mutationCtx
}

// GetByID records the destination read and returns its retained group pointer.
// A missing or nil entry returns ErrGroupNotFound before update preparation can
// mutate a working destination.
func (s *groupProviderCopyRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	s.events = append(s.events, fmt.Sprintf("get:%d", id))
	group := s.groupsByID[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

// GetByIDLite records source validation and returns a configured group pointer.
// Injected source errors take precedence; absent groups return ErrGroupNotFound
// so tests can distinguish lookup failures from later membership reads.
func (s *groupProviderCopyRepoStub) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	s.events = append(s.events, fmt.Sprintf("source:%d", id))
	if err := s.sourceErrors[id]; err != nil {
		return nil, err
	}
	group := s.groupsByID[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

// GetProviderIDsByGroupIDs records source order and returns copied memberships.
// Empty memberships yield nil; an injected failure accompanies the copied IDs
// so callers must discard partial results when the read fails.
func (s *groupProviderCopyRepoStub) GetProviderIDsByGroupIDs(_ context.Context, ids []int64) ([]int64, error) {
	s.events = append(s.events, fmt.Sprintf("members:%v", ids))
	return append([]int64(nil), s.sourceProviderIDs...), s.membershipError
}

// DeleteProviderGroupsByGroupID records the destination ID and reports one
// removed membership. It succeeds for every request, making the clearing step
// observable before the test inspects rebinding.
func (s *groupProviderCopyRepoStub) DeleteProviderGroupsByGroupID(ctx context.Context, groupID int64) (int64, error) {
	s.events = append(s.events, fmt.Sprintf("clear:%d", groupID))
	s.writeContexts = append(s.writeContexts, ctx)
	s.deletedGroupID = groupID
	return 1, nil
}

// BindProvidersToGroup records the destination and copies the supplied
// provider IDs. The method succeeds for every request, and later caller
// mutations cannot change the captured binding slice.
func (s *groupProviderCopyRepoStub) BindProvidersToGroup(ctx context.Context, groupID int64, providerIDs []int64) error {
	s.events = append(s.events, fmt.Sprintf("bind:%d:%v", groupID, providerIDs))
	s.writeContexts = append(s.writeContexts, ctx)
	s.boundGroupID = groupID
	s.boundProviderIDs = append([]int64(nil), providerIDs...)
	return nil
}

// Create records the creation context and assigns the configured destination ID.
// It retains the supplied group pointer through the base repository so later
// membership binding observes that saved identity.
func (s *groupProviderCopyRepoStub) Create(ctx context.Context, group *Group) error {
	s.events = append(s.events, "create")
	s.writeContexts = append(s.writeContexts, ctx)
	group.ID = s.createdID
	return s.groupRepoStubForAdmin.Create(ctx, group)
}

// Update records the update context and retains the supplied group pointer.
// It delegates the successful write to the base fixture, allowing subsequent
// membership calls to be compared with that same operation context.
func (s *groupProviderCopyRepoStub) Update(ctx context.Context, group *Group) error {
	s.events = append(s.events, "update")
	s.writeContexts = append(s.writeContexts, ctx)
	return s.groupRepoStubForAdmin.Update(ctx, group)
}

// GetByIDs records requested membership order before supplying authentication
// records and an optional failure. The records are returned as configured so
// tests can provide missing identities and a different hydration order.
func (s *groupProviderCopyRepoStub) GetByIDs(_ context.Context, ids []int64) ([]GroupProvider, error) {
	s.events = append(s.events, fmt.Sprintf("providers:%v", ids))
	return s.providers, s.providerError
}

// InvalidateAuthCacheByGroupID records the group invalidated after mutation
// completion for call-order assertions.
func (s *groupProviderCopyRepoStub) InvalidateAuthCacheByGroupID(_ context.Context, id int64) {
	s.events = append(s.events, fmt.Sprintf("invalidate:%d", id))
}

// Create retains the supplied group pointer for later assertions and succeeds.
// It stores the caller's working object directly, so callers keep its fields
// stable while inspecting the captured creation argument.
func (s *groupRepoStubForAdmin) Create(_ context.Context, g *Group) error {
	s.created = g
	return nil
}

// Update retains the supplied group pointer for later assertions and succeeds.
// It stores the caller's working object directly, so callers keep its fields
// stable while inspecting the captured update argument.
func (s *groupRepoStubForAdmin) Update(_ context.Context, g *Group) error {
	s.updated = g
	return nil
}

// GetByID returns the configured group pointer unless getErr is set. It
// ignores the requested ID, and callers receive the same object retained in
// the fixture.
func (s *groupRepoStubForAdmin) GetByID(_ context.Context, _ int64) (*Group, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getByID, nil
}

// GetByIDLite returns the configured group pointer unless getErr is set. It
// ignores the requested ID and supplies the same fixture value as the full
// reader.
func (s *groupRepoStubForAdmin) GetByIDLite(_ context.Context, _ int64) (*Group, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getByID, nil
}

// Delete panics when administration tests request direct group deletion. The
// fixture has no implementation for this repository path, so invoking it
// exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete call")
}

// DeleteCascade panics when create or update fixtures receive a cascade
// deletion. The fixture has no implementation for this repository path, so
// invoking it exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}

// List panics when administration requests an unfiltered paginated listing.
// The fixture has no implementation for this repository path, so invoking it
// exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) List(_ context.Context, _ pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

// ListWithFilters records its pagination and filter inputs before returning
// the configured rows. An injected error suppresses rows and pagination;
// absent result metadata is synthesized from the row count and request page
// values.
func (s *groupRepoStubForAdmin) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]Group, *pagination.PaginationResult, error) {
	s.listWithFiltersCalls++
	s.listWithFiltersParams = params
	s.listWithFiltersPlatform = platform
	s.listWithFiltersStatus = status
	s.listWithFiltersSearch = search
	s.listWithFiltersIsExclusive = isExclusive

	if s.listWithFiltersErr != nil {
		return nil, nil, s.listWithFiltersErr
	}

	result := s.listWithFiltersResult
	if result == nil {
		result = &pagination.PaginationResult{
			Total:    int64(len(s.listWithFiltersGroups)),
			Page:     params.Page,
			PageSize: params.PageSize,
		}
	}

	return s.listWithFiltersGroups, result, nil
}

// ListActive panics when administration requests all active groups through
// this fixture. The fixture has no implementation for this repository path,
// so invoking it exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) ListActive(_ context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}

// ListActiveByPlatform panics when administration requests active groups
// filtered by platform. The fixture has no implementation for this repository
// path, so invoking it exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) ListActiveByPlatform(_ context.Context, _ string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}

// ListActiveByPlatformLite panics when administration requests lightweight
// active groups filtered by platform. The fixture has no implementation for
// this repository path, so invoking it exposes an unexpected dependency call
// immediately.
func (s *groupRepoStubForAdmin) ListActiveByPlatformLite(_ context.Context, _ string) ([]Group, error) {
	panic("unexpected ListActiveByPlatformLite call")
}

// ExistsByName panics when administration requests a group-name existence
// check. The fixture has no implementation for this repository path, so
// invoking it exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName call")
}

// GetProviderCount panics when administration requests separate
// provider-count hydration. The fixture has no implementation for this
// repository path, so invoking it exposes an unexpected dependency call
// immediately.
func (s *groupRepoStubForAdmin) GetProviderCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetProviderCount call")
}

// DeleteProviderGroupsByGroupID panics when the base fixture receives
// membership replacement cleanup. The fixture has no implementation for this
// repository path, so invoking it exposes an unexpected dependency call
// immediately.
func (s *groupRepoStubForAdmin) DeleteProviderGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	panic("unexpected DeleteProviderGroupsByGroupID call")
}

// BindProvidersToGroup panics when the base fixture receives new provider
// membership binding. The fixture has no implementation for this repository
// path, so invoking it exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) BindProvidersToGroup(_ context.Context, _ int64, _ []int64) error {
	panic("unexpected BindProvidersToGroup call")
}

// GetProviderIDsByGroupIDs panics when the base fixture receives source
// membership lookup. The fixture has no implementation for this repository
// path, so invoking it exposes an unexpected dependency call immediately.
func (s *groupRepoStubForAdmin) GetProviderIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	panic("unexpected GetProviderIDsByGroupIDs call")
}

// UpdateSortOrders accepts ordering writes successfully and retains no update
// arguments.
func (s *groupRepoStubForAdmin) UpdateSortOrders(_ context.Context, _ []GroupSortOrderUpdate) error {
	return nil
}

// LockGroupSortOrder counts each ordering-lock request and returns successful
// acquisition.
func (s *groupRepoStubForAdmin) LockGroupSortOrder(_ context.Context) error {
	s.groupSortOrderLockCalls++
	return nil
}
