package timing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDataEventExcludesEmptyToolInput distinguishes Claude Code tool keepalive
// deltas from real argument fragments. Empty partial JSON must not establish
// first-event timing, while even one argument byte remains an observed event.
func TestDataEventExcludesEmptyToolInput(t *testing.T) {
	require.False(t, DataEvent(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":""}}`, ""))
	require.False(t, DataEvent(`{"delta":{"type":"input_json_delta","partial_json":""}}`, "content_block_delta"))
	require.True(t, DataEvent(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{"}}`, ""))
}

// TestDataEventGeminiPlaceholders excludes relay padding from first-event timing.
// Native and wrapped empty candidates remain heartbeats, while real candidate
// content, prompt blocking, and additional response metadata retain their observations.
func TestDataEventGeminiPlaceholders(t *testing.T) {
	require.False(t, DataEvent(`{"candidates":[]}`, ""))
	require.False(t, DataEvent(`{"candidates":null}`, ""))
	require.False(t, DataEvent(`{"response":{"candidates":[]}}`, ""))
	require.True(t, DataEvent(`{"candidates":[],"modelVersion":"model"}`, ""))
	require.True(t, DataEvent(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, "ping"))
	require.True(t, DataEvent(`{"promptFeedback":{"blockReason":"SAFETY"}}`, ""))
}
