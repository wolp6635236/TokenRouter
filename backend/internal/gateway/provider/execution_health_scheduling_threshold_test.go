package provider_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	settingstestkit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
	"github.com/stretchr/testify/require"
)

func TestRateLimitService_ApplyProviderSchedulingThreshold_SetsTempUnschedulable(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[providercore.SettingKeyProviderSchedulingThresholds] = `{"openai":80}`

	providerRepo := &gatewaytestkit.HealthStoreRecorder{}
	rl := newUpstreamHealthForTest(providerRepo, &config.Config{}, nil, providercore.HealthOptions{},

		newExecutionReadersFixture(settingsRepo, &config.Config{}))

	until := time.Now().UTC().Add(6 * time.Hour)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 1001,
			Platform:    capability.PlatformOpenAI,
			Status:      billing.StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_7d_used_percent": 91.5,
				"codex_7d_reset_at":     until.Format(time.RFC3339),
			},
		},
	}

	blocked := gatewayprovider.ApplyExecutionSchedulingThreshold(context.Background(), rl, provider)

	require.True(t, blocked)
	require.Equal(t, 1, providerRepo.TempCalls)
	require.NotNil(t, provider.Record.TempUnschedulableUntil)
	require.WithinDuration(t, until, *provider.Record.TempUnschedulableUntil, time.Second)
	require.True(t, providercore.IsProviderSchedulingThresholdReason(providerRepo.LastTempReason))

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(providerRepo.LastTempReason), &payload))
	require.Equal(t, capability.PlatformOpenAI, payload["platform"])
	require.Equal(t, "7d", payload["window"])
	require.Equal(t, float64(80), payload["threshold_percent"])
	require.Equal(t, float64(91.5), payload["used_percent"])
	require.Contains(t, payload["error_message"], "91.5% used >= 80%")
}

func TestRateLimitService_ApplyProviderSchedulingThreshold_UsesProviderOverrideInReason(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[providercore.SettingKeyProviderSchedulingThresholds] = `{"openai":90}`

	providerRepo := &gatewaytestkit.HealthStoreRecorder{}
	rl := newUpstreamHealthForTest(providerRepo, &config.Config{}, nil, providercore.HealthOptions{},

		newExecutionReadersFixture(settingsRepo, &config.Config{}))

	until := time.Now().UTC().Add(6 * time.Hour)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 1003,
			Platform:    capability.PlatformOpenAI,
			Status:      billing.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"provider_scheduling_threshold": 80,
			},
			Extra: map[string]any{
				"codex_7d_used_percent": 85.5,
				"codex_7d_reset_at":     until.Format(time.RFC3339),
			},
		},
	}

	blocked := gatewayprovider.ApplyExecutionSchedulingThreshold(context.Background(), rl, provider)

	require.True(t, blocked)
	require.Equal(t, 1, providerRepo.TempCalls)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(providerRepo.LastTempReason), &payload))
	require.Equal(t, float64(80), payload["threshold_percent"])
	require.Equal(t, float64(85.5), payload["used_percent"])
	require.Contains(t, payload["error_message"], "85.5% used >= 80%")
}

type fableSchedulingThresholdRepoStub struct {
	gatewaytestkit.HealthStoreRecorder

	modelCalls      int
	lastModelScope  string
	lastModelReset  time.Time
	lastModelReason string
}

func (r *fableSchedulingThresholdRepoStub) SetModelRateLimit(_ context.Context, _ int64, scope string, resetAt time.Time, reason ...string) error {
	r.modelCalls++
	r.lastModelScope = scope
	r.lastModelReset = resetAt
	if len(reason) > 0 {
		r.lastModelReason = reason[0]
	}
	return nil
}

