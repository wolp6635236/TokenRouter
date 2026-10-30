package httpapi

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keepaliveProbeWriter supplies connection capabilities and controllable output
// blocking while delegating ordinary response state to the captured Gin writer.
type keepaliveProbeWriter struct {
	gin.ResponseWriter // captured test response writer
	// writeGate and flushGate pause their respective operations until the test
	// closes the channel; nil channels leave that operation immediately available.
	writeGate chan struct{} // heartbeat write release channel
	flushGate chan struct{} // heartbeat flush release channel
	// entered is closed by the single gated operation, so tests begin competing
	// response accesses only after the controller holds its heartbeat mutex.
	entered   chan struct{} // blocked output entry notification
	closed    chan bool     // connection close notification channel
	hijackErr error         // delegated connection takeover error
}

// Write waits for an optional test gate before recording response bytes.
// Tests perform at most one gated write and initialize entered for its
// notification, then close writeGate to release the blocked heartbeat.
func (w *keepaliveProbeWriter) Write(data []byte) (int, error) {
	if w.writeGate != nil {
		// Hold the heartbeat inside the downstream call so the test can observe
		// whether stopping and passive state reads wait for the controller's mutex.
		close(w.entered)
		<-w.writeGate
	}
	return w.ResponseWriter.Write(data)
}

// Flush waits for an optional test gate before committing recorder output.
// The gate lets a test distinguish completion of Write from completion of
// the full heartbeat operation that also flushes those bytes.
func (w *keepaliveProbeWriter) Flush() {
	if w.flushGate != nil {
		// Keep flushing unfinished after bytes have been accepted, exposing any
		// response takeover that incorrectly releases heartbeat ownership before delivery completes.
		close(w.entered)
		<-w.flushGate
	}
	w.ResponseWriter.Flush()
}

// Hijack returns the configured connection error without taking ownership of
// a socket. Tests use this observable result to verify delegation while the
// controller remains eligible to send another heartbeat.
func (w *keepaliveProbeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, w.hijackErr
}

// CloseNotify returns the exact channel supplied by the test.
func (w *keepaliveProbeWriter) CloseNotify() <-chan bool {
	return w.closed
}

// Pusher exposes this probe as the downstream push capability.
func (w *keepaliveProbeWriter) Pusher() http.Pusher {
	return w
}

// Push accepts a probe push request without writing response data.
func (w *keepaliveProbeWriter) Push(string, *http.PushOptions) error {
	return nil
}

// newKeepaliveWriterForTest binds a response writer to the selected heartbeat
// controller without starting its timer. The returned beat and stop functions
// let tests coordinate output directly without depending on elapsed real time.
func newKeepaliveWriterForTest(writer gin.ResponseWriter, format string) (gin.ResponseWriter, func() bool, func()) {
	if format == "sse" {
		k := &openAICompactSSEKeepalive{writer: writer, stop: make(chan struct{})}
		return &compactKeepaliveWriter{
			keepaliveResponseWriter: keepaliveResponseWriter{ResponseWriter: writer, stop: k.Stop, mu: &k.mu},
			k:                       k,
		}, k.beat, k.Stop
	}
	k := &openAIImagesJSONKeepalive{writer: writer, stop: make(chan struct{})}
	return &openAIImagesJSONKeepaliveWriter{
		keepaliveResponseWriter: keepaliveResponseWriter{ResponseWriter: writer, stop: k.Stop, mu: &k.mu},
	}, k.beat, k.Stop
}

// TestKeepaliveResponseWriterTakeoverAndPassiveAccess exercises the response
// operations shared by both heartbeat formats. Output access stops later beats,
// while observation and connection capability lookups leave heartbeat eligibility unchanged.
func TestKeepaliveResponseWriterTakeoverAndPassiveAccess(t *testing.T) {
	for _, format := range []string{"sse", "json"} {
		for _, operation := range []struct {
			name  string                               // response operation under test
			takes bool                                 // operation takes output ownership
			call  func(*testing.T, gin.ResponseWriter) // response access being exercised
		}{
			{
				name:  "Header",
				takes: true,
				call: func(t *testing.T, w gin.ResponseWriter) {
					w.Header().Set("X-Takeover", "true")
				},
			},
			{
				name:  "Write",
				takes: true,
				call: func(t *testing.T, w gin.ResponseWriter) {
					_, _ = w.Write([]byte("output"))
				},
			},
			{
				name:  "WriteString",
				takes: true,
				call: func(t *testing.T, w gin.ResponseWriter) {
					_, _ = w.WriteString("output")
				},
			},
			{
				name:  "WriteHeader",
				takes: true,
				call: func(t *testing.T, w gin.ResponseWriter) {
					w.WriteHeader(http.StatusAccepted)
				},
			},
			{
				name:  "WriteHeaderNow",
				takes: true,
				call: func(t *testing.T, w gin.ResponseWriter) {
					w.WriteHeaderNow()
				},
			},
			{
				name:  "Flush",
				takes: true,
				call: func(t *testing.T, w gin.ResponseWriter) {
					w.Flush()
				},
			},
			{
				name: "Status",
				call: func(t *testing.T, w gin.ResponseWriter) {
					require.Equal(t, http.StatusOK, w.Status())
				},
			},
			{
				name: "Size",
				call: func(t *testing.T, w gin.ResponseWriter) {
					require.Equal(t, -1, w.Size())
				},
			},
			{
				name: "Written",
				call: func(t *testing.T, w gin.ResponseWriter) {
					require.False(t, w.Written())
				},
			},
		} {
			t.Run(format+"/"+operation.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				writer, beat, stop := newKeepaliveWriterForTest(c.Writer, format)
				defer stop()
				operation.call(t, writer)
				require.Equal(t, !operation.takes, beat())
			})
		}
		t.Run(format+"/capabilities", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			probe := &keepaliveProbeWriter{
				ResponseWriter: c.Writer,
				closed:         make(chan bool),
				hijackErr:      errors.New("takeover unavailable"),
			}
			writer, beat, stop := newKeepaliveWriterForTest(probe, format)
			defer stop()
			conn, buffer, err := writer.Hijack()
			require.Nil(t, conn)
			require.Nil(t, buffer)
			require.Same(t, probe.hijackErr, err)
			require.Equal(t, (<-chan bool)(probe.closed), writer.CloseNotify())
			require.Same(t, probe, writer.Pusher())
			require.True(t, beat())
		})
	}
}

