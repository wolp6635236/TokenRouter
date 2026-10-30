package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestCollectGeminiStreamCandidates retains interleaved candidates and sparse usage.
// Distinct thought signatures prevent unsafe text merging, and repeated terminals
// must not duplicate already collected candidate content or erase unknown fields.
func TestCollectGeminiStreamCandidates(t *testing.T) {
	body := strings.Join([]string{
		`data: {"trace":"retained","candidates":[{"index":0,"vendor":true,"content":{"parts":[{"text":"a","thoughtSignature":"first"}]}}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":2}}`,
		`data: {"candidates":[{"index":1,"content":{"parts":[{"text":"b"}]}}]}`,
		`data: {"candidates":[{"index":0,"content":{"parts":[{"text":"c","thoughtSignature":"second"}]},"finishReason":"STOP"}]}`,
		`data: {"candidates":[{"index":1,"content":{"parts":[{"text":"d"}]},"finishReason":"MAX_TOKENS"}]}`,
		`data: {"candidates":[{"index":0,"content":{"parts":[{"text":"c","thoughtSignature":"second"}]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":3,"totalTokenCount":13}}`,
	}, "\n\n") + "\n\n"
	response, result, err := CollectGeminiStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(body))}, GeminiStreamOptions{Candidates: 2}, nil)
	require.NoError(t, err)
	require.True(t, result.Completed)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	require.Equal(t, "retained", gjson.GetBytes(encoded, "trace").String())
	require.True(t, gjson.GetBytes(encoded, "candidates.0.vendor").Bool())
	require.Equal(t, int64(2), gjson.GetBytes(encoded, "candidates.0.content.parts.#").Int())
	require.Equal(t, "first", gjson.GetBytes(encoded, "candidates.0.content.parts.0.thoughtSignature").String())
	require.Equal(t, "second", gjson.GetBytes(encoded, "candidates.0.content.parts.1.thoughtSignature").String())
	require.Equal(t, "bd", gjson.GetBytes(encoded, "candidates.1.content.parts.0.text").String())
	require.Equal(t, int64(10), gjson.GetBytes(encoded, "usageMetadata.promptTokenCount").Int())
	require.Equal(t, int64(2), gjson.GetBytes(encoded, "usageMetadata.cachedContentTokenCount").Int())
	require.Equal(t, int64(13), gjson.GetBytes(encoded, "usageMetadata.totalTokenCount").Int())
}

// TestCollectGeminiStreamPartialFailure returns observations beside missing completion.
// Buffered presentation must retain its failure while diagnostics can inspect the
// already generated text and explicit token counters without inferring success.
func TestCollectGeminiStreamPartialFailure(t *testing.T) {
	body := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}],\"usageMetadata\":{\"promptTokenCount\":4}}\n\n"
	response, result, err := CollectGeminiStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(body))}, GeminiStreamOptions{}, nil)
	require.Error(t, err)
	require.False(t, result.Completed)
	encoded, marshalErr := json.Marshal(response)
	require.NoError(t, marshalErr)
	require.Equal(t, "partial", gjson.GetBytes(encoded, "candidates.0.content.parts.0.text").String())
	require.Equal(t, int64(4), gjson.GetBytes(result.UsageJSON, "usageMetadata.promptTokenCount").Int())
}
