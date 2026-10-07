package provider_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// TestUpdateProviderDeprecatedProbeOnlyPreservesConfiguration 验证只有废弃键的单提供商更新不得覆盖管理员保存的路由和两个压缩开关。
func TestUpdateProviderDeprecatedProbeOnlyPreservesConfiguration(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	provider := &providercore.Record{
		Name: "manual", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "test"},
		Extra:       map[string]any{"openai_text_route_mode": "force_responses", "openai_compact_mode": "force_off", providercore.OpenAINativeCompactionV2ModeExtraKey: "force_on", "keep": true},
	}
	require.NoError(t, repo.Create(ctx, provider))
	svc := newProviderEditorForTest(repo)
	updated, err := svc.UpdateProvider(ctx, provider.ID, &providercore.UpdateProviderInput{Extra: map[string]any{"openai_responses_supported": false}})
	require.NoError(t, err)
	require.Contains(t, updated.UpstreamProtocols(), protocol.ProtocolOpenAIResponses)
	require.NotContains(t, updated.UpstreamProtocols(), protocol.ProtocolOpenAIChatCompletions)
	require.Equal(t, "force_off", updated.Extra["openai_compact_mode"])
	require.Equal(t, "force_on", updated.Extra[providercore.OpenAINativeCompactionV2ModeExtraKey])
	require.Equal(t, true, updated.Extra["keep"])
	require.NotContains(t, updated.Extra, "openai_responses_supported")
}