func TestRateLimitService_ApplyProviderSchedulingThreshold_FableOnlyLimitsFableModels(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[providercore.SettingKeyProviderSchedulingThresholds] = `{"anthropic":100}`

	providerRepo := &fableSchedulingThresholdRepoStub{}
	rl := newUpstreamHealthForTest(providerRepo, &config.Config{}, nil, providercore.HealthOptions{},

		newExecutionReadersFixture(settingsRepo, &config.Config{}))

	until := time.Now().UTC().Add(4 * 24 * time.Hour).Truncate(time.Second)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 1004,
			Platform:    capability.PlatformAnthropic,
			Status:      billing.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"provider_scheduling_threshold": 60,
			},
			Extra: map[string]any{
				"passive_usage_7d_utilization":    0.40,
				"passive_usage_7d_reset":          float64(time.Now().UTC().Add(3 * 24 * time.Hour).Unix()),
				"passive_usage_7d_oi_utilization": 0.61,
				"passive_usage_7d_oi_reset":       float64(until.Unix()),
			},
		},
	}

	blocked := gatewayprovider.ApplyExecutionSchedulingThreshold(context.Background(), rl, provider)

	require.False(t, blocked, "the Fable-only window must not pause the whole provider")
	require.Zero(t, providerRepo.TempCalls)
	require.Equal(t, 1, providerRepo.modelCalls)
	require.Equal(t, providercore.AnthropicFableRateLimitKey, providerRepo.lastModelScope)
	require.WithinDuration(t, until, providerRepo.lastModelReset, time.Second)
	require.True(t, providercore.IsProviderSchedulingThresholdReason(providerRepo.lastModelReason))
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Schedulable(context.Background(), "claude-fable-5"))
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Schedulable(context.Background(), "claude-fable-5[1m]"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Schedulable(context.Background(), "claude-opus-4-8"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Schedulable(context.Background(), "claude-sonnet-4-6"))

	blocked = gatewayprovider.ApplyExecutionSchedulingThreshold(context.Background(), rl, provider)

	require.False(t, blocked)
	require.Equal(t, 1, providerRepo.modelCalls, "an active model limit should not be persisted twice")
}

func TestRateLimitService_ApplyProviderSchedulingThreshold_SkipsDuplicateTempUnschedulable(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[providercore.SettingKeyProviderSchedulingThresholds] = `{"openai":80}`

	providerRepo := &gatewaytestkit.HealthStoreRecorder{}
	rl := newUpstreamHealthForTest(providerRepo, &config.Config{}, nil, providercore.HealthOptions{},

		newExecutionReadersFixture(settingsRepo, &config.Config{}))

	until := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Second)
	existingReason := providercore.BuildDetailedProviderSchedulingThresholdReason(providercore.ProviderSchedulingThresholdReasonInput{
		Platform:         capability.PlatformOpenAI,
		Window:           "7d",
		ThresholdPercent: 80,
		UsedPercent:      91.5,
		Until:            until,
		Now:              until.Add(-time.Hour),
	})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 1002,
			Platform:                capability.PlatformOpenAI,
			Status:                  billing.StatusActive,
			Schedulable:             true,
			TempUnschedulableUntil:  &until,
			TempUnschedulableReason: existingReason,
			Extra: map[string]any{
				"codex_7d_used_percent": 91.5,
				"codex_7d_reset_at":     until.Format(time.RFC3339),
			},
		},
	}

	blocked := gatewayprovider.ApplyExecutionSchedulingThreshold(context.Background(), rl, provider)

	require.True(t, blocked)
	require.Equal(t, 0, providerRepo.TempCalls)
	require.Equal(t, existingReason, provider.Record.TempUnschedulableReason)
	require.NotNil(t, provider.Record.TempUnschedulableUntil)
	require.True(t, until.Equal(*provider.Record.TempUnschedulableUntil))
}

func TestRateLimitService_ApplyProviderSchedulingThreshold_UnsupportedPlatformDoesNotBlock(t *testing.T) {
	settingsRepo := settingstestkit.NewMemory()
	settingsRepo.Data[providercore.SettingKeyProviderSchedulingThresholds] = `{"openai":80}`

	providerRepo := &gatewaytestkit.HealthStoreRecorder{}
	rl := newUpstreamHealthForTest(providerRepo, &config.Config{}, nil, providercore.HealthOptions{},

		newExecutionReadersFixture(settingsRepo, &config.Config{}))

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 2002,
			Platform:    "kiro",
			Status:      billing.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"provider_scheduling_threshold": 1,
			},
			Extra: map[string]any{
				"kiro_sched_utilization": 99.0,
				"kiro_sched_reset_at":    time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	blocked := gatewayprovider.ApplyExecutionSchedulingThreshold(context.Background(), rl, provider)

	require.False(t, blocked)
	require.Equal(t, 0, providerRepo.TempCalls)
	require.Nil(t, provider.Record.TempUnschedulableUntil)
	require.Empty(t, provider.Record.TempUnschedulableReason)
}
