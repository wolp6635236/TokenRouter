package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestRequiresBillableGrokChatUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider *ExecutionProvider
		models   []string
		want     bool
	}{
		{name: "grok platform", provider: &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok}}, models: []string{"alias"}, want: true},
		{name: "compatible Grok model", provider: &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"grok-4.5"}, want: true},
		{name: "mapped Grok model", provider: &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"alias", "grok-4.5"}, want: true},
		{name: "namespaced Grok model", provider: &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"x-ai/grok-4.5"}, want: true},
		{name: "ordinary OpenAI model", provider: &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, models: []string{"gpt-5.4"}, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, RequiresBillableGrokChatUsage(testCase.provider, testCase.models...))
		})
	}
}

func TestHasBillableGrokChatUsageRequiresAggregateToken(t *testing.T) {
	t.Parallel()

	require.False(t, HasBillableGrokChatUsage(openai.ForwardUsage{}))
	require.False(t, HasBillableGrokChatUsage(openai.ForwardUsage{ImageInputTokens: 2, ImageOutputTokens: 1}))
	require.True(t, HasBillableGrokChatUsage(openai.ForwardUsage{InputTokens: 1}))
	require.True(t, HasBillableGrokChatUsage(openai.ForwardUsage{OutputTokens: 1}))
	require.True(t, HasBillableGrokChatUsage(openai.ForwardUsage{CacheCreationInputTokens: 1}))
	require.True(t, HasBillableGrokChatUsage(openai.ForwardUsage{CacheReadInputTokens: 1}))
}
