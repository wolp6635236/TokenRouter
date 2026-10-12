package provider_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestScheduleOllamaCloudUsageActivityOnlyForOllama(t *testing.T) {
	deferred, activity := gatewaytestkit.DeferredActivityRecorder(t)
	ollama := ollamaUsageProvider(1)
	other := ollamaUsageProvider(2)
	other.Record.Credentials["base_url"] = "https://api.openai.com"

	(&provideradapter.TransportHealth{Deferred: deferred}).Attempt(gatewayprovider.ExecutionRecord(ollama))
	(&provideradapter.TransportHealth{Deferred: deferred}).Attempt(gatewayprovider.ExecutionRecord(other))
	(&provideradapter.TransportHealth{}).Attempt(gatewayprovider.ExecutionRecord(ollama))

	require.NoError(t, deferred.StopContext(context.Background()))
	_, ok := activity.Load(int64(1))
	require.True(t, ok)
	_, ok = activity.Load(int64(2))
	require.False(t, ok)
}

func ollamaUsageProvider(id int64) *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: id, Name: fmt.Sprintf("ollama-%d", id), Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{"base_url": "https://ollama.com", "api_key": fmt.Sprintf("key-%d", id)},
			Extra:       map[string]any{}, Status: billing.StatusActive, Schedulable: true, Concurrency: 1,
		},
	}
}
