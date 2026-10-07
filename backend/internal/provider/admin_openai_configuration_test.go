package provider_test

import (
	"context"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestUpdateProviderLegacyPatchOverridesEchoedNewShape(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	provider := &providercore.Record{
		Name:     "openai-provider",
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "secret",
			providercore.OpenAIWorkloadCapabilitiesCredentialKey: []any{"embeddings"},
		},
		Extra: map[string]any{
			providercore.ExtraKeyTextRouteMode:                  "preserve_client_protocol",
			"openai_responses_probe_status":                     "supported",
			providercore.ExtraKeyResponsesContinuationSupported: true,
		},
	}
	require.NoError(t, repo.Create(ctx, provider))
	svc := newProviderEditorForTest(repo)

	updated, err := svc.UpdateProvider(ctx, provider.ID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{
			providercore.LegacyOpenAICapabilitiesCredentialKey: []any{"chat_completions"},
		},
		Extra: map[string]any{
			providercore.ExtraKeyTextRouteMode:             "force_responses",
			providercore.LegacyOpenAIResponsesModeExtraKey: "force_chat_completions",
			"openai_responses_probe_status":                "supported",
			"openai_responses_supported":                   false,
		},
	})

	require.NoError(t, err)
	require.Contains(t, updated.UpstreamProtocols(), protocol.ProtocolOpenAIChatCompletions)
	require.NotContains(t, updated.UpstreamProtocols(), protocol.ProtocolOpenAIResponses)
	require.NotContains(t, updated.Credentials, providercore.LegacyOpenAICapabilitiesCredentialKey)
	require.NotContains(t, updated.Extra, providercore.ExtraKeyTextRouteMode)
	require.NotContains(t, updated.Extra, "openai_responses_probe_status")
	require.Equal(t, true, updated.Extra[providercore.ExtraKeyResponsesContinuationSupported])
	require.NotContains(t, updated.Extra, providercore.LegacyOpenAIResponsesModeExtraKey)
	require.NotContains(t, updated.Extra, "openai_responses_supported")
}
