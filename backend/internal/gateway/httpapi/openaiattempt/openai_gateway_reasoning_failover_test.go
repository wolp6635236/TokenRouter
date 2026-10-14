package openaiattempt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// encryptedReasoningCanonicalBody 模拟带有上游账号签发的加密 reasoning 项的 Responses 请求。
const encryptedReasoningCanonicalBody = `{"model":"gpt-5.1","stream":false,"input":[` +
	`{"type":"message","role":"user","content":"hello"},` +
	`{"type":"reasoning","id":"rs_first_abc123","encrypted_content":"ENC_BLOB","summary":[{"type":"summary_text","text":"thinking"}]},` +
	`{"type":"message","role":"assistant","content":"hi"}` +
	`]}`

func newOpenAIPassthroughProvider(id int64, passthrough bool) *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: id,
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
			Extra:    map[string]any{"openai_passthrough": passthrough},
		},
	}
}

func reasoningItemCount(t *testing.T, body []byte) int {
	t.Helper()
	count := 0
	gjson.GetBytes(body, "input").ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "reasoning" {
			count++
		}
		return true
	})
	return count
}

// TestDeriveOpenAIForwardAttemptBody_FirstProviderKeepsReasoning 检查第一个提供商的首次尝试和同提供商重试使用 canonical 原文。
func TestDeriveOpenAIForwardAttemptBody_FirstProviderKeepsReasoning(t *testing.T) {
	canonical := []byte(encryptedReasoningCanonicalBody)
	state := &openAIFailoverReasoningState{}
	first := newOpenAIPassthroughProvider(40, false)

	firstBody := deriveOpenAIForwardAttemptBody(nil, canonical, first, state)
	retryBody := deriveOpenAIForwardAttemptBody(nil, canonical, first, state)

	require.JSONEq(t, encryptedReasoningCanonicalBody, string(firstBody))
	require.JSONEq(t, encryptedReasoningCanonicalBody, string(retryBody))
}

// TestDeriveOpenAIForwardAttemptBody_ProviderSwitchStripsReasoning 检查换到其他提供商后删除整个加密 reasoning 项，
// 透传设置不影响结果，后续尝试继续使用清理后的正文，canonical 保持不变。
func TestDeriveOpenAIForwardAttemptBody_ProviderSwitchStripsReasoning(t *testing.T) {
	cases := []struct {
		name                          string
		firstPassthrough, passthrough bool
	}{
		{name: "non_passthrough_to_non_passthrough"},
		{name: "passthrough_to_non_passthrough", firstPassthrough: true},
		{name: "non_passthrough_to_passthrough", passthrough: true},
		{name: "passthrough_to_passthrough", firstPassthrough: true, passthrough: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonical := []byte(encryptedReasoningCanonicalBody)
			state := &openAIFailoverReasoningState{}

			first := deriveOpenAIForwardAttemptBody(nil, canonical, newOpenAIPassthroughProvider(50, tc.firstPassthrough), state)
			second := deriveOpenAIForwardAttemptBody(nil, canonical, newOpenAIPassthroughProvider(51, tc.passthrough), state)
			retry := deriveOpenAIForwardAttemptBody(nil, canonical, newOpenAIPassthroughProvider(51, tc.passthrough), state)
			third := deriveOpenAIForwardAttemptBody(nil, canonical, newOpenAIPassthroughProvider(52, false), state)

			require.Equal(t, 1, reasoningItemCount(t, first))
			for _, body := range [][]byte{second, retry, third} {
				require.Equal(t, 0, reasoningItemCount(t, body), "换提供商后需要删除整个 reasoning 项")
				require.NotContains(t, string(body), "rs_first_abc123")
				require.NotContains(t, string(body), "ENC_BLOB")
				require.Equal(t, 2, int(gjson.GetBytes(body, "input.#").Int()))
				require.Equal(t, "hello", gjson.GetBytes(body, "input.0.content").String())
				require.Equal(t, "hi", gjson.GetBytes(body, "input.1.content").String())
			}
			require.JSONEq(t, encryptedReasoningCanonicalBody, string(canonical))
		})
	}
}

// TestDeriveOpenAIForwardAttemptBody_ProviderSwitchWithoutEncryptedReasoning 检查没有加密 reasoning 时换提供商仍使用原文。
func TestDeriveOpenAIForwardAttemptBody_ProviderSwitchWithoutEncryptedReasoning(t *testing.T) {
	const plain = `{"model":"gpt-5.1","input":[{"type":"reasoning","id":"rs_plain","summary":[]},{"type":"message","role":"user","content":"hello"}]}`
	canonical := []byte(plain)
	state := &openAIFailoverReasoningState{}

	_ = deriveOpenAIForwardAttemptBody(nil, canonical, newOpenAIPassthroughProvider(60, false), state)
	second := deriveOpenAIForwardAttemptBody(nil, canonical, newOpenAIPassthroughProvider(61, false), state)

	require.JSONEq(t, plain, string(second))
}
