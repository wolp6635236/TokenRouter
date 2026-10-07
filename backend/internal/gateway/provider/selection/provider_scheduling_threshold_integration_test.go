package selection

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	provider "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	settingstestkit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
	"github.com/stretchr/testify/require"
)

type thresholdSelectionProviderRepoStub struct {
	gatewaytestkit.HealthStoreRecorder

	providers []gatewayprovider.ExecutionProvider
}

func (r *thresholdSelectionProviderRepoStub) ListSchedulableByPlatform(_ context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	filtered := make([]gatewayprovider.ExecutionProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		if provider.Record.Platform == platform {
			filtered = append(filtered, provider)
		}
	}
	return filtered, nil
}

func (r *thresholdSelectionProviderRepoStub) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *thresholdSelectionProviderRepoStub) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func TestGatewayService_ListSchedulableProviders_DoesNotFilterUnsupportedThresholdPlatforms(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[provider.SettingKeyProviderSchedulingThresholds] = `{"openai":90}`

	providerRepo := &thresholdSelectionProviderRepoStub{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: provider.Record{
					LoadLocation: time.LoadLocation, ID: 3101,
					Platform:    "kiro",
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{
						"model_whitelist":               []string{"*"},
						"provider_scheduling_threshold": 1,
					},
					Extra: map[string]any{
						"kiro_sched_utilization": 95.0,
						"kiro_sched_reset_at":    time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
					},
				},
			},
			{
				Record: provider.Record{
					Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3102,
					Platform:    "kiro",
					Status:      billing.StatusActive,
					Schedulable: true,
					Extra: map[string]any{
						"kiro_sched_utilization": 42.0,
						"kiro_sched_reset_at":    time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
					},
				},
			},
		},
	}

	healthObserver := gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: providerRepo, Readers: gatewaytestkit.RuntimeReaders(settings.New(settingsRepo))})
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: providerRepo}, Shared: Shared{Health: healthObserver}}, &config.Config{})

	providers, useMixed, err := svc.listSchedulableProviders(context.Background(), selectionFixtureGroupID(context.Background()), "kiro", false)

	require.NoError(t, err)
	require.False(t, useMixed)
	require.Len(t, providers, 2)
	require.Equal(t, int64(3101), providers[0].Record.ID)
	require.Equal(t, int64(3102), providers[1].Record.ID)
	require.Equal(t, 0, providerRepo.TempCalls)
}

func TestOpenAIGatewayService_ListSchedulableProviders_FiltersThresholdBlockedProviders(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[provider.SettingKeyProviderSchedulingThresholds] = `{"openai":85}`

	providerRepo := &thresholdSelectionProviderRepoStub{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: provider.Record{
					Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 4101,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					Extra: map[string]any{
						"codex_7d_used_percent": 91.0,
						"codex_7d_reset_at":     time.Now().UTC().Add(12 * time.Hour).Format(time.RFC3339),
					},
				},
			},
			{
				Record: provider.Record{
					Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 4102,
					Platform:    capability.PlatformOpenAI,
					Status:      billing.StatusActive,
					Schedulable: true,
					Extra: map[string]any{
						"codex_7d_used_percent": 40.0,
						"codex_7d_reset_at":     time.Now().UTC().Add(12 * time.Hour).Format(time.RFC3339),
					},
				},
			},
		},
	}

	healthObserver := gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: providerRepo, Readers: gatewaytestkit.RuntimeReaders(settings.New(settingsRepo))})
	svc := newCompatibleSelectionForTest(CompatibleDependencies{Reads: Reads{Providers: providerRepo}, Shared: Shared{Health: healthObserver}}, &config.Config{})

	providers, err := svc.listSchedulableProviders(context.Background(), selectionFixtureGroupID(context.Background()), capability.PlatformOpenAI)

	require.NoError(t, err)
	require.Len(t, providers, 1)
	require.Equal(t, int64(4102), providers[0].Record.ID)
	require.Equal(t, 1, providerRepo.TempCalls)
}
