package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestApplyOpenCodeSessionHeaderTrustBoundary(t *testing.T) {
	tests := []struct {
		name      string
		provider  *gatewayprovider.ExecutionProvider
		targetURL string
		incoming  string
		want      string
	}{
		{
			name:      "official origin",
			provider:  openCodeSessionTestProvider("https://opencode.ai/zen/v1"),
			targetURL: "https://opencode.ai/zen/v1/chat/completions",
			incoming:  " conversation-123 ",
			want:      "conversation-123",
		},
		{
			name:      "lookalike origin",
			provider:  openCodeSessionTestProvider("https://opencode.ai.evil.example/v1"),
			targetURL: "https://opencode.ai.evil.example/v1/responses",
			incoming:  "conversation-123",
		},
		{
			name:      "subdomain is not implicitly trusted",
			provider:  openCodeSessionTestProvider("https://api.opencode.ai/v1"),
			targetURL: "https://api.opencode.ai/v1/responses",
			incoming:  "conversation-123",
		},
		{
			name:      "insecure official origin",
			provider:  openCodeSessionTestProvider("http://opencode.ai/zen/v1"),
			targetURL: "http://opencode.ai/zen/v1/responses",
			incoming:  "conversation-123",
		},
		{
			name:      "missing caller value",
			provider:  openCodeSessionTestProvider("https://opencode.ai/zen/v1"),
			targetURL: "https://opencode.ai/zen/v1/responses",
		},
		{
			name:      "oauth provider",
			provider:  &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}},
			targetURL: "https://opencode.ai/zen/v1/responses",
			incoming:  "conversation-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := make(http.Header)
			ApplyOpenCodeSessionHeader(newOpenCodeSessionTestContext(t, tt.incoming), tt.provider, tt.targetURL, headers)
			require.Equal(t, tt.want, headers.Get("X-OpenCode-Session"))
		})
	}
}
