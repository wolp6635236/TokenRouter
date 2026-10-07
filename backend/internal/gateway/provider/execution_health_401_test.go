package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	settingstestkit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"

	"github.com/TokenFlux/TokenRouter/internal/config"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestRateLimitService_HandleUpstreamError_OpenAIOAuth403UsesTempUnschedulable(t *testing.T) {
	repo := &gatewaytestkit.HealthStoreRecorder{}
	counter := &gatewaytestkit.ForbiddenCounter{Counts: []int64{1}}
	settingRepo := settingstestkit.NewMemory()
	data, err := json.Marshal(providercore.OpenAI403CooldownSettings{
		Enabled:                 true,
		CooldownMinutes:         7,
		ErrorOnThresholdEnabled: true,
		ThresholdCount:          providercore.OpenAI403DisableThresholdDefault,
		ThresholdWindowMinutes:  providercore.OpenAI403CounterWindowMinutesDefault,
	})
	require.NoError(t, err)
	settingRepo.Data[providercore.SettingKeyOpenAI403CooldownSettings] = string(data)
	service := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{ForbiddenCounter: counter},

		newExecutionReadersFixture(settingRepo, &config.Config{}))
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 104,
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
		},
	}

	before := time.Now()
	shouldDisable := gatewayprovider.ApplyExecutionHealth(context.Background(), service, provider, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"temporary forbidden"}}`), nil)).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.SetErrorCalls)
	require.Equal(t, 1, repo.TempCalls)
	require.WithinDuration(t, before.Add(7*time.Minute), repo.LastTempUntil, 2*time.Second)
	require.Contains(t, repo.LastTempReason, "temporary forbidden")
	require.Contains(t, repo.LastTempReason, "(1/3)")
}

func TestRateLimitService_HandleUpstreamError_OpenAIOAuth403DisabledUsesSetError(t *testing.T) {
	repo := &gatewaytestkit.HealthStoreRecorder{}
	counter := &gatewaytestkit.ForbiddenCounter{Counts: []int64{1}}
	settingRepo := settingstestkit.NewMemory()
	data, err := json.Marshal(providercore.OpenAI403CooldownSettings{Enabled: false, CooldownMinutes: 7})
	require.NoError(t, err)
	settingRepo.Data[providercore.SettingKeyOpenAI403CooldownSettings] = string(data)
	service := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{ForbiddenCounter: counter},

		newExecutionReadersFixture(settingRepo, &config.Config{}))
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 105,
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
		},
	}

	shouldDisable := gatewayprovider.ApplyExecutionHealth(context.Background(), service, provider, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"temporary forbidden"}}`), nil)).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.SetErrorCalls)
	require.Equal(t, 0, repo.TempCalls)
	require.Contains(t, repo.LastErrorMsg, "temporary forbidden")
}

func TestRateLimitService_HandleUpstreamError_OpenAIOAuth403WithoutCounterUsesSetError(t *testing.T) {
	repo := &gatewaytestkit.HealthStoreRecorder{}
	service := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{}, nil)

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 106,
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
		},
	}

	shouldDisable := gatewayprovider.ApplyExecutionHealth(context.Background(), service, provider, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"temporary forbidden"}}`), nil)).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.SetErrorCalls)
	require.Equal(t, 0, repo.TempCalls)
	require.Contains(t, repo.LastErrorMsg, "temporary forbidden")
}

func TestRateLimitService_HandleUpstreamError_NonOpenAIOAuth403UsesSetError(t *testing.T) {
	repo := &gatewaytestkit.HealthStoreRecorder{}
	service := newUpstreamHealthForTest(repo, &config.Config{}, nil, providercore.HealthOptions{}, nil)

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 107,
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeOAuth,
		},
	}

	shouldDisable := gatewayprovider.ApplyExecutionHealth(context.Background(), service, provider, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"forbidden"}}`), nil)).StopScheduling

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.SetErrorCalls)
	require.Equal(t, 0, repo.TempCalls)
	require.Contains(t, repo.LastErrorMsg, "Access forbidden (403)")
}