// TestKeepaliveResponseWriterWaitsForHeartbeat blocks either writing or flushing
// an active heartbeat. Header takeover and passive state reads must wait until
// the complete heartbeat operation releases the controller's mutex.
func TestKeepaliveResponseWriterWaitsForHeartbeat(t *testing.T) {
	for _, format := range []string{"sse", "json"} {
		for _, phase := range []string{"write", "flush"} {
			t.Run(format+"/"+phase, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				gate := make(chan struct{})
				probe := &keepaliveProbeWriter{ResponseWriter: c.Writer, entered: make(chan struct{})}
				if phase == "write" {
					probe.writeGate = gate
				} else {
					probe.flushGate = gate
				}
				writer, beat, stop := newKeepaliveWriterForTest(probe, format)
				defer stop()
				// Release a blocked beat before deferred stopping even if an assertion
				// fails, so a failing test cannot leave its controller waiting forever.
				defer func() {
					select {
					case <-gate:
					default:
						close(gate)
					}
				}()
				beatDone := make(chan bool, 1)
				// Wait until the beat blocks inside the captured writer, then start
				// request access so every observed wait competes with an active heartbeat.
				go func() { beatDone <- beat() }()
				<-probe.entered
				headerDone := make(chan struct{})
				statusDone := make(chan int, 1)
				go func() {
					writer.Header()
					close(headerDone)
				}()
				go func() { statusDone <- writer.Status() }()
				// Allow competing goroutines time to attempt response access.
				select {
				case <-headerDone:
					t.Fatal("header access completed before the heartbeat")
				case <-statusDone:
					t.Fatal("status read completed before the heartbeat")
				case <-time.After(20 * time.Millisecond):
				}
				// Releasing the downstream operation lets the beat finish and both
				// waiting accesses continue; header takeover then prevents every later beat.
				close(gate)
				require.True(t, <-beatDone)
				<-headerDone
				require.Equal(t, http.StatusOK, <-statusDone)
				require.True(t, recorder.Flushed)
				require.False(t, beat())
			})
		}
	}
}

// TestKeepaliveResponseWriterCleanupPreservesReplacement replaces an installed
// wrapper with another wrapper of the same concrete type. Repeated cleanup
// must retain that later owner so final output continues through the replacement.
func TestKeepaliveResponseWriterCleanupPreservesReplacement(t *testing.T) {
	for _, format := range []string{"sse", "json"} {
		t.Run(format, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			original := c.Writer
			start := StartOpenAISSEKeepalive
			if format == "json" {
				start = StartOpenAIImagesJSONKeepalive
			}
			cleanup := start(c, time.Hour)
			replacement, _, stop := newKeepaliveWriterForTest(original, format)
			defer stop()
			c.Writer = replacement
			cleanup()
			cleanup()
			require.Same(t, replacement, c.Writer)
			_, err := c.Writer.WriteString("final")
			require.NoError(t, err)
			require.Equal(t, "final", recorder.Body.String())
		})
	}
}

// TestOpenAICompactKeepaliveWriter_DelegatesWhenReady checks state and write delegation for an initialized wrapper.
// Header and string output must reach the recorder, and the wrapper must
// report the delegated status, byte size, and written flag after response takeover.
func TestOpenAICompactKeepaliveWriter_DelegatesWhenReady(t *testing.T) {
	c, rec := newCompactBridgeTestContext(t, true)
	// Delay the first beat while Header takes over through the wrapper, so
	// subsequent assertions isolate response delegation from heartbeat-driven status and byte changes.
	stop := StartOpenAICompactSSEKeepalive(c, time.Hour)
	defer stop()

	w, ok := c.Writer.(*compactKeepaliveWriter)
	require.True(t, ok)

	w.Header().Set("X-Test", "ok")
	w.WriteHeader(http.StatusAccepted)
	n, err := w.WriteString("ready")
	require.NoError(t, err)
	require.Equal(t, len("ready"), n)

	require.Equal(t, http.StatusAccepted, w.Status())
	require.Equal(t, len("ready"), w.Size())
	require.True(t, w.Written())
	require.Equal(t, "ok", rec.Header().Get("X-Test"))
	require.Equal(t, "ready", rec.Body.String())
}

