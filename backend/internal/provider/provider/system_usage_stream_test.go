package provider

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// system_usage_stream_test.go checks recovery at SSE frame boundaries after
// malformed or oversized data. Later numeric observations must remain available
// for provider accounting, while evidence lost from an earlier frame prevents
// the observer from reporting complete usage.

// TestSystemUsageObserverRecoversAfterBadFrames splits invalid and oversized SSE
// frames across reads. Later valid usage survives, including an EOF tail, while
// the lost frame keeps the accounting evidence explicitly partial.
func TestSystemUsageObserverRecoversAfterBadFrames(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		for _, newline := range []string{"", "\n"} {
			observer := systemUsageObserver{sse: true}
			if oversized {
				observer.consume([]byte("data: "))
				observer.consume([]byte(strings.Repeat("x", systemUsageFrameLimit)))
				observer.consume([]byte("discarded suffix\n"))
			} else {
				observer.consume([]byte("data: {broken"))
				observer.consume([]byte("}\n"))
			}
			observer.consume([]byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}" + newline))
			observer.finish(io.EOF)
			row := &usage.UsageLog{SystemUsage: &usage.SystemUsage{HTTPStatus: 200, Outcome: "failed", UsageState: "missing"}}
			observer.apply(row, nil)
			require.Equal(t, 12, row.InputTokens)
			require.Equal(t, 3, row.OutputTokens)
			require.Equal(t, "partial", row.SystemUsage.UsageState)
			require.Empty(t, observer.buffer)
		}
	}
}
