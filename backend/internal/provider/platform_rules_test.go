package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestUsageMonitorPlatformsReturnsIndependentSelection mutates one returned
// candidate list before asking for another. It checks declaration order remains
// stable and callers cannot change later monitoring rounds through the slice.
func TestUsageMonitorPlatformsReturnsIndependentSelection(t *testing.T) {
	t.Parallel()
	platforms := usageMonitorPlatforms()
	require.Equal(t, []string{PlatformKimi, PlatformZhipu, PlatformDeepseek}, platforms)
	platforms[0] = PlatformOpenAI
	require.Equal(t, []string{PlatformKimi, PlatformZhipu, PlatformDeepseek}, usageMonitorPlatforms())
}

// TestResponsesOperationPathRetainsPlatformSuffixes compares the unversioned
// DeepSeek operation with versioned endpoints and an unknown platform. It checks
// the shared URL builder receives the existing suffix for every declaration.
func TestResponsesOperationPathRetainsPlatformSuffixes(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/responses", ResponsesOperationPath(PlatformDeepseek))
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformOpenAI, PlatformOpenCode, "unknown"} {
		require.Equal(t, "/v1/responses", ResponsesOperationPath(platform))
	}
}

// TestRequiresStatelessResponses compares DeepSeek, Kimi, Zhipu, and
// OpenAI platform records, including Kimi coding mode. It checks that native
// Responses support follows platform identity for those supplied
// configurations.
func TestRequiresStatelessResponses(t *testing.T) {
	t.Parallel()
	require.True(t, (ProtocolTarget{Record: &Record{Platform: capability.PlatformDeepseek}}).RequiresStatelessResponses())
	require.True(t, (ProtocolTarget{Record: &Record{Platform: capability.PlatformKimi}}).RequiresStatelessResponses())
	require.True(t, (ProtocolTarget{Record: &Record{Platform: capability.PlatformKimi, Credentials: map[string]any{"provider_mode": ProviderModeCoding}}}).RequiresStatelessResponses())
	require.False(t, (ProtocolTarget{Record: &Record{Platform: capability.PlatformZhipu}}).RequiresStatelessResponses())
	require.False(t, (ProtocolTarget{Record: &Record{Platform: capability.PlatformOpenAI}}).RequiresStatelessResponses())
}
