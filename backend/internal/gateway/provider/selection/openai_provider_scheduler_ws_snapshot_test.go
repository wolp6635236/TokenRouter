package selection

import (
	"context"
	"testing"
	time "time"

	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	egress "github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	scheduler "github.com/TokenFlux/TokenRouter/internal/scheduler"
	schedulerredis "github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayService_SelectProviderWithScheduler_UsesWSPassthroughSnapshotFlags(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10105)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 35001,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 10,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_oauth_responses_websockets_v2_mode": providercore.OpenAIWSIngressModePassthrough,
			},
		},
	}

	snapshotCache := &openAISnapshotCacheStub{
		snapshotProviders: []*gatewayprovider.ExecutionProvider{provider},
		providersByID:     map[int64]*gatewayprovider.ExecutionProvider{provider.Record.ID: provider},
	}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = providercore.OpenAIWSIngressModeCtxPool

	svc := newCompatibleSelectionForTest(CompatibleDependencies{
		Reads: Reads{
			Providers: schedulerTestOpenAIProviderRepo{providers: []gatewayprovider.ExecutionProvider{*provider}},
			Snapshot: schedulerredis.NewSnapshotReader(scheduler.NewSnapshotService(snapshotCache, nil,
				nil, nil, nil)),
		},
		Shared: Shared{
			Cache:       &schedulerTestGatewayCache{},
			Concurrency: scheduler.NewConcurrencyService(schedulerTestConcurrencyCache{}, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			Health:      gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{}),
			Parameters:  newAdvancedSchedulerParametersForTest(cfg, "true"),
		},
	}, cfg)

	selection, decision, err := svc.SelectProviderWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_ws_passthrough",
		"gpt-5.1",
		nil, egress.OpenAIUpstreamTransportResponsesWebsocketV2, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Provider)
	require.Equal(t, provider.Record.ID, selection.Provider.Record.ID)
	require.Equal(t, openAIProviderScheduleLayerLoadBalance, decision.Layer)
}
