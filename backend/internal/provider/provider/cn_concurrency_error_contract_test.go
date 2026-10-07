package provider

import (
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	upstreamkimi "github.com/TokenFlux/TokenRouter/internal/upstream/kimi"
	"github.com/stretchr/testify/require"
)

func TestIsCNProviderConcurrencyLimit403_ExactClassification(t *testing.T) {
	kimi := &providercore.Record{Platform: capability.PlatformKimi}

	require.True(t, CNConcurrencyLimit403(kimi, upstreamkimi.ConcurrentRequestLimitMessage))
	require.True(t, CNConcurrencyLimit403(kimi, "  "+upstreamkimi.ConcurrentRequestLimitMessage+"\n"))

	for name, tc := range map[string]struct {
		provider *providercore.Record
		message  string
	}{
		"permission denied":              {kimi, "You do not have permission to access this resource."},
		"generic concurrency wording":    {kimi, "concurrent request limit reached"},
		"near match missing punctuation": {kimi, "You've reached your concurrent request limit. Please wait for your ongoing requests to finish and try again"},
		"other CN provider":              {&providercore.Record{Platform: capability.PlatformZhipu}, upstreamkimi.ConcurrentRequestLimitMessage},
		"non CN provider":                {&providercore.Record{Platform: capability.PlatformOpenAI}, upstreamkimi.ConcurrentRequestLimitMessage},
		"nil provider":                   {nil, upstreamkimi.ConcurrentRequestLimitMessage},
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, CNConcurrencyLimit403(tc.provider, tc.message))
		})
	}
}
