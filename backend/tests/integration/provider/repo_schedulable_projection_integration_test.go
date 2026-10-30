//go:build integration

package provider_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// repo_schedulable_projection_integration_test.go checks provider eligibility
// and the lightweight load projection against PostgreSQL. Fixtures cover
// disabled, expired, overloaded, rate-limited, and temporarily paused providers,
// with platform and membership restrictions, hydrated groups, priority ordering,
// and load-factor defaults.

// TestListSchedulableProviderLoadsMatchesListSchedulable creates PostgreSQL
// providers spanning configuration, health-window, and expiry conditions.
// Lightweight and full queries return the same IDs and order, with matching
// effective load values and the expected default of one.
func TestListSchedulableProviderLoadsMatchesListSchedulable(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newProviderStoreContract(client, tx, nil)
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	create := func(name string) *providercore.Record {
		return mustCreateProvider(t, client, &providercore.Record{Name: name, Schedulable: true})
	}

	positiveLoad := create("projection-positive-load")
	_, err := client.Provider.UpdateOneID(positiveLoad.ID).SetConcurrency(2).SetLoadFactor(9).SetPriority(30).Save(ctx)
	require.NoError(t, err)
	concurrencyFallback := create("projection-concurrency-fallback")
	_, err = client.Provider.UpdateOneID(concurrencyFallback.ID).SetConcurrency(4).SetPriority(10).Save(ctx)
	require.NoError(t, err)
	zeroFallback := create("projection-zero-fallback")
	_, err = client.Provider.UpdateOneID(zeroFallback.ID).SetConcurrency(0).SetLoadFactor(0).SetPriority(20).Save(ctx)
	require.NoError(t, err)

	disabled := create("projection-disabled")
	_, err = client.Provider.UpdateOneID(disabled.ID).SetStatus(billing.StatusDisabled).Save(ctx)
	require.NoError(t, err)
	unschedulable := create("projection-unschedulable")
	_, err = client.Provider.UpdateOneID(unschedulable.ID).SetSchedulable(false).Save(ctx)
	require.NoError(t, err)
	expired := create("projection-expired")
	_, err = client.Provider.UpdateOneID(expired.ID).SetExpiresAt(past).SetAutoPauseOnExpired(true).Save(ctx)
	require.NoError(t, err)
	expiredAllowed := create("projection-expired-allowed")
	_, err = client.Provider.UpdateOneID(expiredAllowed.ID).SetExpiresAt(past).SetAutoPauseOnExpired(false).Save(ctx)
	require.NoError(t, err)
	overloaded := create("projection-overloaded")
	_, err = client.Provider.UpdateOneID(overloaded.ID).SetOverloadUntil(future).Save(ctx)
	require.NoError(t, err)
	overloadCleared := create("projection-overload-cleared")
	_, err = client.Provider.UpdateOneID(overloadCleared.ID).SetOverloadUntil(past).Save(ctx)
	require.NoError(t, err)
	rateLimited := create("projection-rate-limited")
	_, err = client.Provider.UpdateOneID(rateLimited.ID).SetRateLimitResetAt(future).Save(ctx)
	require.NoError(t, err)
	rateLimitCleared := create("projection-rate-limit-cleared")
	_, err = client.Provider.UpdateOneID(rateLimitCleared.ID).SetRateLimitResetAt(past).Save(ctx)
	require.NoError(t, err)
	tempBlocked := create("projection-temp-blocked")
	_, err = client.Provider.UpdateOneID(tempBlocked.ID).SetTempUnschedulableUntil(future).Save(ctx)
	require.NoError(t, err)
	tempCleared := create("projection-temp-cleared")
	_, err = client.Provider.UpdateOneID(tempCleared.ID).SetTempUnschedulableUntil(past).Save(ctx)
	require.NoError(t, err)

	providers, err := repo.ListSchedulable(ctx)
	require.NoError(t, err)
	loads, err := repo.ListSchedulableProviderLoads(ctx)
	require.NoError(t, err)

	providerIDs := make([]int64, 0, len(providers))
	wantByID := make(map[int64]int, len(providers))
	for i := range providers {
		providerIDs = append(providerIDs, providers[i].ID)
		wantByID[providers[i].ID] = providers[i].EffectiveLoadFactor()
	}

	loadIDs := make([]int64, 0, len(loads))
	byID := make(map[int64]int, len(loads))
	for _, load := range loads {
		loadIDs = append(loadIDs, load.ID)
		byID[load.ID] = load.MaxConcurrency
	}
	require.Equal(t, providerIDs, loadIDs)
	targetIDs := map[int64]struct{}{
		positiveLoad.ID: {}, concurrencyFallback.ID: {}, zeroFallback.ID: {},
	}
	targetOrder := make([]int64, 0, len(targetIDs))
	for _, id := range loadIDs {
		if _, ok := targetIDs[id]; ok {
			targetOrder = append(targetOrder, id)
		}
	}
	require.Equal(t, []int64{concurrencyFallback.ID, zeroFallback.ID, positiveLoad.ID}, targetOrder)
	require.Equal(t, wantByID, byID)
	require.Equal(t, 9, byID[positiveLoad.ID])
	require.Equal(t, 4, byID[concurrencyFallback.ID])
	require.Equal(t, 1, byID[zeroFallback.ID])
	for _, included := range []*providercore.Record{expiredAllowed, overloadCleared, rateLimitCleared, tempCleared} {
		require.Contains(t, byID, included.ID)
	}
	for _, excluded := range []*providercore.Record{disabled, unschedulable, expired, overloaded, rateLimited, tempBlocked} {
		require.NotContains(t, byID, excluded.ID)
	}
}

