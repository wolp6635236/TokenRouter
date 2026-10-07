package provider_test

import (
	"testing"
	time "time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestRequiresBillableGrokChatUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider *gatewayprovider.ExecutionProvider
		models   []string
		want     bool
	}{
		{name: "grok platform", provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok}}, models: []string{"alias"}, want: true},
		{name: "compatible Grok model", provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"grok-4.5"}, want: true},
		{name: "mapped Grok model", provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"alias", "grok-4.5"}, want: true},
		{name: "namespaced Grok model", provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"x-ai/grok-4.5"}, want: true},
		{name: "ordinary OpenAI model", provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"gpt-5.4"}, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, gatewayprovider.RequiresBillableGrokChatUsage(testCase.provider, testCase.models...))
		})
	}
}

func TestHasBillableGrokChatUsageRequiresAggregateToken(t *testing.T) {
	t.Parallel()

	require.False(t, gatewayprovider.HasBillableGrokChatUsage(openai.ForwardUsage{}))
	require.False(t, gatewayprovider.HasBillableGrokChatUsage(openai.ForwardUsage{ImageInputTokens: 2, ImageOutputTokens: 1}))
	require.True(t, gatewayprovider.HasBillableGrokChatUsage(openai.ForwardUsage{InputTokens: 1}))
	require.True(t, gatewayprovider.HasBillableGrokChatUsage(openai.ForwardUsage{OutputTokens: 1}))
	require.True(t, gatewayprovider.HasBillableGrokChatUsage(openai.ForwardUsage{CacheCreationInputTokens: 1}))
	require.True(t, gatewayprovider.HasBillableGrokChatUsage(openai.ForwardUsage{CacheReadInputTokens: 1}))
}
