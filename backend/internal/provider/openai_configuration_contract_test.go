package provider_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/stretchr/testify/require"
)

// TestBuildProviderForCreateNormalizesLegacyOpenAIConfigurationForCreateAndImport 检查通用导入调用 CreateProvider，创建时清理废弃 OpenAI 配置。
func TestBuildProviderForCreateNormalizesLegacyOpenAIConfigurationForCreateAndImport(t *testing.T) {
	input := &providercore.CreateProviderInput{
		Name:     "legacy-openai",
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "secret",
			"openai_capabilities": map[string]any{
				"chat_completions": true,
				"embeddings":       false,
			},
		},
	}
	extra := map[string]any{
		"openai_responses_mode":      "force_responses",
		"openai_responses_supported": false,
		"unrelated":                  map[string]any{"keep": true},
	}

	provider, err := providercore.BuildProviderForCreate(input, extra, providercore.CreationOptions{Now: time.Now, LoadLocation: time.LoadLocation, NewSeed: uuid.NewString})

	require.NoError(t, err)
	require.Contains(t, provider.UpstreamProtocols(), protocol.ProtocolOpenAIResponses)
	require.NotContains(t, provider.UpstreamProtocols(), protocol.ProtocolOpenAIChatCompletions)
	require.NotContains(t, provider.Extra, providercore.ExtraKeyTextRouteMode)
	require.NotContains(t, provider.Extra, "openai_responses_probe_status")
	require.Equal(t, false, provider.Extra[providercore.ExtraKeyResponsesContinuationSupported])
	require.Equal(t, map[string]any{"keep": true}, provider.Extra["unrelated"])
	require.NotContains(t, provider.Credentials, providercore.LegacyOpenAICapabilitiesCredentialKey)
	require.NotContains(t, provider.Extra, providercore.LegacyOpenAIResponsesModeExtraKey)
	require.NotContains(t, provider.Extra, "openai_responses_supported")
}

func TestNormalizeOpenAIAPIKeyConfigurationDefaultsAndExplicitEmpty(t *testing.T) {
	t.Run("缺失配置写入显式默认值", func(t *testing.T) {
		provider := &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}

		require.NoError(t, providercore.NormalizeOpenAIAPIKeyConfiguration(provider))
		require.Equal(t, []string{"text_generation", "embeddings"}, provider.Credentials[providercore.OpenAIWorkloadCapabilitiesCredentialKey])
		require.Equal(t, "preserve_client_protocol", provider.Extra[providercore.ExtraKeyTextRouteMode])
		require.NotContains(t, provider.Extra, "openai_responses_probe_status")
		require.Equal(t, false, provider.Extra[providercore.ExtraKeyResponsesContinuationSupported])
	})

	t.Run("显式空能力集合保持为空", func(t *testing.T) {
		provider := &providercore.Record{
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				providercore.LegacyOpenAICapabilitiesCredentialKey: []any{},
			},
		}

		require.NoError(t, providercore.NormalizeOpenAIAPIKeyConfiguration(provider))
		require.Equal(t, []string{}, provider.Credentials[providercore.OpenAIWorkloadCapabilitiesCredentialKey])
		require.False(t, provider.SupportsOpenAIEndpointCapability(providercore.OpenAIEndpointCapabilityTextGeneration, nil))
		require.False(t, provider.SupportsOpenAIEndpointCapability(providercore.OpenAIEndpointCapabilityEmbeddings, nil))
	})
}

func TestNormalizeOpenAIAPIKeyConfigurationPatchSupportsLegacyBulkPayload(t *testing.T) {
	credentials := map[string]any{
		providercore.LegacyOpenAICapabilitiesCredentialKey: []any{"chat_completions", "embeddings"},
	}
	extra := map[string]any{
		providercore.LegacyOpenAIResponsesModeExtraKey: "auto",
		"openai_responses_supported":                   true,
	}

	require.NoError(t, providercore.NormalizeOpenAIAPIKeyConfigurationPatch(credentials, extra))
	require.Equal(t, []string{"text_generation", "embeddings"}, credentials[providercore.OpenAIWorkloadCapabilitiesCredentialKey])
	require.Equal(t, "preserve_client_protocol", extra[providercore.ExtraKeyTextRouteMode])
	require.NotContains(t, extra, "openai_responses_probe_status")
	require.NotContains(t, credentials, providercore.LegacyOpenAICapabilitiesCredentialKey)
	require.NotContains(t, extra, providercore.LegacyOpenAIResponsesModeExtraKey)
	require.NotContains(t, extra, "openai_responses_supported")
}

func TestNormalizeOpenAIResponsesContinuationSupported(t *testing.T) {
	t.Run("explicit values are preserved", func(t *testing.T) {
		extra := map[string]any{
			providercore.ExtraKeyResponsesContinuationSupported: true,
		}
		require.NoError(t, providercore.NormalizeOpenAIAPIKeyConfigurationPatch(nil, extra))
		require.Equal(t, true, extra[providercore.ExtraKeyResponsesContinuationSupported])

		extra[providercore.ExtraKeyResponsesContinuationSupported] = false
		require.NoError(t, providercore.NormalizeOpenAIAPIKeyConfigurationPatch(nil, extra))
		require.Equal(t, false, extra[providercore.ExtraKeyResponsesContinuationSupported])
	})

	t.Run("null becomes false", func(t *testing.T) {
		extra := map[string]any{
			providercore.ExtraKeyResponsesContinuationSupported: nil,
		}
		require.NoError(t, providercore.NormalizeOpenAIAPIKeyConfigurationPatch(nil, extra))
		require.Equal(t, false, extra[providercore.ExtraKeyResponsesContinuationSupported])
	})

	t.Run("invalid type is rejected", func(t *testing.T) {
		extra := map[string]any{
			providercore.ExtraKeyResponsesContinuationSupported: "true",
		}
		err := providercore.NormalizeOpenAIAPIKeyConfigurationPatch(nil, extra)
		require.Error(t, err)
		require.Contains(t, err.Error(), "OPENAI_RESPONSES_CONTINUATION_INVALID")
	})
}

func TestNormalizeOpenAITextRouteModeRejectsInvalidNewValue(t *testing.T) {
	extra := map[string]any{providercore.ExtraKeyTextRouteMode: "auto"}

	err := providercore.NormalizeOpenAIAPIKeyConfigurationPatch(nil, extra)

	require.Error(t, err)
}
