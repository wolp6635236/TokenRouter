package provider_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestGetAPIProtocol 验证协议凭证维度的平台校验矩阵：
// DeepSeek 和 Kimi 支持 Responses，字段缺失或无效时回退 Chat Completions。
func TestGetAPIProtocol(t *testing.T) {
	t.Parallel()

	mk := func(platform, protocol string) providercore.ProtocolTarget {
		creds := map[string]any{"api_key": "sk-test"}
		if protocol != "" {
			creds["api_protocol"] = protocol
		}
		return providercore.ProtocolTarget{Record: &providercore.Record{Platform: platform, Type: capability.ProviderTypeAPIKey, Credentials: creds}}
	}

	require.Equal(t, providercore.APIProtocolChatCompletions, mk(capability.PlatformKimi, "").GetAPIProtocol(), "缺失回退默认")
	require.Equal(t, providercore.APIProtocolAnthropic, mk(capability.PlatformZhipu, providercore.APIProtocolAnthropic).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolAnthropic, mk(capability.PlatformKimi, providercore.APIProtocolAnthropic).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolAnthropic, mk(capability.PlatformDeepseek, providercore.APIProtocolAnthropic).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolResponses, mk(capability.PlatformDeepseek, providercore.APIProtocolResponses).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolResponses, mk(capability.PlatformKimi, providercore.APIProtocolResponses).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolAdaptive, mk(capability.PlatformKimi, providercore.APIProtocolAdaptive).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolAdaptive, mk(capability.PlatformZhipu, providercore.APIProtocolAdaptive).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolAdaptive, mk(capability.PlatformDeepseek, providercore.APIProtocolAdaptive).GetAPIProtocol())
	require.Equal(t, providercore.APIProtocolChatCompletions, mk(capability.PlatformZhipu, providercore.APIProtocolResponses).GetAPIProtocol(), "zhipu 无 responses 端点")
	require.Equal(t, providercore.APIProtocolChatCompletions, mk(capability.PlatformKimi, "bogus").GetAPIProtocol(), "非法值回退默认")
	require.Equal(t, providercore.APIProtocolChatCompletions, (providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}).GetAPIProtocol(), "非 CN 供应商恒为默认")
}

func TestGetCodingPlanProviderUsesPlatformInsteadOfURL(t *testing.T) {
	t.Parallel()
	kimi := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"provider_mode": providercore.ProviderModeCoding,
			"base_url":      "https://relay.example/custom",
		},
	}}
	require.Equal(t, capability.PlatformKimi, kimi.GetCodingPlanProvider())
	zhipu := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"provider_mode": providercore.ProviderModeCoding,
			"base_url":      "https://api.kimi.com/coding/v1",
		},
	}}
	require.Equal(t, capability.PlatformZhipu, zhipu.GetCodingPlanProvider(), "中继路径不得改变平台身份")
}

func TestSupportsNativeCNResponses(t *testing.T) {
	t.Parallel()
	require.True(t, (providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformDeepseek}}).SupportsNativeCNResponses())
	require.True(t, (providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformKimi}}).SupportsNativeCNResponses())
	require.True(t, (providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformKimi, Credentials: map[string]any{"provider_mode": providercore.ProviderModeCoding}}}).SupportsNativeCNResponses())
	require.False(t, (providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformZhipu}}).SupportsNativeCNResponses())
	require.False(t, (providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformOpenAI}}).SupportsNativeCNResponses())
}

func TestAdaptiveProtocolBaseURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		platform      string
		mode          string
		wantChat      string
		wantAnthropic string
		wantResponses string
	}{
		{"kimi payg", capability.PlatformKimi, providercore.ProviderModePayG, providercore.DefaultKimiPayGBaseURL, providercore.DefaultKimiPayGAnthropicBaseURL, providercore.DefaultKimiPayGBaseURL},
		{"kimi coding", capability.PlatformKimi, providercore.ProviderModeCoding, providercore.DefaultKimiCodingBaseURL, providercore.DefaultKimiCodingAnthropicBaseURL, providercore.DefaultKimiCodingBaseURL},
		{"zhipu payg", capability.PlatformZhipu, providercore.ProviderModePayG, providercore.DefaultZhipuPayGBaseURL, providercore.DefaultZhipuAnthropicBaseURL, providercore.DefaultZhipuPayGBaseURL},
		{"zhipu coding", capability.PlatformZhipu, providercore.ProviderModeCoding, providercore.DefaultZhipuCodingBaseURL, providercore.DefaultZhipuAnthropicBaseURL, providercore.DefaultZhipuCodingBaseURL},
		{"deepseek", capability.PlatformDeepseek, providercore.ProviderModePayG, providercore.DefaultDeepseekBaseURL, providercore.DefaultDeepseekAnthropicBaseURL, providercore.DefaultDeepseekBaseURL},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := providercore.ProtocolTarget{Record: &providercore.Record{Platform: tc.platform, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
				"api_protocol":  providercore.APIProtocolAdaptive,
				"provider_mode": tc.mode,
			}}}
			require.Equal(t, tc.wantChat, provider.GetCNProtocolBaseURL(providercore.APIProtocolChatCompletions))
			require.Equal(t, tc.wantAnthropic, provider.GetCNProtocolBaseURL(providercore.APIProtocolAnthropic))
			require.Equal(t, tc.wantResponses, provider.GetCNProtocolBaseURL(providercore.APIProtocolResponses))
			require.Equal(t, tc.wantAnthropic, provider.GetAnthropicProtocolBaseURL())
		})
	}
}

