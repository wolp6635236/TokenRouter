package provider_test

import (
	"context"
	"net/http"
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// dbFallbackRepoStub extends errorPolicyRepoStub with a configurable DB provider
// returned by GetByID, simulating cache miss + DB fallback.
type dbFallbackRepoStub struct {
	gatewaytestkit.ErrorPolicyStore

	dbProvider *gatewayprovider.ExecutionProvider // 非空时由 GetByID 返回。
}

func (r *dbFallbackRepoStub) GetByID(ctx context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	if r.dbProvider != nil && r.dbProvider.Record.ID == id {
		return r.dbProvider, nil
	}
	return nil, nil // not found, no error
}

func TestCheckErrorPolicy_401_DBFallback_Escalates(t *testing.T) {
	// Scenario: cache provider has empty TempUnschedulableReason (cache miss),
	// but DB provider has a previous 401 record.
	// Non-Antigravity: should escalate to ErrorPolicyNone (second 401 = permanent error).
	// Antigravity: skips escalation logic (401 handled by applyErrorPolicy rules).
	t.Run("gemini_escalates", func(t *testing.T) {
		repo := &dbFallbackRepoStub{
			dbProvider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 20,
					TempUnschedulableReason: `{"status_code":401,"until_unix":1735689600}`,
				},
			},
		}
		svc := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{}, nil)

		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 20,
				Type:                    capability.ProviderTypeOAuth,
				Platform:                capability.PlatformGemini,
				TempUnschedulableReason: "",
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(401),
							"keywords":         []any{"unauthorized"},
							"duration_minutes": float64(10),
						},
					},
				},
			},
		}

		result := svc.CheckErrorPolicy(context.Background(), gatewayprovider.ExecutionRecord(provider), gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusUnauthorized, nil, []byte(`unauthorized`), nil))
		require.Equal(t, providercore.ErrorPolicyNone, result, "gemini 401 with DB fallback showing previous 401 should escalate")
	})

	t.Run("antigravity_stays_temp", func(t *testing.T) {
		repo := &dbFallbackRepoStub{
			dbProvider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 20,
					TempUnschedulableReason: `{"status_code":401,"until_unix":1735689600}`,
				},
			},
		}
		svc := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{}, nil)

		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 20,
				Type:                    capability.ProviderTypeOAuth,
				Platform:                capability.PlatformAntigravity,
				TempUnschedulableReason: "",
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(401),
							"keywords":         []any{"unauthorized"},
							"duration_minutes": float64(10),
						},
					},
				},
			},
		}

		result := svc.CheckErrorPolicy(context.Background(), gatewayprovider.ExecutionRecord(provider), gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusUnauthorized, nil, []byte(`unauthorized`), nil))
		require.Equal(t, providercore.ErrorPolicyTempUnscheduled, result, "antigravity 401 skips escalation, stays temp-unscheduled")
	})
}

func TestCheckErrorPolicy_401_DBFallback_NoDBRecord_FirstHit(t *testing.T) {
	// Scenario: cache provider has empty TempUnschedulableReason,
	// DB also has no previous 401 record → should NOT escalate (first hit → temp unscheduled).
	repo := &dbFallbackRepoStub{
		dbProvider: &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 21,
				TempUnschedulableReason: "",
			}, // DB also empty
		},
	}
	svc := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{}, nil)

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 21,
			Type:                    capability.ProviderTypeOAuth,
			Platform:                capability.PlatformAntigravity,
			TempUnschedulableReason: "",
			Credentials: map[string]any{
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(401),
						"keywords":         []any{"unauthorized"},
						"duration_minutes": float64(10),
					},
				},
			},
		},
	}

	result := svc.CheckErrorPolicy(context.Background(), gatewayprovider.ExecutionRecord(provider), gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusUnauthorized, nil, []byte(`unauthorized`), nil))
	require.Equal(t, providercore.ErrorPolicyTempUnscheduled, result, "401 first hit with no DB record should temp-unschedule")
}

func TestCheckErrorPolicy_401_DBFallback_DBError_FirstHit(t *testing.T) {
	// Scenario: cache provider has empty TempUnschedulableReason,
	// DB lookup returns nil (not found) → should treat as first hit → temp unscheduled.
	repo := &dbFallbackRepoStub{
		dbProvider: nil, // GetByID returns nil, nil
	}
	svc := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{}, nil)

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 22,
			Type:                    capability.ProviderTypeOAuth,
			Platform:                capability.PlatformAntigravity,
			TempUnschedulableReason: "",
			Credentials: map[string]any{
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(401),
						"keywords":         []any{"unauthorized"},
						"duration_minutes": float64(10),
					},
				},
			},
		},
	}

	result := svc.CheckErrorPolicy(context.Background(), gatewayprovider.ExecutionRecord(provider), gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusUnauthorized, nil, []byte(`unauthorized`), nil))
	require.Equal(t, providercore.ErrorPolicyTempUnscheduled, result, "401 first hit with DB not found should temp-unschedule")
}