// newNativeCompactWriterTestContext supplies a request and recorder for wrappers with missing components.
func newNativeCompactWriterTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	MarkOpenAICompactClientStream(c)
	return c, rec
}

// TestOpenAICompactKeepaliveWriter_NilInnerWriter_NoPanic supplies a controller
// with no response writer. Every exercised wrapper operation must return its
// documented empty or error result without panicking during output and interface lookup.
func TestOpenAICompactKeepaliveWriter_NilInnerWriter_NoPanic(t *testing.T) {
	k := &openAICompactSSEKeepalive{stop: make(chan struct{})}
	w := &compactKeepaliveWriter{
		keepaliveResponseWriter: keepaliveResponseWriter{stop: k.Stop, mu: &k.mu},
		k:                       k,
	}
	w.ResponseWriter = nil

	assert.NotPanics(t, func() {
		assert.Equal(t, 0, w.Status())
	})
	assert.NotPanics(t, func() {
		assert.Equal(t, 0, w.Size())
	})
	assert.NotPanics(t, func() {
		assert.False(t, w.Written())
	})
	assert.NotPanics(t, func() {
		assert.NotNil(t, w.Header())
	})
	assert.NotPanics(t, func() {
		n, err := w.Write([]byte("test"))
		assert.Equal(t, 0, n)
		assert.NoError(t, err)
	})
	assert.NotPanics(t, func() {
		n, err := w.WriteString("test")
		assert.Equal(t, 0, n)
		assert.NoError(t, err)
	})
	assert.NotPanics(t, func() {
		w.WriteHeader(http.StatusOK)
	})
	assert.NotPanics(t, func() {
		w.WriteHeaderNow()
	})
	assert.NotPanics(t, func() {
		w.Flush()
	})
	assert.NotPanics(t, func() {
		conn, rw, err := w.Hijack()
		assert.Nil(t, conn)
		assert.Nil(t, rw)
		assert.Error(t, err)
	})
	assert.NotPanics(t, func() {
		ch := w.CloseNotify()
		assert.NotNil(t, ch)
	})
	assert.NotPanics(t, func() {
		assert.Nil(t, w.Pusher())
	})
}

// TestOpenAICompactKeepaliveWriter_NilKeepalive_NoPanic supplies a response writer
// with no heartbeat controller. State reads must return fallback values, while
// headers and string bytes reach the recorder and flush remains callable.
func TestOpenAICompactKeepaliveWriter_NilKeepalive_NoPanic(t *testing.T) {
	c, rec := newNativeCompactWriterTestContext(t)
	w := &compactKeepaliveWriter{
		keepaliveResponseWriter: keepaliveResponseWriter{ResponseWriter: c.Writer},
	}

	assert.NotPanics(t, func() {
		assert.Equal(t, 0, w.Status())
	})
	assert.NotPanics(t, func() {
		assert.Equal(t, 0, w.Size())
	})
	assert.NotPanics(t, func() {
		assert.False(t, w.Written())
	})
	assert.NotPanics(t, func() {
		w.Header().Set("X-Test", "ok")
	})
	assert.NotPanics(t, func() {
		w.WriteHeader(http.StatusAccepted)
	})
	assert.NotPanics(t, func() {
		n, err := w.WriteString("ok")
		assert.Equal(t, 2, n)
		assert.NoError(t, err)
	})
	assert.NotPanics(t, func() {
		w.Flush()
	})
	require.Equal(t, "ok", rec.Header().Get("X-Test"))
	require.Equal(t, "ok", rec.Body.String())
}

// TestOpenAIImagesJSONKeepaliveWriter_NilGuards exercises a wrapper lacking
// heartbeat state and an underlying writer. Its response methods remain
// callable, hijacking fails, and close notification is already closed.
func TestOpenAIImagesJSONKeepaliveWriter_NilGuards(t *testing.T) {
	w := &openAIImagesJSONKeepaliveWriter{}
	require.NotPanics(t, func() {
		require.NotNil(t, w.Header())
		_, _ = w.Write([]byte("test"))
		_, _ = w.WriteString("test")
		w.WriteHeader(http.StatusOK)
		w.WriteHeaderNow()
		w.Flush()
		require.Equal(t, 0, w.Status())
		require.Equal(t, 0, w.Size())
		require.False(t, w.Written())
		require.Nil(t, w.Pusher())
	})

	conn, _, err := w.Hijack()
	require.Error(t, err)
	require.Nil(t, conn)
	select {
	case <-w.CloseNotify():
	default:
		t.Fatal("nil writer CloseNotify channel should be closed")
	}
}
