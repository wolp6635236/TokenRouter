package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	protocolanthropic "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestModelIdentityPreservesEffortSuffix 验证转发不修正拼写、不拆解档位、不替换未知型号。
func TestModelIdentityPreservesEffortSuffix(t *testing.T) {
	for _, model := range []string{"gpt-5.4-xhigh", "gpt-5.6-sol-max", "gpt-5.1-codex-max", "gpt5.6sol", "vendor/gpt-5.4", "claude-opus-5.5", "claude-opus-5-5", ""} {
		require.Equal(t, model, (ModelPolicy{}).NormalizeOpenAI(model))
	}
}

// TestExplicitMessagesEffortPreserved 检查客户端指定的档位是否按最终型号能力转换。
func TestExplicitMessagesEffortPreserved(t *testing.T) {
	req := &protocolanthropic.AnthropicRequest{Model: "client-alias", OutputConfig: &protocolanthropic.AnthropicOutputConfig{Effort: "max"}}
	require.Equal(t, "max", OpenAICompatAnthropicReasoningEffort(req, "gpt-5.6-sol", "xhigh"))
	require.Equal(t, "xhigh", OpenAICompatAnthropicReasoningEffort(nil, "gpt-5.4", "xhigh"))
}

// TestSparkIdentityIsExact 验证大小写比较不开放后缀或供应商别名。
func TestSparkIdentityIsExact(t *testing.T) {
	require.True(t, IsCodexSparkModel(" GPT-5.3-CODEX-SPARK "))
	require.False(t, IsCodexSparkModel("gpt-5.3-codex-spark-high"))
	require.False(t, IsCodexSparkModel("vendor/gpt-5.3-codex-spark"))
}

func TestNormalizeOpenAICodexCompactReasoningEffortDowngradesMax(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"compact me","reasoning":{"effort":"max","summary":"auto"}}`)

	normalized, changed, err := NormalizeOpenAICodexCompactReasoningEffort(body, "gpt-5.6-sol")

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(normalized, "model").String())
	require.Equal(t, "xhigh", gjson.GetBytes(normalized, "reasoning.effort").String())
	require.Equal(t, "auto", gjson.GetBytes(normalized, "reasoning.summary").String())
}

func TestFilterOpenAIResponsesNoneReasoningEffortForProvider(t *testing.T) {
	tests := []struct {
		name          string
		provider      *providercore.Record
		body          string
		wantNested    bool
		wantFlat      bool
		wantSummary   bool
		wantReasoning bool
	}{
		{
			name:          "Kimi removes none placeholder",
			provider:      &providercore.Record{Platform: capability.PlatformKimi, Type: capability.ProviderTypeAPIKey},
			body:          `{"reasoning":{"effort":"none"},"reasoning_effort":"NONE"}`,
			wantReasoning: false,
		},
		{
			name:          "custom compatible endpoint removes none placeholder",
			provider:      &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{"base_url": "https://compat.example/v1"}},
			body:          `{"reasoning":{"effort":"none"},"reasoning_effort":"NONE"}`,
			wantReasoning: false,
		},
		{
			name:          "preserves other reasoning members",
			provider:      &providercore.Record{Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey},
			body:          `{"reasoning":{"effort":" none ","summary":"auto"}}`,
			wantSummary:   true,
			wantReasoning: true,
		},
		{
			name:          "official OpenAI API preserves none",
			provider:      &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
			body:          `{"reasoning":{"effort":"none"},"reasoning_effort":"none"}`,
			wantNested:    true,
			wantFlat:      true,
			wantReasoning: true,
		},
		{
			name:          "OpenAI OAuth preserves none",
			provider:      &providercore.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			body:          `{"reasoning":{"effort":"none"}}`,
			wantNested:    true,
			wantReasoning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilterOpenAIResponsesNoneReasoningEffortForProvider(tt.provider, []byte(tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.wantNested, gjson.GetBytes(got, "reasoning.effort").Exists())
			require.Equal(t, tt.wantFlat, gjson.GetBytes(got, "reasoning_effort").Exists())
			require.Equal(t, tt.wantSummary, gjson.GetBytes(got, "reasoning.summary").Exists())
			require.Equal(t, tt.wantReasoning, gjson.GetBytes(got, "reasoning").Exists())
		})
	}
}

func TestFilterOpenAIResponsesNoneReasoningEffortForProvider_APIKeyAutomaticPassthroughPreservesRequest(t *testing.T) {
	body := []byte(`{"model":"qwen3.8-27b","input":"hi","max_output_tokens":20,"reasoning":{"effort":"none"},"presence_penalty":1.5}`)
	provider := &providercore.Record{
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://compat.example/v1",
		},
		Extra: map[string]any{"openai_passthrough": true},
	}

	got, err := FilterOpenAIResponsesNoneReasoningEffortForProvider(provider, body)

	require.NoError(t, err)
	require.JSONEq(t, string(body), string(got))
}
