package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// gemini_sse_scenario_test.go exercises sse_keepalive.go, output.go, wait.go,
// gemini_native.go, and google_gemini_errors.go together. Terminal metadata and
// local failure output must preserve the same framing and accounting rules as
// native generation, even after an idle heartbeat commits HTTP 200.

// geminiShortWriter accepts a partial terminal to exercise HTTP sink error handling.
type geminiShortWriter struct {
	gin.ResponseWriter // delegated HTTP writer
}

// Write accepts every byte except the final one without a transport error.
func (w geminiShortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return w.ResponseWriter.Write(p[:len(p)-1])
}

// TestGeminiSSETerminalMetadata ends a response whose terminal is beyond the sniff limit.
// The sink must stop heartbeats using accepted event metadata, without retaining
// an unbounded JSON prefix or adding bytes after the terminal frame.
func TestGeminiSSETerminalMetadata(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolGeminiGenerateContent, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		output := upstream.NewOutputContext(NewResponseSink(c))
		output.Header("Content-Type", "text/event-stream")
		payload := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"" + strings.Repeat("x", 5000) + "\"}]},\"finishReason\":\"STOP\"}]}\n\n"
		output.NextEvent(true, true)
		_, err := io.WriteString(output.Writer, payload)
		require.NoError(t, err)
		output.Writer.Flush()
		time.Sleep(25 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, payload, readKeepaliveBody(t, c, rec))
		require.Zero(t, StreamHeartbeatBytes(c))
	})
}

// TestGeminiSSEQueueAccounting emits only the admission observer's empty heartbeat.
// Native SDK-compatible padding must remain outside the retry commitment and
// must not introduce a comment or synthetic generation event.
func TestGeminiSSEQueueAccounting(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolGeminiGenerateContent, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		started := false
		observer := WaitObserver(c, SSEPingFormatGemini, time.Second, true, &started, true)
		require.NoError(t, observer.Begin())
		require.NoError(t, observer.Heartbeat())
		require.True(t, started)
		require.Equal(t, "\n\n", readKeepaliveBody(t, c, rec))
		require.Equal(t, 2, StreamHeartbeatBytes(c))
		require.Equal(t, -1, HeartbeatAdjustedWrittenSize(c))
	})
}

// TestGeminiSSEFinalError selects JSON before commitment and native SSE afterward.
// A second error cannot duplicate the failure, and heartbeat-selected HTTP 200
// must preserve the intended Google status inside a data-only JSON event.
func TestGeminiSSEFinalError(t *testing.T) {
	for _, heartbeat := range []bool{false, true} {
		runSSEKeepaliveTest(t, protocol.ProtocolGeminiGenerateContent, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
			if heartbeat {
				writer, ok := c.Writer.(*sseKeepaliveWriter)
				require.True(t, ok)
				writer.openHeaderWait(c.Request.Context())
				time.Sleep(15 * time.Millisecond)
				synctest.Wait()
			}
			WriteGoogleError(c, http.StatusBadGateway, "fixture failure")
			WriteGoogleError(c, http.StatusInternalServerError, "duplicate failure")
			time.Sleep(25 * time.Millisecond)
			synctest.Wait()
			body := readKeepaliveBody(t, c, rec)
			require.NotContains(t, body, "duplicate failure")
			require.NotContains(t, body, "event:")
			if heartbeat {
				require.Equal(t, http.StatusOK, rec.Code)
				failure, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Equal(t, http.StatusBadGateway, failure.IntendedStatus)
				require.Equal(t, 1, strings.Count(body, "data: "))
				body = strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(body, "\n"), "data: "))
			} else {
				require.Equal(t, http.StatusBadGateway, rec.Code)
			}
			require.Equal(t, int64(502), gjson.Get(body, "error.code").Int())
			require.Equal(t, HTTPStatusToGoogleStatus(http.StatusBadGateway), gjson.Get(body, "error.status").String())
		})
	}
}

// TestGeminiSSEErrorEnvelopeTypes normalizes vendor error fields after HTTP commitment.
// SDKs require numeric codes and string statuses; a string code or null status
// must not turn an intended API error into another decoder failure.
func TestGeminiSSEErrorEnvelopeTypes(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolGeminiGenerateContent, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString("\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
		writeGoogleErrorBody(c, http.StatusTooManyRequests, []byte(`{"error":{"code":"429","status":null,"message":"quota","details":[{"reason":"retry"}]}}`))
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(readKeepaliveBody(t, c, rec), "\n"), "data: "))
		require.Equal(t, gjson.Number, gjson.Get(body, "error.code").Type)
		require.Equal(t, int64(429), gjson.Get(body, "error.code").Int())
		require.Equal(t, "RESOURCE_EXHAUSTED", gjson.Get(body, "error.status").String())
		require.Equal(t, "retry", gjson.Get(body, "error.details.0.reason").String())
	})
}

// TestGeminiSSETerminalShortWrite rejects an incomplete terminal through the normal chain.
// Partial acceptance must return io.ErrShortWrite and must not publish successful
// terminal delivery even when the writer reports no underlying transport error.
func TestGeminiSSETerminalShortWrite(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolGeminiGenerateContent, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		writer, ok := c.Writer.(*sseKeepaliveWriter)
		require.True(t, ok)
		c.Writer = geminiShortWriter{ResponseWriter: c.Writer}
		sink := NewResponseSink(c)
		err := sink.Emit(upstream.OutputEvent{Data: []byte("data: {}\n\n"), Terminal: true, Flush: true})
		require.True(t, errors.Is(err, io.ErrShortWrite))
		writer.mu.Lock()
		terminal := writer.terminal
		writer.mu.Unlock()
		require.False(t, terminal)
	})
}
