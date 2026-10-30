//go:build integration

package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	_ "github.com/TokenFlux/TokenRouter/ent/runtime"
	"github.com/TokenFlux/TokenRouter/internal/app/bootstrap"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// helpers_integration_test.go supplies shared database and provider-probe fixtures.
// PostgreSQL scenarios share one migrated pool between SQL and Ent operations;
// the recording transport and attribute repository keep assembled native probes
// independent of real upstream services while preserving their request and
// model-policy observations for assertions.

var (
	// NewProviderTestsForTest exposes provider probe construction with application
	// dependencies for integration packages that exercise the assembled test service.
	NewProviderTestsForTest = provideProviderTests

	// These health aliases expose the application's runtime and upstream-health
	// constructors so integration consumers resolve the production observation bindings.
	NewProviderHealthRuntimeForTest = provideProviderHealthRuntime
	NewUpstreamHealthForTest        = provideUpstreamHealth

	// NewProviderStoreForTest exposes application store configuration and event-write
	// construction to integration packages that need the assembled provider persistence.
	NewProviderStoreForTest = provideProviderStore
)

// databaseFixture exposes one migrated PostgreSQL container through SQL and Ent.
type databaseFixture struct {
	// db and client share the same pool through Ent's SQL driver. Test cleanup
	// closes the client before terminating the container; callers keep the fixture
	// available until their database operations finish.
	db     *sql.DB       // direct fixture queries
	client *dbent.Client // fixture entity operations
	dsn    string        // fixture connection string
	host   string        // mapped container hostname
	port   int           // mapped PostgreSQL port
}

// nativeProviderTestTransport records platform test requests and serves a
// configured stream body.
type nativeProviderTestTransport struct {
	body string // configured platform stream
	// requests and ids are appended together for each Do invocation. Sequential
	// subtests inspect the corresponding indexes after platform Test returns, retaining each supplied request pointer.
	requests []*http.Request // captured upstream requests
	ids      []int64         // captured provider identities
}

// probeAttributeRepository supplies group-specific profiles for assembled provider tests.
type probeAttributeRepository struct {
	routing.ModelAttributeRepository // unused profile management operations
	// config changes between platform cases to restrict the same declaration to
	// that platform's endpoint. Each resolution reads a detached profile copy.
	config *routing.ModelAttributeConfig
	groups []int64 // observed profile lookup identifiers
}

// newDatabaseFixture starts a PostgreSQL container, opens its shared SQL/Ent
// pool, and applies migrations within a two-minute setup context. Failures
// stop the test. Test cleanup closes Ent before terminating any created
// container with a fresh thirty-second context, including after startup errors.
// Callers need no separate pool owner.
func newDatabaseFixture(t *testing.T) *databaseFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23", tcpostgres.WithDatabase("test_contracts"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("postgres"), tcpostgres.BasicWaitStrategies())
	// Register any created container before startup errors can stop this test.
	// Later client cleanups run first because testing reverses registration order.
	if pg != nil {
		t.Cleanup(func() {
			// Setup or test cancellation must not cancel Docker removal. Start the
			// bounded cleanup context when this callback runs.
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := pg.Terminate(cleanupCtx); err != nil {
				t.Errorf("terminate PostgreSQL container: %v", err)
			}
		})
	}
	require.NoError(t, err)
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	// Bound fixture connections while Ent and SQL consumers share this pool.
	db.SetMaxOpenConns(16)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	// Apply the production schema before returning fixture access so downstream
	// constructors and direct SQL assertions operate on the migrated database.
	require.NoError(t, bootstrap.ApplyMigrations(ctx, db))
	host, err := pg.Host(ctx)
	require.NoError(t, err)
	port, err := pg.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return &databaseFixture{db: db, client: client, dsn: dsn, host: host, port: port.Int()}
}

// Do records the request and provider ID before returning the configured
// stream. The caller closes the owned response body, while proxy and
// concurrency inputs are ignored by this sequential platform-test fixture.
func (f *nativeProviderTestTransport) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	f.requests = append(f.requests, req)
	f.ids = append(f.ids, id)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

// DoWithTLS delegates the configured platform test exchange while ignoring TLS
// profile.
func (f *nativeProviderTestTransport) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return f.Do(req, proxy, id, concurrency)
}

// ForGroup supplies group 59's profile, fails group 61, and rejects output in 62.
// Other groups inherit catalogue defaults, allowing each test to observe isolated overrides.
func (r *probeAttributeRepository) ForGroup(_ context.Context, groupID int64) (*routing.ModelAttributeConfig, error) {
	r.groups = append(r.groups, groupID)
	if groupID == 61 {
		return nil, errors.New("profile storage unavailable")
	}
	if groupID == 62 {
		empty := []string{}
		return &routing.ModelAttributeConfig{Status: "active", Rules: []routing.ModelAttributeRule{{Models: []string{"*"}, Attributes: modelcatalog.Attributes{OutputModalities: &empty}}}}, nil
	}
	if groupID == 59 {
		return r.config, nil
	}
	return nil, nil
}
