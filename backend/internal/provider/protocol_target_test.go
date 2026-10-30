package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestGetAPIProtocol supplies supported, absent, and invalid legacy variants
// across Kimi, Zhipu, and DeepSeek platforms. It checks recognized routing
// modes, Zhipu's Responses fallback, and default Chat selection on an OpenAI
// record.
func TestGetAPIProtocol(t *testing.T) {
	t.Parallel()

	mk := func(platform, protocol string) ProtocolTarget {
		creds := map[string]any{"api_key": "sk-test"}
		if protocol != "" {
			creds["api_protocol"] = protocol
		}
		return ProtocolTarget{Record: &Record{Platform: platform, Type: capability.ProviderTypeAPIKey, Credentials: creds}}
	}

	require.Equal(t, APIProtocolChatCompletions, mk(capability.PlatformKimi, "").GetAPIProtocol(), "缺失回退默认")
	require.Equal(t, APIProtocolAnthropic, mk(capability.PlatformZhipu, APIProtocolAnthropic).GetAPIProtocol())
	require.Equal(t, APIProtocolAnthropic, mk(capability.PlatformKimi, APIProtocolAnthropic).GetAPIProtocol())
	require.Equal(t, APIProtocolAnthropic, mk(capability.PlatformDeepseek, APIProtocolAnthropic).GetAPIProtocol())
	require.Equal(t, APIProtocolResponses, mk(capability.PlatformDeepseek, APIProtocolResponses).GetAPIProtocol())
	require.Equal(t, APIProtocolResponses, mk(capability.PlatformKimi, APIProtocolResponses).GetAPIProtocol())
	require.Equal(t, APIProtocolAdaptive, mk(capability.PlatformKimi, APIProtocolAdaptive).GetAPIProtocol())
	require.Equal(t, APIProtocolAdaptive, mk(capability.PlatformZhipu, APIProtocolAdaptive).GetAPIProtocol())
	require.Equal(t, APIProtocolAdaptive, mk(capability.PlatformDeepseek, APIProtocolAdaptive).GetAPIProtocol())
	require.Equal(t, APIProtocolChatCompletions, mk(capability.PlatformZhipu, APIProtocolResponses).GetAPIProtocol(), "zhipu 无 responses 端点")
	require.Equal(t, APIProtocolChatCompletions, mk(capability.PlatformKimi, "bogus").GetAPIProtocol(), "非法值回退默认")
	require.Equal(t, APIProtocolChatCompletions, (ProtocolTarget{Record: &Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}).GetAPIProtocol(), "OpenAI providers use the default Chat protocol")
}

// TestGetCodingPlanProviderUsesPlatformInsteadOfURL supplies coding-plan records
// with custom or another platform's endpoint spelling. It checks that Kimi and
// Zhipu identity comes from Platform rather than their configured URL.
func TestGetCodingPlanProviderUsesPlatformInsteadOfURL(t *testing.T) {
	t.Parallel()
	kimi := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"provider_mode": ProviderModeCoding,
			"base_url":      "https://relay.example/custom",
		},
	}}
	require.Equal(t, capability.PlatformKimi, kimi.GetCodingPlanProvider())
	zhipu := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"provider_mode": ProviderModeCoding,
			"base_url":      "https://api.kimi.com/coding/v1",
		},
	}}
	require.Equal(t, capability.PlatformZhipu, zhipu.GetCodingPlanProvider(), "中继路径不得改变平台身份")
}

