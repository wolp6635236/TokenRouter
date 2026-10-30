package upstream

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
)

// filterHeartbeatInput pushes physical SSE lines while preserving their endings.
// It calls Flush at EOF, matching a reader that retains unfinished candidate
// bytes but never holds ordinary output until the complete response ends.
func filterHeartbeatInput(input string) string {
	var filter SSEHeartbeatLineFilter
	var out strings.Builder
	for input != "" {
		n := strings.IndexAny(input, "\r\n")
		if n < 0 {
			n = len(input)
		} else {
			n++
			if input[n-1] == '\r' && n < len(input) && input[n] == '\n' {
				n++
			}
		}
		for _, line := range filter.Push([]byte(input[:n])) {
			_, _ = out.Write(line)
		}
		input = input[n:]
	}
	for _, line := range filter.Flush() {
		_, _ = out.Write(line)
	}
	return out.String()
}

// TestSSEHeartbeatClassification covers payload precedence and non-content data.
// Explicit JSON application types override misleading heartbeat event names,
// while empty payloads and all three supported heartbeat types are consumed.
func TestSSEHeartbeatClassification(t *testing.T) {
	require.True(t, IsSSEHeartbeatEvent("custom", " \t"))
	for _, name := range []string{"ping", "heartbeat", "keepalive"} {
		require.True(t, IsSSEHeartbeatEvent(name, "{}"))
		require.True(t, IsSSEHeartbeatEvent("custom", `{"type":"`+name+`"}`))
	}
	require.False(t, IsSSEHeartbeatEvent("ping", `{"type":"response.completed"}`))
	require.False(t, IsSSEHeartbeatEvent("ping", `{"type":" ping "}`))
	require.False(t, IsSSEHeartbeatEvent("", "[DONE]"))
}

// TestSSEHeartbeatLineFilter drops comments and complete heartbeat candidates.
// Original non-heartbeat bytes, unknown fields, CRLF and lone-CR delimiters,
// malformed candidates and unfinished EOF frames follow their specified rules.
func TestSSEHeartbeatLineFilter(t *testing.T) {
	for _, tc := range []struct {
		name  string // filter scenario name
		input string // original upstream bytes
		want  string // retained client bytes
	}{
		{"comments", ": source\n\n: other\r\n\r\n", ""},
		{"mixed comment", "event: future\n: source\ndata: {}\n\n", "event: future\ndata: {}\n\n"},
		{"ping", "event: ping\ndata: {\"type\":\"ping\",\"cost\":\"0\"}\n\n", ""},
		{"payload precedence", "event: custom\ndata: {\"type\":\"heartbeat\"}\n\n", ""},
		{"empty error", "event: error\ndata:\n\n", "event: error\ndata:\n\n"},
		{"unnamed", "data: {\"type\":\"heartbeat\"}\n\n", ""},
		{"empty data", "data:\n\ndata: \r\n\r\n", ""},
		{"event only", "event: ping\n\n", ""},
		{"missing type", "event: ping\ndata: {\"cost\":2}\n\n", ""},
		{"malformed", "event: ping\ndata: {not-json}\n\n", ""},
		{"trailing JSON", "event: ping\ndata: {\"type\":\"ping\"} trailing\n\n", ""},
		{"conflicting type", "event: ping\ndata: {\"type\":\"response.completed\"}\n\n", "event: ping\ndata: {\"type\":\"response.completed\"}\n\n"},
		{"unknown field", "event: ping\nid: 7\ndata: {\"type\":\"ping\"}\n\n", "event: ping\nid: 7\ndata: {\"type\":\"ping\"}\n\n"},
		{"CRLF", "event: ping\r\ndata: {}\r\n\r\nevent: future\r\ndata: {}\r\n\r\n", "event: future\r\ndata: {}\r\n\r\n"},
		{"bare CR", "event: ping\rdata: {}\r\revent: future\rdata: {}\r\r", "event: future\rdata: {}\r\r"},
		{"incomplete", "event: ping\ndata: {}", "event: ping\ndata: {}"},
		{"plain data", "data: {\"text\":\"hello\"}\n\ndata: [DONE]\n\n", "data: {\"text\":\"hello\"}\n\ndata: [DONE]\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, filterHeartbeatInput(tc.input)) })
	}
}

// TestSSEHeartbeatLineFilterBoundsAndCopies checks both storage limits and
// scanner-buffer reuse. Candidates beyond either limit retain bytes unchanged,
// and a later scanner write cannot corrupt an earlier retained event line.
func TestSSEHeartbeatLineFilterBoundsAndCopies(t *testing.T) {
	byLines := "event: ping\n" + strings.Repeat("data: \n", SSEHeartbeatFrameMaxLines) + "\n"
	byBytes := "event: ping\ndata: {\"type\":\"ping\",\"pad\":\"" + strings.Repeat("x", SSEHeartbeatFrameMaxBytes) + "\"}\n\n"
	require.Equal(t, byLines, filterHeartbeatInput(byLines))
	require.Equal(t, byBytes, filterHeartbeatInput(byBytes))
	var filter SSEHeartbeatLineFilter
	line := []byte("event: ping\n")
	require.Empty(t, filter.Push(line))
	copy(line, "corrupted!!")
	require.Equal(t, [][]byte{[]byte("event: ping\n")}, filter.Flush())
	require.Equal(t, [][]byte{[]byte("data: content\n")}, filter.Push([]byte("data: content\n")))
}

// TestSSEHeartbeatFilteringComposesWithClientTools passes a billing ping and
// a terminal through client-tool restoration. Common filtering removes the
// upstream ping while preserving restored protocol metadata and response identity.
func TestSSEHeartbeatFilteringComposesWithClientTools(t *testing.T) {
	input := ": upstream-comment\n\nevent: ping\ndata: {\"type\":\"ping\",\"cost\":0}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\"}}\n\n"
	body := NewResponsesClientToolStreamBody(io.NopCloser(strings.NewReader(input)), bridge.ResponsesClientToolMapping{CustomTools: map[string]bool{"apply_patch": true}}, DefaultSSELineLimit)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.NotContains(t, string(data), "upstream-comment")
	filtered := filterHeartbeatInput(string(data))
	require.NotContains(t, filtered, "ping")
	require.Contains(t, filtered, "response.completed")
	require.Contains(t, filtered, "resp_test")
}
