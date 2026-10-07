package provider_test

import (
	"testing"
	"time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// TestGrokStreamIdleFailoverError 验证 Grok 流空闲超时的故障转移错误。
func TestGrokStreamIdleFailoverError(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	err := gatewayprovider.GrokStreamIdleFailure(provider, 180*time.Second)
	require.NotNil(t, err)
	require.Equal(t, 502, err.StatusCode)
	require.True(t, err.SafeToFailoverAfterWrite)
	require.True(t, err.RetryableOnSameProvider)
	require.True(t, err.RequestScopedTransient)
	require.Equal(t, 1, err.SameProviderRetryMax)
	require.Contains(t, string(err.ResponseBody), "empty_upstream")
	require.WithinDuration(t, time.Now().Add(180*time.Second), err.SameProviderRetryDeadline, 2*time.Second)
}

func TestGrokStreamIdleFailoverErrorRequiresGrokProvider(t *testing.T) {
	openAI := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}
	err := gatewayprovider.GrokStreamIdleFailure(openAI, time.Second)
	require.False(t, err.RetryableOnSameProvider)
	require.True(t, err.RequestScopedTransient)
}
