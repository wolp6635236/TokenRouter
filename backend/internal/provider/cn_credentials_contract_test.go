package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestGetOpenAIProtocolAPIKey_CNProviders 验证 OpenAI 协议族密钥读取覆盖国产供应商，
// IsOpenAIApiKey 在 OpenAI 平台返回 true，调度倍率和 WS 准入按平台判断。
func TestGetOpenAIProtocolAPIKey_CNProviders(t *testing.T) {
	t.Parallel()

	kimi := &Record{
		Platform:    capability.PlatformKimi,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-kimi"},
	}
	require.Equal(t, "sk-kimi", kimi.GetOpenAIProtocolAPIKey())
	require.False(t, kimi.IsOpenAIApiKey(), "IsOpenAIApiKey stays openai-only for scheduling gates")

	// 非 APIKey 类型的 CN 提供商不返回密钥
	notAPIKey := &Record{
		Platform:    capability.PlatformDeepseek,
		Type:        capability.ProviderTypeOAuth,
		Credentials: map[string]any{"api_key": "sk-leak"},
	}
	require.Equal(t, "", notAPIKey.GetOpenAIProtocolAPIKey())

	// OpenAI 提供商使用 OpenAI API Key 分支。
	openai := &Record{
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-openai"},
	}
	require.Equal(t, "sk-openai", openai.GetOpenAIProtocolAPIKey())
}

func TestCNProviderProviderModeAndCredentialValidation(t *testing.T) {
	t.Parallel()
	historical := &Record{
		Platform:    capability.PlatformKimi,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
	}
	require.Equal(t, ProviderModePayG, historical.GetProviderMode())
	require.Equal(t, APIProtocolChatCompletions, (ProtocolTarget{Record: historical}).GetAPIProtocol())
	require.NoError(t, NormalizeCNProviderCredentials(historical, false))
	require.NotContains(t, historical.Credentials, "provider_mode", "编辑历史提供商不应强制回写默认字段")

	created := &Record{
		Platform:    capability.PlatformZhipu,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
	}
	require.NoError(t, NormalizeCNProviderCredentials(created, true))
	require.Equal(t, ProviderModePayG, created.Credentials["provider_mode"])
	require.Equal(t, APIProtocolChatCompletions, created.Credentials["api_protocol"])

	invalidResponses := &Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": APIProtocolResponses},
	}
	require.Error(t, NormalizeCNProviderCredentials(invalidResponses, false))
	invalidType := &Record{Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeOAuth}
	require.Error(t, NormalizeCNProviderCredentials(invalidType, false))
}