// TestAdaptiveProtocolBaseURLs combines adaptive selection with each supported
// Kimi, Zhipu, and DeepSeek platform and provider mode. It checks their default
// Chat, Messages, and Responses addresses plus the Messages endpoint
// convenience method.
func TestAdaptiveProtocolBaseURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string // endpoint scenario display name
		platform      string // configured Kimi/Zhipu/DeepSeek platform identity
		mode          string // configured provider billing mode
		wantChat      string // expected default Chat address
		wantAnthropic string // expected default Messages address
		wantResponses string // expected default Responses address
	}{
		{"kimi payg", capability.PlatformKimi, ProviderModePayG, DefaultKimiPayGBaseURL, DefaultKimiPayGAnthropicBaseURL, DefaultKimiPayGBaseURL},
		{"kimi coding", capability.PlatformKimi, ProviderModeCoding, DefaultKimiCodingBaseURL, DefaultKimiCodingAnthropicBaseURL, DefaultKimiCodingBaseURL},
		{"zhipu payg", capability.PlatformZhipu, ProviderModePayG, DefaultZhipuPayGBaseURL, DefaultZhipuAnthropicBaseURL, DefaultZhipuPayGBaseURL},
		{"zhipu coding", capability.PlatformZhipu, ProviderModeCoding, DefaultZhipuCodingBaseURL, DefaultZhipuAnthropicBaseURL, DefaultZhipuCodingBaseURL},
		{"deepseek", capability.PlatformDeepseek, ProviderModePayG, DefaultDeepseekBaseURL, DefaultDeepseekAnthropicBaseURL, DefaultDeepseekBaseURL},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := ProtocolTarget{Record: &Record{Platform: tc.platform, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
				"api_protocol":  APIProtocolAdaptive,
				"provider_mode": tc.mode,
			}}}
			require.Equal(t, tc.wantChat, provider.GetTextAPIKeyProtocolBaseURL(APIProtocolChatCompletions))
			require.Equal(t, tc.wantAnthropic, provider.GetTextAPIKeyProtocolBaseURL(APIProtocolAnthropic))
			require.Equal(t, tc.wantResponses, provider.GetTextAPIKeyProtocolBaseURL(APIProtocolResponses))
			require.Equal(t, tc.wantAnthropic, provider.GetAnthropicProtocolBaseURL())
		})
	}
}

// TestAdaptiveProtocolBaseURLOverrides supplies three distinct protocol slots
// alongside a legacy Chat base on a DeepSeek record. It checks that adaptive
// lookup uses the corresponding slot for every asserted endpoint.
func TestAdaptiveProtocolBaseURLOverrides(t *testing.T) {
	t.Parallel()

	provider := ProtocolTarget{Record: &Record{Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
		"api_protocol": APIProtocolAdaptive,
		"base_url":     "https://legacy-chat.example.com",
		"api_base_urls": map[string]any{
			APIProtocolChatCompletions: "https://chat.example.com",
			APIProtocolAnthropic:       "https://anthropic.example.com",
			APIProtocolResponses:       "https://responses.example.com",
		},
	}}}

	require.Equal(t, "https://chat.example.com", provider.GetOpenAIBaseURL())
	require.Equal(t, "https://chat.example.com", provider.GetTextAPIKeyProtocolBaseURL(APIProtocolChatCompletions))
	require.Equal(t, "https://anthropic.example.com", provider.GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://responses.example.com", provider.GetTextAPIKeyProtocolBaseURL(APIProtocolResponses))
}

// TestAnthropicProtocolBaseURL supplies fixed legacy Messages records with
// platform defaults and a custom relay. It checks the selected Messages bases
// and an empty result for a fixed Chat configuration.
func TestAnthropicProtocolBaseURL(t *testing.T) {
	t.Parallel()

	// Default endpoints depend on platform and provider mode.
	require.Equal(t, "https://api.moonshot.cn/anthropic", (ProtocolTarget{Record: &Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": APIProtocolAnthropic},
	}}).GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://api.kimi.com/coding", (ProtocolTarget{Record: &Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": APIProtocolAnthropic, "provider_mode": ProviderModeCoding},
	}}).GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://open.bigmodel.cn/api/anthropic", (ProtocolTarget{Record: &Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": APIProtocolAnthropic},
	}}).GetAnthropicProtocolBaseURL())
	require.Equal(t, "https://api.deepseek.com/anthropic", (ProtocolTarget{Record: &Record{
		Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": APIProtocolAnthropic},
	}}).GetAnthropicProtocolBaseURL())

	// An explicit base_url overrides the legacy default.
	require.Equal(t, "https://custom.example.com/anthropic", (ProtocolTarget{Record: &Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_protocol": APIProtocolAnthropic, "base_url": "https://custom.example.com/anthropic"},
	}}).GetAnthropicProtocolBaseURL())

	// A fixed Chat configuration has no Messages base URL.
	require.Empty(t, (ProtocolTarget{Record: &Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://open.bigmodel.cn/api/paas/v4"},
	}}).GetAnthropicProtocolBaseURL())
}

