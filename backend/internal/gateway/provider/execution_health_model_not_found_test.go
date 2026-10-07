package provider_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestRateLimitService_TempUnschedulableContextPreservesModelForPoolDependency(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	svc := newUpstreamHealthForTest(repo, nil, nil, providercore.HealthOptions{}, nil)

	provider := gatewaytestkit.ModelNotFoundProvider()
	provider.Record.Credentials["pool_mode"] = true
	ctx := requeststate.WithHealthModel(context.Background(), []string{"gpt-5.4"})

	// 池模式的健康观测未显式传入模型时，使用上下文中的规范模型限制冷却范围。
	observation := gatewayprovider.HealthObservationFromContext(ctx, http.StatusNotFound, http.Header{},
		[]byte(`{"error":{"message":"endpoint not found"}}`), nil)
	handled := gatewayprovider.ApplyExecutionHealth(ctx, svc, provider, observation).StopScheduling

	require.True(t, handled)
	require.Zero(t, repo.TempCalls)
	require.Len(t, repo.ModelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.4", repo.ModelRateLimitCalls[0].Scope)
}

func TestRateLimitService_ModelTempUnschedulableIsolatesSchedulerByModel(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	svc := newUpstreamHealthForTest(repo, nil, nil, providercore.HealthOptions{}, nil)

	provider := gatewaytestkit.ModelNotFoundProvider()
	provider.Record.Credentials["model_mapping"] = map[string]any{
		"public-a":   "upstream-a",
		"upstream-a": "upstream-b",
	}

	handled := gatewayprovider.ApplyExecutionHealth(context.Background(), svc, provider, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"endpoint not found"}}`), []string{"upstream-a"})).StopScheduling

	require.True(t, handled)
	require.Len(t, repo.ModelRateLimitCalls, 1)
	call := repo.ModelRateLimitCalls[0]
	require.Equal(t, "upstream-a", call.Scope, "canonical upstream model must not be mapped a second time")

	provider.Record.Extra = map[string]any{
		"model_rate_limits": map[string]any{
			call.Scope: map[string]any{
				"rate_limit_reset_at": call.ResetAt.UTC().Format(time.RFC3339),
			},
		},
	}

	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Schedulable(context.Background(), "public-a"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Schedulable(context.Background(), "gpt-5.6-sol"))
}
