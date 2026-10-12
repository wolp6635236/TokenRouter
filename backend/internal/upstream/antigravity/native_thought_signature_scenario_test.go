package antigravity

// 本文件检查 request_transformer.go 的 DummyThoughtSignature 与 protocol/gemini/signature_cleaner.go 的签名清理配合。

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	protocolgemini "github.com/TokenFlux/TokenRouter/internal/protocol/gemini"
)

func TestCleanGeminiNativeThoughtSignatures_ReplacesNestedThoughtSignatures(t *testing.T) {
	input := []byte(`{
		"contents": [
			{
				"role": "user",
				"parts": [{"text": "hello"}]
			},
			{
				"role": "model",
				"parts": [
					{"text": "thinking", "thought": true, "thoughtSignature": "sig_1"},
					{"functionCall": {"name": "toolA", "args": {"k": "v"}}, "thoughtSignature": "sig_2"}
				]
			}
		],
		"cachedContent": {
			"parts": [{"text": "cached", "thoughtSignature": "sig_3"}]
		},
		"signature": "keep_me"
	}`)

	cleaned := protocolgemini.CleanNativeThoughtSignatures(input, bridge.DummyThoughtSignature)

	var got map[string]any
	require.NoError(t, json.Unmarshal(cleaned, &got))

	require.NotContains(t, string(cleaned), `"thoughtSignature":"sig_1"`)
	require.NotContains(t, string(cleaned), `"thoughtSignature":"sig_2"`)
	require.NotContains(t, string(cleaned), `"thoughtSignature":"sig_3"`)
	require.Contains(t, string(cleaned), `"thoughtSignature":"`+DummyThoughtSignature+`"`)
	require.Contains(t, string(cleaned), `"signature":"keep_me"`)
}

func TestCleanGeminiNativeThoughtSignatures_InvalidJSONReturnsOriginal(t *testing.T) {
	input := []byte(`{"contents":[invalid-json]}`)

	cleaned := protocolgemini.CleanNativeThoughtSignatures(input, bridge.DummyThoughtSignature)

	require.Equal(t, input, cleaned)
}