// TestGetOpenAIFormatBaseURL_ProtocolAware supplies built-in and custom legacy
// Messages bases plus a Chat record. It checks platform Chat defaults, terminal
// Messages-path removal on relays, and direct use of the supplied Chat base.
func TestGetOpenAIFormatBaseURL_ProtocolAware(t *testing.T) {
	t.Parallel()

	zhipuAnthropic := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformZhipu, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAnthropic,
			"base_url":     "https://open.bigmodel.cn/api/anthropic",
		},
	}}
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4", zhipuAnthropic.GetOpenAIFormatBaseURL())

	kimiCodingAnthropic := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol":  APIProtocolAnthropic,
			"provider_mode": ProviderModeCoding,
			"base_url":      "https://api.kimi.com/coding",
		},
	}}
	require.Equal(t, "https://api.kimi.com/coding/v1", kimiCodingAnthropic.GetOpenAIFormatBaseURL())

	deepseekRelay := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAnthropic,
			"base_url":     "https://relay.example.com/proxy/deepseek/anthropic/",
		},
	}}
	require.Equal(t, "https://relay.example.com/proxy/deepseek", deepseekRelay.GetOpenAIFormatBaseURL())

	kimiRelay := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAnthropic,
			"base_url":     "https://relay.example.com/moonshot/anthropic",
		},
	}}
	require.Equal(t, "https://relay.example.com/moonshot", kimiRelay.GetOpenAIFormatBaseURL())

	// Chat configurations use the configured base_url directly.
	ccProvider := ProtocolTarget{Record: &Record{
		Platform: capability.PlatformDeepseek, Type: capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://ds-relay.example.com"},
	}}
	require.Equal(t, "https://ds-relay.example.com", ccProvider.GetOpenAIFormatBaseURL())
}

// TestOpenCodeProtocolEndpointsKeepProviderPolicySeparate selects each
// supported wire family through one endpoint entry. Kimi, Zhipu, and DeepSeek
// stateless classification and Kimi, Zhipu, and DeepSeek-specific endpoint
// lookup must remain unavailable for the OpenCode record.
func TestOpenCodeProtocolEndpointsKeepProviderPolicySeparate(t *testing.T) {
	r := &Record{Platform: PlatformOpenCode, Type: ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}
	for selected, suffix := range map[capability.ProtocolID]string{
		capability.ProtocolOpenAIResponses:       "/openai/v1",
		capability.ProtocolOpenAIChatCompletions: "/openai/v1",
		capability.ProtocolAnthropicMessages:     "/anthropic/v1",
		capability.ProtocolGeminiGenerateContent: "/google/v1beta",
		capability.ProtocolSystemOne:             "/systemone/v1",
	} {
		target := ProtocolTarget{Record: r, Protocol: selected}
		require.Equal(t, "https://opencode.ai/inference"+suffix, target.BaseURL())
		require.False(t, target.UsesStatelessResponses())
		require.Empty(t, target.GetTextAPIKeyProtocolBaseURL(APIProtocolResponses))
	}
	require.Equal(t, DefaultJevBaseURL, r.GetJevBaseURL())
	require.Equal(t, "google-default", r.GetGeminiBaseURL("google-default"))
}

// TestProtocolBaseURLUsesAnthropicProviderEndpoint checks ordinary Anthropic
// API-key records with a unified protocol selection. Both default and custom
// endpoints must resolve through Anthropic settings without requiring Kimi,
// Zhipu, and DeepSeek slots.
func TestProtocolBaseURLUsesAnthropicProviderEndpoint(t *testing.T) {
	for _, custom := range []string{"", "https://messages.example/anthropic"} {
		r := &Record{Platform: PlatformAnthropic, Type: ProviderTypeAPIKey, Credentials: map[string]any{
			"api_key": "key", "base_url": custom, UpstreamProtocolsKey: []string{string(capability.ProtocolAnthropicMessages)},
		}}
		want := custom
		if want == "" {
			want = "https://api.anthropic.com"
		}
		require.Equal(t, want, (ProtocolTarget{Record: r, Protocol: capability.ProtocolAnthropicMessages}).BaseURL())
	}
}

