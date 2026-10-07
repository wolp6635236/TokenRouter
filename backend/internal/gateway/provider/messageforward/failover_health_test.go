package messageforward

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGatewayFailoverSideEffects_BedrockUsesMappedModel(t *testing.T) {
	repo := &gatewaytestkit.ErrorPolicyStore{}
	healthObserver := gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: repo, Options: providercore.HealthOptions{}})

	svc := NewRuntime(Dependencies{Health: healthObserver}, Options{})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 20503,
			Type:     capability.ProviderTypeBedrock,
			Platform: capability.PlatformAnthropic,
			Credentials: map[string]any{
				"pool_mode":                  true,
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(http.StatusServiceUnavailable),
						"keywords":         []any{"maintenance"},
						"duration_minutes": float64(30),
					},
				},
			},
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"maintenance"}}`)),
	}

	decision := svc.failoverHealth(context.Background(), resp, provider, "anthropic.claude-mapped")

	require.Equal(t, providercore.ErrorPolicyTempUnscheduled, decision.Policy)
	require.True(t, decision.StopScheduling)
	require.False(t, decision.RetryableOnSameProvider(gatewayprovider.ExecutionErrorPolicy(provider), http.StatusServiceUnavailable))
	require.Len(t, repo.ModelRateLimitCalls, 1)
	require.Equal(t, "anthropic.claude-mapped", repo.ModelRateLimitCalls[0].Scope)
	require.Zero(t, repo.TempCalls)
}