// TestPlatformSchedulingEligibility applies every platform selector to the same
// PostgreSQL fixtures. Eligibility windows, membership filtering, hydration,
// and priority ordering agree across single-platform and multiple-platform calls.
func TestPlatformSchedulingEligibility(t *testing.T) {
	ctx := t.Context()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newProviderStoreContract(client, tx, nil)
	group := mustCreateGroup(t, client, &routing.Group{Name: "platform-eligibility"})
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	fixtures := []struct {
		name     string                       // fixture state label
		platform string                       // stored platform identifier
		eligible bool                         // expected window eligibility
		grouped  bool                         // attach group membership
		update   func(*ent.ProviderUpdateOne) // optional state mutation
	}{
		{name: "ordinary", eligible: true},
		{name: "grouped", eligible: true, grouped: true},
		{name: "disabled", update: func(q *ent.ProviderUpdateOne) { q.SetStatus(billing.StatusDisabled) }},
		{name: "unschedulable", update: func(q *ent.ProviderUpdateOne) { q.SetSchedulable(false) }},
		{name: "deleted", update: func(q *ent.ProviderUpdateOne) { q.SetDeletedAt(past) }},
		{name: "expired", update: func(q *ent.ProviderUpdateOne) {
			q.SetExpiresAt(past)
			q.SetAutoPauseOnExpired(true)
		}},
		{name: "expired-allowed", eligible: true, update: func(q *ent.ProviderUpdateOne) {
			q.SetExpiresAt(past)
			q.SetAutoPauseOnExpired(false)
		}},
		{name: "future-expiry", eligible: true, update: func(q *ent.ProviderUpdateOne) {
			q.SetExpiresAt(future)
			q.SetAutoPauseOnExpired(true)
		}},
		{name: "overload", update: func(q *ent.ProviderUpdateOne) { q.SetOverloadUntil(future) }},
		{name: "overload-cleared", eligible: true, update: func(q *ent.ProviderUpdateOne) { q.SetOverloadUntil(past) }},
		{name: "rate-limit", update: func(q *ent.ProviderUpdateOne) { q.SetRateLimitResetAt(future) }},
		{name: "rate-limit-cleared", eligible: true, update: func(q *ent.ProviderUpdateOne) { q.SetRateLimitResetAt(past) }},
		{name: "temp-pause", update: func(q *ent.ProviderUpdateOne) { q.SetTempUnschedulableUntil(future) }},
		{name: "temp-pause-cleared", eligible: true, update: func(q *ent.ProviderUpdateOne) { q.SetTempUnschedulableUntil(past) }},
		{name: "openai", platform: capability.PlatformOpenAI, eligible: true},
		{name: "gemini", platform: capability.PlatformGemini, eligible: true},
	}
	providerIDs := make([]int64, len(fixtures))
	for index, fixture := range fixtures {
		provider := mustCreateProvider(t, client, &providercore.Record{
			Name:     "eligibility-" + fixture.name,
			Platform: fixture.platform,
			Priority: len(fixtures) - index,
		})
		providerIDs[index] = provider.ID
		if fixture.update != nil {
			update := client.Provider.UpdateOneID(provider.ID)
			fixture.update(update)
			require.NoError(t, update.Exec(ctx))
		}
		if fixture.grouped {
			mustBindProviderToGroup(t, client, provider.ID, group.ID)
		}
	}

	for _, test := range []struct {
		name      string                                               // selected method label
		multiple  bool                                                 // include OpenAI providers
		ungrouped bool                                                 // exclude grouped providers
		query     func(context.Context) ([]providercore.Record, error) // selector under test
	}{
		{name: "platform", query: func(ctx context.Context) ([]providercore.Record, error) {
			return repo.ListSchedulableByPlatform(ctx, capability.PlatformAnthropic)
		}},
		{name: "platforms", multiple: true, query: func(ctx context.Context) ([]providercore.Record, error) {
			return repo.ListSchedulableByPlatforms(ctx, []string{capability.PlatformAnthropic, capability.PlatformOpenAI})
		}},
		{name: "ungrouped_platform", ungrouped: true, query: func(ctx context.Context) ([]providercore.Record, error) {
			return repo.ListSchedulableUngroupedByPlatform(ctx, capability.PlatformAnthropic)
		}},
		{name: "ungrouped_platforms", multiple: true, ungrouped: true, query: func(ctx context.Context) ([]providercore.Record, error) {
			return repo.ListSchedulableUngroupedByPlatforms(ctx, []string{capability.PlatformAnthropic, capability.PlatformOpenAI})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wantIDs := make([]int64, 0)
			for index := range slices.Backward(fixtures) {
				fixture := fixtures[index]
				if !fixture.eligible || (test.ungrouped && fixture.grouped) || fixture.platform == capability.PlatformGemini {
					continue
				}
				if fixture.platform == capability.PlatformOpenAI && !test.multiple {
					continue
				}
				wantIDs = append(wantIDs, providerIDs[index])
			}
			records, err := test.query(t.Context())
			require.NoError(t, err)
			gotIDs := make([]int64, 0, len(records))
			for _, record := range records {
				gotIDs = append(gotIDs, record.ID)
				if record.ID == providerIDs[1] {
					require.Equal(t, []int64{group.ID}, record.GroupIDs)
					require.Len(t, record.Groups, 1)
					require.Equal(t, group.Name, record.Groups[0].Name)
					require.Len(t, record.ProviderGroups, 1)
				}
			}
			require.Equal(t, wantIDs, gotIDs)
		})
	}
}
