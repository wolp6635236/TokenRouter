package provider

import (
	"testing"

	acct "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

func TestGetGrokBaseURLUsesSubscriptionProxyForOAuth(t *testing.T) {
	tests := []struct {
		name     string
		provider acct.Record
		expected string
	}{
		{
			name: "oauth without base_url uses CLI subscription proxy",
			provider: acct.Record{
				Type:        capability.ProviderTypeOAuth,
				Platform:    capability.PlatformGrok,
				Credentials: map[string]any{},
			},
			expected: xai.DefaultCLIBaseURL,
		},
		{
			name: "oauth stored official API endpoint is honored (manual endpoint switch)",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": xai.DefaultBaseURL,
				},
			},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth stored regional API endpoint is honored",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "https://us-west-2.api.x.ai/v1",
				},
			},
			expected: "https://us-west-2.api.x.ai/v1",
		},
		{
			name: "oauth stored CLI proxy is honored verbatim",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": xai.DefaultCLIBaseURL,
				},
			},
			expected: xai.DefaultCLIBaseURL,
		},
		{
			name: "oauth unparseable base_url falls back to CLI proxy",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "not a url",
				},
			},
			expected: xai.DefaultCLIBaseURL,
		},
		{
			name: "oauth explicit custom base_url redirects forwarding traffic",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "https://custom.example.com/v1",
				},
			},
			expected: "https://custom.example.com/v1",
		},
		{
			name: "oauth custom base_url with path prefix redirects forwarding traffic",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "https://relay.example.com/xai/v1",
				},
			},
			expected: "https://relay.example.com/xai/v1",
		},
		{
			name: "API key without base_url uses official credit-backed API",
			provider: acct.Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformGrok,
				Credentials: map[string]any{},
			},
			expected: xai.DefaultBaseURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, GrokProviderBaseURL(&tt.provider))
		})
	}
}

func TestGetGrokBaseURLHonorsOAuthCustomRegardlessOfUnsafeOverrides(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	provider := acct.Record{
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformGrok,
		Credentials: map[string]any{
			"base_url": "https://custom.example.com/v1",
		},
	}

	require.Equal(t, "https://custom.example.com/v1", GrokProviderBaseURL(&provider))
}

func TestGetGrokMediaBaseURLRedirectsCLIGatewayToOfficialAPI(t *testing.T) {
	tests := []struct {
		name     string
		provider acct.Record
		expected string
	}{
		{
			name: "oauth without base_url uses official media API",
			provider: acct.Record{
				Type:        capability.ProviderTypeOAuth,
				Platform:    capability.PlatformGrok,
				Credentials: map[string]any{},
			},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth stored CLI proxy is separated from the media API",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": xai.DefaultCLIBaseURL,
				},
			},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth stored CLI proxy variant is canonicalized to the media API",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "HTTPS://CLI-CHAT-PROXY.GROK.COM:443/%76%31/",
				},
			},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth unparseable base_url falls back to official media API",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "not a url",
				},
			},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth stored official API endpoint is honored (manual endpoint switch)",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": xai.DefaultBaseURL,
				},
			},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth stored regional API endpoint is honored for media",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "https://us-west-2.api.x.ai/v1",
				},
			},
			expected: "https://us-west-2.api.x.ai/v1",
		},
		{
			name: "oauth custom base_url redirects media traffic",
			provider: acct.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "https://custom.example.com/v1",
				},
			},
			expected: "https://custom.example.com/v1",
		},
		{
			name: "API key retains its configured media API",
			provider: acct.Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformGrok,
				Credentials: map[string]any{
					"base_url": "https://grok.example.com/v1",
				},
			},
			expected: "https://grok.example.com/v1",
		},
		{
			name: "non-Grok provider has no media base URL",
			provider: acct.Record{
				Type:        capability.ProviderTypeOAuth,
				Platform:    capability.PlatformOpenAI,
				Credentials: map[string]any{},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, GrokProviderMediaBaseURL(&tt.provider))
		})
	}
}

func TestGetGrokMediaBaseURLHonorsOAuthCustomRegardlessOfUnsafeOverrides(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	provider := acct.Record{
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformGrok,
		Credentials: map[string]any{
			"base_url": "https://custom.example.com/v1",
		},
	}

	require.Equal(t, "https://custom.example.com/v1", GrokProviderMediaBaseURL(&provider))
}