func TestAdaptiveProtocolBaseURLOverrides(t *testing.T) {
	t.Parallel()

	provider := providercore.ProtocolTarget{Record: &providercore.Record{Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
		"api_protocol": providercore.APIProtocolAdaptive,
		"base_url":     "https://legacy-chat.example.com",
		"api_base_urls": map[string]any{
			providercore.APIProtocolChatCompletions: "https://chat.example.com",
			providercore.APIProtocolAnthropic:       "https://anthropic.example.com",
			providercore.APIProtocolResponses:       "https://responses.example.com",
		},
	}}}

	require.Equal(t, "https://chat.example.com", provider.GetOpenAIBaseURL())
	require.Equal(t, "https://chat.example.com", provider.GetCNProtocolBaseURL(providercore.APIProtocolChatCompletions))
	require.Equal(t, "https://anthropic.example.com", provider.GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://responses.example.com", provider.GetCNProtocolBaseURL(providercore.APIProtocolResponses))
}

// TestAnthropicProtocolBaseURL 验证 Anthropic 协议默认端点与协议感知的
// OpenAI 格式 base 回退。
func TestAnthropicProtocolBaseURL(t *testing.T) {
	t.Parallel()

	// 默认端点（按供应商 × 模式）
	require.Equal(t, "https://api.moonshot.cn/anthropic", (providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": providercore.APIProtocolAnthropic},
	}}).GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://api.kimi.com/coding", (providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": providercore.APIProtocolAnthropic, "provider_mode": providercore.ProviderModeCoding},
	}}).GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://open.bigmodel.cn/api/anthropic", (providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": providercore.APIProtocolAnthropic},
	}}).GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://api.deepseek.com/anthropic", (providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": providercore.APIProtocolAnthropic},
	}}).GetAnthropicProtocolBaseURL())

	// 凭证 base_url 覆盖默认值
	require.Equal(t, "https://custom.example.com/anthropic", (providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": providercore.APIProtocolAnthropic, "base_url": "https://custom.example.com/anthropic"},
	}}).GetAnthropicProtocolBaseURL())

	// 非 Anthropic 协议返回空串
	require.Empty(t, (providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://open.bigmodel.cn/api/paas/v4"},
	}}).GetAnthropicProtocolBaseURL())
}

// TestGetOpenAIFormatBaseURL_ProtocolAware 验证 Anthropic 协议提供商的 OpenAI
// 格式路径：官方端点映射到默认 CC base，自定义中继保留 host 与路径前缀。
func TestGetOpenAIFormatBaseURL_ProtocolAware(t *testing.T) {
	t.Parallel()

	zhipuAnthropic := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": providercore.APIProtocolAnthropic,
			"base_url":     "https://open.bigmodel.cn/api/anthropic",
		},
	}}
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4", zhipuAnthropic.GetOpenAIFormatBaseURL())

	kimiCodingAnthropic := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol":  providercore.APIProtocolAnthropic,
			"provider_mode": providercore.ProviderModeCoding,
			"base_url":      "https://api.kimi.com/coding",
		},
	}}
	require.Equal(t, "https://api.kimi.com/coding/v1", kimiCodingAnthropic.GetOpenAIFormatBaseURL())

	deepseekRelay := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": providercore.APIProtocolAnthropic,
			"base_url":     "https://relay.example.com/proxy/deepseek/anthropic/",
		},
	}}
	require.Equal(t, "https://relay.example.com/proxy/deepseek", deepseekRelay.GetOpenAIFormatBaseURL())

	kimiRelay := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": providercore.APIProtocolAnthropic,
			"base_url":     "https://relay.example.com/moonshot/anthropic",
		},
	}}
	require.Equal(t, "https://relay.example.com/moonshot", kimiRelay.GetOpenAIFormatBaseURL())

	// chat_completions 协议下行为不变（凭证 base_url 原样返回）
	ccProvider := providercore.ProtocolTarget{Record: &providercore.Record{
		Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://ds-relay.example.com"},
	}}
	require.Equal(t, "https://ds-relay.example.com", ccProvider.GetOpenAIFormatBaseURL())
}
