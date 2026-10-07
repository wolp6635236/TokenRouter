package app

import (
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

func TestGrokAPIKeyURLPolicyFollowsGlobalSecurityConfig(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"base_url": "http://grok.example.test/v1",
			},
		},
	}

	t.Run("insecure HTTP enabled with allowlist disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = false
		cfg.Security.URLAllowlist.AllowInsecureHTTP = true

		responsesURL, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, "http://grok.example.test/v1/responses", responsesURL)

		chatURL, err := provideGrokRoutes(cfg, nil).Chat(provider, false)
		require.NoError(t, err)
		require.Equal(t, "http://grok.example.test/v1/chat/completions", chatURL)

		mediaURL, err := provideGrokRoutes(cfg, nil).Media(provider, xai.GrokMediaEndpointImagesGenerations, "")
		require.NoError(t, err)
		require.Equal(t, "http://grok.example.test/v1/images/generations", mediaURL)

		contentURL, err := provideGrokRoutes(cfg, nil).Media(provider, xai.GrokMediaEndpointVideoContent, "request 123")
		require.NoError(t, err)
		require.Equal(t, "http://grok.example.test/v1/videos/request%20123/content", contentURL)
	})

	t.Run("insecure HTTP disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = false
		cfg.Security.URLAllowlist.AllowInsecureHTTP = false

		_, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")
	})

	t.Run("enabled allowlist remains HTTPS only", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.AllowInsecureHTTP = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"grok.example.test"}

		_, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")
	})
}

func TestGrokAPIKeyURLPolicyAppliesAllowlistAndPrivateHostControls(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"base_url": "https://grok.example.test/v1",
			},
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"grok.example.test"}

	target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
	require.NoError(t, err)
	require.Equal(t, "https://grok.example.test/v1/responses", target)

	cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}
	_, err = provideGrokRoutes(cfg, nil).Responses(provider, false)
	require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")

	provider.Record.Credentials["base_url"] = "https://127.0.0.1/v1"
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	_, err = provideGrokRoutes(cfg, nil).Responses(provider, false)
	require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")

	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	target, err = provideGrokRoutes(cfg, nil).Responses(provider, false)
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1/v1/responses", target)
}

func TestGrokAPIKeyURLPolicyRedactsMalformedConfiguredURL(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"base_url": "https://%zz:secret@grok.example.test/v1",
			},
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	_, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
	require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")
	require.NotContains(t, err.Error(), "secret")
}

func TestGrokOAuthURLPolicy(t *testing.T) {
	t.Run("default CLI gateway always allowed under restrictive allowlist", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Credentials: map[string]any{},
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}

		target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultCLIBaseURL+"/responses", target)
	})

	t.Run("stored official API endpoint is honored (manual endpoint switch)", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": xai.DefaultBaseURL,
				},
			},
		}
		cfg := &config.Config{}

		target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultBaseURL+"/responses", target)
	})

	t.Run("stored regional API endpoint is trusted even under restrictive allowlist", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": "https://us-west-2.api.x.ai/v1",
				},
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}

		target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, "https://us-west-2.api.x.ai/v1/responses", target)
	})

	t.Run("custom forwarding address follows operator policy", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": "https://relay.example.test/v1",
				},
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = false

		target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example.test/v1/responses", target)
	})

	t.Run("custom path prefix is preserved", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": "https://relay.example.test/xai/v1",
				},
			},
		}
		cfg := &config.Config{}

		target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example.test/xai/v1/responses", target)
	})

	t.Run("custom forwarding address rejected by allowlist", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": "https://relay.example.test/v1",
				},
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}

		_, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")
	})

	t.Run("insecure HTTP custom address requires operator opt-in", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": "http://relay.example.test/v1",
				},
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = false
		cfg.Security.URLAllowlist.AllowInsecureHTTP = false

		_, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")

		cfg.Security.URLAllowlist.AllowInsecureHTTP = true
		target, err := provideGrokRoutes(cfg, nil).Responses(provider, false)
		require.NoError(t, err)
		require.Equal(t, "http://relay.example.test/v1/responses", target)
	})

	t.Run("unsafe override switch does not relax the operator allowlist for custom hosts", func(t *testing.T) {
		// XAI_ALLOW_UNSAFE_URL_OVERRIDES 会放宽受信任主机校验，但自定义 OAuth 转发主机
		// 仍按运营方白名单选择转发目标，bearer token 随请求发送到该目标。
		t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"cli-chat-proxy.grok.com"}

		custom := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{
					"base_url": "http://10.0.0.1/v1",
				},
			},
		}
		_, err := provideGrokRoutes(cfg, nil).Responses(custom, false)
		require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")

		// 白名单限制自定义主机时，官方网关照常解析。
		official := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Credentials: map[string]any{},
			},
		}
		target, err := provideGrokRoutes(cfg, nil).Responses(official, false)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultCLIBaseURL+"/responses", target)
	})
}

func TestGrokOAuthMediaURLFollowsCustomForwardingUpstream(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://custom.example.test/v1",
			},
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	target, err := provideGrokRoutes(cfg, nil).Media(provider, xai.GrokMediaEndpointVideosGenerations, "")
	require.NoError(t, err)
	require.Equal(t, "https://custom.example.test/v1/videos/generations", target)
}