// TestSupportsHTTPResponsesContinuation separates native response references from
// converted or stateless routes. OpenCode keeps its platform identity while API
// keys on its Responses route can retain an authorized upstream identifier.
func TestSupportsHTTPResponsesContinuation(t *testing.T) {
	for _, tc := range []struct {
		platform string                // configured provider platform
		kind     string                // configured credential type
		protocol capability.ProtocolID // admitted operation protocol
		allowed  bool                  // expected continuation eligibility
	}{
		{capability.PlatformOpenCode, capability.ProviderTypeAPIKey, capability.ProtocolOpenAIResponses, true},
		{capability.PlatformOpenCode, capability.ProviderTypeAPIKey, capability.ProtocolOpenAIChatCompletions, false},
		{capability.PlatformOpenCode, capability.ProviderTypeAPIKey, capability.ProtocolAnthropicMessages, false},
		{capability.PlatformOpenCode, capability.ProviderTypeAPIKey, capability.ProtocolGeminiGenerateContent, false},
		{capability.PlatformOpenCode, capability.ProviderTypeAPIKey, "", false},
		{capability.PlatformOpenCode, capability.ProviderTypeOAuth, capability.ProtocolOpenAIResponses, false},
		{capability.PlatformKimi, capability.ProviderTypeAPIKey, capability.ProtocolOpenAIResponses, false},
		{capability.PlatformDeepseek, capability.ProviderTypeAPIKey, capability.ProtocolOpenAIResponses, false},
		{capability.PlatformOpenAI, capability.ProviderTypeAPIKey, capability.ProtocolOpenAIResponses, true},
		{capability.PlatformOpenAI, capability.ProviderTypeAPIKey, capability.ProtocolOpenAIChatCompletions, false},
		{capability.PlatformOpenAI, capability.ProviderTypeAPIKey, "", true},
		{capability.PlatformOpenAI, capability.ProviderTypeOAuth, capability.ProtocolOpenAIResponses, false},
	} {
		t.Run(tc.platform+"/"+tc.kind+"/"+string(tc.protocol), func(t *testing.T) {
			target := ProtocolTarget{Record: &Record{Platform: tc.platform, Type: tc.kind}, Protocol: tc.protocol}
			require.Equal(t, tc.allowed, target.SupportsHTTPResponsesContinuation())
			require.Equal(t, tc.platform, target.Platform)
		})
	}
	require.False(t, (ProtocolTarget{}).SupportsHTTPResponsesContinuation())
}

// TestResponsesOperationPoliciesResolveSelectedProtocol exercises platform
// rules on explicit Responses and Chat operations over an adaptive provider.
// It checks that endpoint slots, continuation removal, and client-tool encoding
// retain separate platform choices and never leak into a selected Chat route.
func TestResponsesOperationPoliciesResolveSelectedProtocol(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformOpenAI, PlatformOpenCode} {
		for _, protocol := range []capability.ProtocolID{capability.ProtocolOpenAIResponses, capability.ProtocolOpenAIChatCompletions} {
			t.Run(platform+"/"+string(protocol), func(t *testing.T) {
				target := ProtocolTarget{Record: &Record{Platform: platform, Type: ProviderTypeAPIKey, Credentials: map[string]any{UpstreamProtocolsKey: []string{}}}, Protocol: protocol}
				responses := protocol == capability.ProtocolOpenAIResponses
				slots := responses && (platform == PlatformKimi || platform == PlatformDeepseek)
				require.Equal(t, slots, target.UsesResponsesEndpointSlots())
				require.Equal(t, slots, target.UsesStatelessResponses())
				require.Equal(t, responses && platform == PlatformDeepseek, target.RequiresResponsesClientToolEncoding())
			})
		}
	}
}
