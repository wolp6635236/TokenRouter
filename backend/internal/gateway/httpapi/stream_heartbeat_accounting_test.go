package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// TestStreamHeartbeatAccounting combines each existing heartbeat source with
// queue output. Only content bytes may affect retry observations, regardless
// of which controller emitted the earlier connection keepalive bytes.
func TestStreamHeartbeatAccounting(t *testing.T) {
	for _, sources := range [][]string{{"queue"}, {"reader"}, {"sse"}, {"json"}, {"queue", "reader", "sse"}, {"queue", "reader", "json"}} {
		t.Run(sources[len(sources)-1], func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			require.Equal(t, -1, HeartbeatAdjustedWrittenSize(c))
			c.Writer.WriteHeaderNow()
			require.Zero(t, HeartbeatAdjustedWrittenSize(c))
			count := 0
			for _, source := range sources {
				n, err := c.Writer.Write([]byte(": heartbeat\n\n"))
				require.NoError(t, err)
				count += n
				switch source {
				case "queue":
					RecordStreamHeartbeat(c, n)
				case "reader":
					RecordOpenAIStreamKeepaliveBytes(c, n)
				case "sse":
					c.Set(openAICompactSSEKeepaliveKey, &openAICompactSSEKeepalive{writer: c.Writer, bytes: n})
				case "json":
					c.Set(openAIImagesJSONKeepaliveKey, &openAIImagesJSONKeepalive{writer: c.Writer, bytes: n})
				}
			}
			require.Equal(t, count, StreamHeartbeatBytes(c))
			require.Equal(t, -1, HeartbeatAdjustedWrittenSize(c))
			require.True(t, StreamHasOnlyHeartbeats(c))
			output := upstream.NewOutputContext(NewResponseSink(c))
			require.True(t, output.Writer.Written())
			require.False(t, output.Writer.ContentWritten())
			_, err := output.Writer.Write([]byte("data: content\n\n"))
			require.NoError(t, err)
			require.True(t, output.Writer.ContentWritten())
			require.Equal(t, len("data: content\n\n"), HeartbeatAdjustedWrittenSize(c))
			require.False(t, StreamHasOnlyHeartbeats(c))
			require.True(t, NewResponseSink(c).OutputState().ContentCommitted)
		})
	}
	require.Zero(t, StreamHeartbeatBytes(nil))
	require.Equal(t, -1, HeartbeatAdjustedWrittenSize(nil))
}
