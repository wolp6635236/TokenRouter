package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// provider_queries_test.go checks the SQL and failure contracts of platform
// scheduling selectors. Captured statements verify shared eligibility clocks
// and priority ordering, while empty platform lists preserve their nil result
// without contacting storage.

// schedulingQueryCapture records the executed statement before returning a
// supplied failure, allowing selector tests to inspect SQL without a database.
type schedulingQueryCapture struct {
	dialect.Driver        // unused driver operations
	query          string // executed SQL statement
	args           []any  // bound query parameters
	err            error  // injected query failure
	calls          int    // executed query count
}

// Dialect selects PostgreSQL placeholders and quoting for captured queries.
func (*schedulingQueryCapture) Dialect() string { return dialect.Postgres }

// Query captures SQL and bound values before returning the configured failure.
// No rows are produced, so callers must propagate that error before attempting
// provider hydration or another database operation. Unexpected argument types
// return a fixture error instead of attempting to capture incompatible values.
func (d *schedulingQueryCapture) Query(_ context.Context, query string, args, _ any) error {
	d.query = query
	values, ok := args.([]any)
	if !ok {
		return fmt.Errorf("expected []any query arguments, got %T", args)
	}
	d.args = values
	d.calls++
	return d.err
}

// TestPlatformSchedulingQueries captures the four platform scheduling selectors
// when their initial query fails. Each returns that error without hydration,
// and the captured SQL exposes eligibility, ordering, and bound clock values.
func TestPlatformSchedulingQueries(t *testing.T) {
	for _, name := range []string{"platform", "platforms", "ungrouped_platform", "ungrouped_platforms"} {
		t.Run(name, func(t *testing.T) {
			queryErr := errors.New("provider query failed")
			driver := &schedulingQueryCapture{err: queryErr}
			store := NewProviderStore(ent.NewClient(ent.Driver(driver)), nil, ProviderStoreOptions{})
			var records []provider.Record
			var err error
			started := time.Now()
			switch name {
			case "platform":
				records, err = store.ListSchedulableByPlatform(t.Context(), "anthropic")
			case "platforms":
				records, err = store.ListSchedulableByPlatforms(t.Context(), []string{"anthropic", "openai"})
			case "ungrouped_platform":
				records, err = store.ListSchedulableUngroupedByPlatform(t.Context(), "anthropic")
			case "ungrouped_platforms":
				records, err = store.ListSchedulableUngroupedByPlatforms(t.Context(), []string{"anthropic", "openai"})
			}
			require.ErrorIs(t, err, queryErr)
			require.Nil(t, records)
			require.Equal(t, 1, driver.calls)
			assertSchedulingQuery(t, driver, name, started, time.Now())
			t.Logf("SQL: %s\nARGS: %#v", driver.query, driver.args)
		})
	}
}

// assertSchedulingQuery checks the complete eligibility conjunction after
// substituting bound values, permitting predicates to change position. All
// application-clock parameters must share one sample inside the call interval;
// temporary pauses retain PostgreSQL NOW and ordering remains priority only.
func assertSchedulingQuery(t *testing.T, captured *schedulingQueryCapture, name string, started, finished time.Time) {
	t.Helper()
	_, whereAndOrder, found := strings.Cut(captured.query, ` FROM "providers" WHERE `)
	require.True(t, found)
	where, order, found := strings.Cut(whereAndOrder, " ORDER BY ")
	require.True(t, found)
	require.Equal(t, `"providers"."priority" ASC`, order)

	var sampled time.Time
	timeCount := 0
	for index := range slices.Backward(captured.args) {
		var replacement string
		switch value := captured.args[index].(type) {
		case time.Time:
			if sampled.IsZero() {
				sampled = value
			}
			require.Equal(t, sampled, value)
			require.False(t, value.Before(started))
			require.False(t, value.After(finished))
			timeCount++
			replacement = "@now"
		case string:
			replacement = fmt.Sprintf("%q", value)
		default:
			t.Fatalf("unexpected bound value: %#v", value)
		}
		where = strings.ReplaceAll(where, fmt.Sprintf("$%d", index+1), replacement)
	}
	require.Equal(t, 3, timeCount)

	predicates := []string{
		`"providers"."status" = "active"`,
		`"providers"."schedulable"`,
		`("providers"."temp_unschedulable_until" IS NULL OR "providers"."temp_unschedulable_until" <= NOW())`,
		`("providers"."expires_at" IS NULL OR "providers"."expires_at" > @now OR NOT "providers"."auto_pause_on_expired")`,
		`("providers"."overload_until" IS NULL OR "providers"."overload_until" <= @now)`,
		`("providers"."rate_limit_reset_at" IS NULL OR "providers"."rate_limit_reset_at" <= @now)`,
		`"providers"."deleted_at" IS NULL`,
	}
	if strings.HasSuffix(name, "platforms") {
		require.Len(t, captured.args, 6)
		predicates = append(predicates, `"providers"."platform" IN ("anthropic", "openai")`)
	} else {
		require.Len(t, captured.args, 5)
		predicates = append(predicates, `"providers"."platform" = "anthropic"`)
	}
	if strings.HasPrefix(name, "ungrouped_") {
		predicates = append(predicates, `(NOT (EXISTS (SELECT "provider_groups"."provider_id" FROM "provider_groups" WHERE "providers"."id" = "provider_groups"."provider_id")))`)
	}
	for _, predicate := range predicates {
		require.Equal(t, 1, strings.Count(where, predicate), "predicate: %s; WHERE: %s", predicate, where)
		where = strings.Replace(where, predicate, "", 1)
	}
	require.Equal(t, len(predicates)-1, strings.Count(where, "AND"))
	remaining := strings.NewReplacer("AND", "", "(", "", ")", "", " ", "").Replace(where)
	require.Empty(t, remaining, "unexpected eligibility SQL")
}

// TestPlatformSchedulingQueriesEmptyPlatforms supplies nil and allocated empty
// platform lists to both plural selectors. They return nil without consulting
// the database, preserving the distinction from a successful query with no rows.
func TestPlatformSchedulingQueriesEmptyPlatforms(t *testing.T) {
	store := &ProviderStore{}
	for _, platforms := range [][]string{nil, {}} {
		records, err := store.ListSchedulableByPlatforms(t.Context(), platforms)
		require.NoError(t, err)
		require.Nil(t, records)
		records, err = store.ListSchedulableUngroupedByPlatforms(t.Context(), platforms)
		require.NoError(t, err)
		require.Nil(t, records)
	}
}
