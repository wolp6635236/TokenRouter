package capability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAntigravityRetiredSnapshot 验证旧快照中手动声明的协议不能恢复调用资格。
func TestAntigravityRetiredSnapshot(t *testing.T) {
	protocols := []ProtocolID{ProtocolAnthropicMessages, ProtocolGeminiGenerateContent, ProtocolOpenAIChatCompletions, ProtocolOpenAIResponses}
	for _, kind := range []string{ProviderTypeAPIKey, ProviderTypeUpstream, ProviderTypeSetupToken} {
		for _, source := range protocols {
			_, ok := ResolveRoute(ProviderProtocols{Platform: PlatformAntigravity, Type: kind, Enabled: protocols}, source, nil)
			require.False(t, ok, "%s %s", kind, source)
		}
		require.Empty(t, NativeProtocolOptions(PlatformAntigravity, kind, ""))
	}
	for _, source := range protocols {
		target, ok := ResolveRoute(ProviderProtocols{Platform: PlatformAntigravity, Type: ProviderTypeOAuth, Enabled: []ProtocolID{ProtocolGeminiGenerateContent}}, source, nil)
		require.True(t, ok)
		require.Equal(t, ProtocolGeminiGenerateContent, target)
	}
}

func TestMixedGroupAutomaticAndExplicitRoutes(t *testing.T) {
	// 相同 Chat 请求根据候选提供商选择单步路线，优先使用提供商直接支持的协议。
	for _, tc := range []struct {
		platform string
		target   ProtocolID
	}{
		{PlatformAnthropic, ProtocolAnthropicMessages},
		{PlatformOpenAI, ProtocolOpenAIChatCompletions},
		{PlatformGemini, ProtocolGeminiGenerateContent},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			candidate := ProviderProtocols{Platform: tc.platform, Type: ProviderTypeAPIKey, Enabled: []ProtocolID{tc.target}}
			target, ok := ResolveRoute(candidate, ProtocolOpenAIChatCompletions, nil)
			require.True(t, ok)
			require.Equal(t, tc.target, target)
			_, ok = ResolveRoute(candidate, ProtocolOpenAIChatCompletions, map[ProtocolID][]ProtocolID{ProtocolOpenAIChatCompletions: {}})
			require.Equal(t, tc.platform == PlatformOpenAI, ok)
		})
	}
}
