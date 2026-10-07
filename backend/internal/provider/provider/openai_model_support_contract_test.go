package provider

import (
	"testing"

	acct "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func newOpenAIOAuthProviderForModelTest() *acct.Record {
	return &acct.Record{
		ID:       1,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
	}
}

func TestIsModelSupported_OpenAIOAuthEmptyMapping_UsesDefaultDirectory(t *testing.T) {
	provider := newOpenAIOAuthProviderForModelTest()
	for _, model := range []string{"gpt-5.4", "gpt-5.6-terra", "gpt-5.6-sol"} {
		require.True(t, provider.IsModelSupported(model, ModelDefaults(), ModelRules(provider)), model)
	}
	for _, model := range []string{"", "my-custom-alias", "model-outside-current-catalog"} {
		require.False(t, provider.IsModelSupported(model, ModelDefaults(), ModelRules(provider)), model)
	}
}

func TestIsModelSupported_OpenAIOAuthEmptyMapping_RejectsForeignModels(t *testing.T) {
	provider := newOpenAIOAuthProviderForModelTest()

	// Codex 上游必然以不可重试的 400 拒绝这些厂商家族，调度阶段就应跳过
	// 该提供商，让明确支持的 API Key 提供商接手。
	foreign := []string{
		"deepseek-v4",
		"deepseek-chat",
		"glm-4.7",
		"kimi-k2",
		"k3",
		"k3-256k",
		"moonshot-v1-128k",
		"gemini-3.0-pro",
		"grok-4",
		"qwen3-max",
		"minimax-m2.5",
		"llama-3.3-70b",
		"provider/deepseek-v4", // vendor/model 形式取最后一段判定。
		"provider/k3",          // Kimi Code bare ID 的 vendor/model 形式。
	}
	for _, model := range foreign {
		require.False(t, provider.IsModelSupported(model, ModelDefaults(), ModelRules(provider)), "expected %q to be rejected by empty-mapping OpenAI OAuth provider", model)
	}
}

func TestIsModelSupported_OpenAIOAuthMappingKeepsForkSemantics(t *testing.T) {
	provider := newOpenAIOAuthProviderForModelTest()
	provider.Credentials = map[string]any{
		"model_mapping": map[string]any{"deepseek-v4": "gpt-5.4", "k3": "gpt-5.4"},
	}

	// 映射可以引入别名；未命中的模型仍受默认目录和认证能力限制。
	require.True(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
	require.True(t, provider.IsModelSupported("k3", ModelDefaults(), ModelRules(provider)))
	require.False(t, provider.IsModelSupported("glm-4.7", ModelDefaults(), ModelRules(provider)))

	provider.Credentials["model_whitelist"] = []any{"gpt-5.4"}
	require.True(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
	require.False(t, provider.IsModelSupported("glm-4.7", ModelDefaults(), ModelRules(provider)))
}

func TestIsModelSupported_OpenAIOAuthEmptyMappingRespectsWhitelist(t *testing.T) {
	provider := newOpenAIOAuthProviderForModelTest()
	provider.Credentials = map[string]any{"model_whitelist": []any{"gpt-5.4", "deepseek-v4"}}

	// 最终白名单仍先限制允许范围，但不能让 Codex 上游无法服务的厂商模型绕过平台限制。
	require.True(t, provider.IsModelSupported("gpt-5.4", ModelDefaults(), ModelRules(provider)))
	require.False(t, provider.IsModelSupported("gpt-5.3-codex", ModelDefaults(), ModelRules(provider)))
	require.False(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
}

func TestIsModelSupported_OpenAIOAuthPassthroughKeepsModelScope(t *testing.T) {
	provider := newOpenAIOAuthProviderForModelTest()
	provider.Extra = map[string]any{"openai_passthrough": true}
	require.False(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
	provider.Credentials = map[string]any{"model_whitelist": []string{"*"}}
	require.False(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
}

func TestIsModelSupported_OpenAIAPIKeyRequiresExplicitCustomScope(t *testing.T) {
	provider := &acct.Record{ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}
	require.False(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
	require.True(t, provider.IsModelSupported("gpt-5.4", ModelDefaults(), ModelRules(provider)))
	provider.Credentials = map[string]any{"model_whitelist": []string{"*"}}
	require.True(t, provider.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(provider)))
}

func TestIsModelSupported_AnthropicDefaultsRejectForeignModel(t *testing.T) {
	anthropic := &acct.Record{ID: 3, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}
	require.True(t, anthropic.IsModelSupported("claude-sonnet-4-6", ModelDefaults(), ModelRules(anthropic)))
	require.False(t, anthropic.IsModelSupported("deepseek-v4", ModelDefaults(), ModelRules(anthropic)))
}
