package httpapi

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// response_keepalive_writer.go coordinates response access with a heartbeat
// controller that writes to the captured downstream writer. Both SSE comments
// and JSON padding can be in flight when the request begins constructing its
// final response. Header and output operations first stop their controller,
// whose mutex covers the entire heartbeat write and flush, before forwarding
// access to the captured writer. Final response callers still serialize their
// own writes after this handoff.
//
// Status, size, and commitment reads use that same mutex without stopping
// heartbeats. Connection capability lookups and hijacking retain their direct
// delegation behavior, so callers stop heartbeats explicitly before hijacking.
// Protocol controllers retain framing, byte accounting, timer lifetimes, and
// wrapper identity; this adapter owns only the shared response-access contract.

// keepaliveResponseWriter serializes response access against an associated
// heartbeat controller while preserving the captured writer's response interfaces.
type keepaliveResponseWriter struct {
	gin.ResponseWriter // captured downstream response writer
	// stop and mu belong to the same controller for this captured writer. The
	// callback stops future beats and waits through active writing and flushing;
	// the mutex serializes passive reads with that controller's heartbeat updates.
	// stop may be nil when no controller is bound, leaving response delegation
	// available without an associated heartbeat controller.
	stop func() // heartbeat stop callback
	// mu may be nil when no controller is bound; passive reads then return zero
	// or false without consulting the captured writer.
	mu *sync.Mutex // controller heartbeat state mutex
}

// suspend idempotently stops heartbeats before response construction, including
// Header access. A nil stop callback is ignored. The controller callback waits
// for the current heartbeat to finish, allowing delegated output after the
// controller loses permission to write more heartbeat bytes.
func (w *keepaliveResponseWriter) suspend() {
	if w.stop == nil {
		return
	}
	w.stop()
}

// Header stops heartbeats before exposing headers for request response construction.
// A missing inner writer returns a fresh empty map. Otherwise callers receive
// the inner header map and continue to own its response-building mutations.
func (w *keepaliveResponseWriter) Header() http.Header {
	w.suspend()
	if w.ResponseWriter == nil {
		return http.Header{}
	}
	return w.ResponseWriter.Header()
}

// Write stops heartbeats before delegating request bytes to the captured writer.
// A missing inner writer returns zero without error. Otherwise the downstream
// byte count and error pass back to the response producer unchanged.
func (w *keepaliveResponseWriter) Write(data []byte) (int, error) {
	w.suspend()
	if w.ResponseWriter == nil {
		return 0, nil
	}
	return w.ResponseWriter.Write(data)
}

// WriteString stops heartbeats before delegating string output to the captured
// writer.
// A missing inner writer returns zero without error. The downstream string
// write otherwise supplies the byte count and error returned to the caller.
func (w *keepaliveResponseWriter) WriteString(s string) (int, error) {
	w.suspend()
	if w.ResponseWriter == nil {
		return 0, nil
	}
	return w.ResponseWriter.WriteString(s)
}

// WriteHeader stops heartbeats before selecting the request's response status.
// It delegates the status to the captured writer when present. A missing
// inner writer leaves status selection without an output target.
func (w *keepaliveResponseWriter) WriteHeader(code int) {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// WriteHeaderNow stops heartbeats before committing the captured writer's pending
// headers. Missing output is ignored. The controller's stop callback waits for
// any beat, so later heartbeat bytes cannot interleave with response construction.
func (w *keepaliveResponseWriter) WriteHeaderNow() {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.WriteHeaderNow()
}

// Flush stops heartbeats before flushing the request's captured response writer.
// A missing inner writer is ignored. Callers that need recurring heartbeat
// flushes leave them to beat, which operates directly on its writer.
func (w *keepaliveResponseWriter) Flush() {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.Flush()
}

// Hijack returns an error when the inner response writer is missing.
// Otherwise it delegates connection takeover to that writer. Callers stop the
// heartbeat controller before taking over the connection because this method
// leaves controller state unchanged.
func (w *keepaliveResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.ResponseWriter == nil {
		return nil, nil, errors.New("response writer released")
	}
	return w.ResponseWriter.Hijack()
}

// CloseNotify exposes the captured writer's connection-close notification channel.
// A missing inner writer yields a newly closed channel so receivers can proceed.
// This lookup leaves heartbeat generation and controller state unchanged.
func (w *keepaliveResponseWriter) CloseNotify() <-chan bool {
	if w.ResponseWriter == nil {
		ch := make(chan bool)
		// Receiving from this closed channel yields false but wakes selectors,
		// allowing notification consumers to proceed when no inner connection
		// channel is available.
		close(ch)
		return ch
	}
	return w.ResponseWriter.CloseNotify()
}

// Pusher returns the captured writer's optional HTTP push interface.
// A missing writer yields nil; supported writers decide whether push is
// available. Looking up this interface leaves heartbeat generation and
// controller state unchanged.
func (w *keepaliveResponseWriter) Pusher() http.Pusher {
	if w.ResponseWriter == nil {
		return nil
	}
	return w.ResponseWriter.Pusher()
}

// Status reads the inner writer under the heartbeat mutex, or returns zero if
// either is absent.
// Reading status keeps heartbeats enabled. The lock prevents observing the writer
// while beat changes its headers, status, or byte count under the same mutex.
func (w *keepaliveResponseWriter) Status() int {
	if w.mu == nil || w.ResponseWriter == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Status()
}

// Size reads the captured writer's byte count under the heartbeat mutex.
// Missing output or mutex returns zero. The read keeps heartbeats enabled;
// callers use adjusted-size accounting when heartbeat bytes should be excluded.
func (w *keepaliveResponseWriter) Size() int {
	if w.mu == nil || w.ResponseWriter == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Size()
}

// Written reports the captured writer's committed-output state under the
// heartbeat mutex.
// Missing output or mutex returns false. Heartbeat commitment counts as
// written here, so callers use their protocol's adjusted byte count to
// distinguish heartbeat bytes from actual response content.
func (w *keepaliveResponseWriter) Written() bool {
	if w.mu == nil || w.ResponseWriter == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Written()
}
