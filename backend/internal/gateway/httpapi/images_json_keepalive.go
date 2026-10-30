package httpapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// images_json_keepalive.go coordinates JSON whitespace heartbeats with final
// image response writes and retry accounting.
// Nonstreaming image generation can wait for an OAuth upstream that emits SSE
// internally. JSON whitespace keeps the HTTP response active while the
// eventual image document is still being assembled; the first heartbeat
// commits status 200, so an error arriving afterward uses that status with a
// JSON error body.
//
// Heartbeat writes and stopping share a mutex. Accessing the wrapped writer
// for final headers or output stops future heartbeats and waits for any
// current write to finish. The retained heartbeat byte count lets retry
// checks distinguish committed whitespace from actual response content, even
// after the wrapper has been restored.

// openAIImagesJSONKeepaliveKey identifies retained heartbeat state in Gin so
// error and retry checks can inspect accepted padding bytes.
const openAIImagesJSONKeepaliveKey = "openai_images_json_keepalive"

// openAIImagesJSONKeepalive coordinates whitespace writes with the final
// response writer.
type openAIImagesJSONKeepalive struct {
	// mu protects writer operations and started, stopped, and bytes together. stop
	// is closed while holding mu when ordinary stopping first disables future beats.
	mu      sync.Mutex         // heartbeat and stopping lock
	writer  gin.ResponseWriter // unwrapped original response writer
	started bool               // first heartbeat committed status
	stopped bool               // further heartbeat writes disabled
	bytes   int                // accepted heartbeat byte count
	stop    chan struct{}      // heartbeat exit notification channel
}

// openAIImagesJSONKeepaliveWriter identifies one installed JSON heartbeat
// wrapper so cleanup restores only the writer that it owns.
type openAIImagesJSONKeepaliveWriter struct {
	keepaliveResponseWriter // shared heartbeat takeover operations
}

// StartOpenAIImagesJSONKeepalive installs whitespace heartbeats and returns a
// cleanup function. A missing context or writer, or a nonpositive interval,
// returns a no-op cleanup function. The request context can end the heartbeat
// loop while retained state remains available to error handling.
//
// The wrapper stops beats before final header or output operations; heartbeat
// writes use the unwrapped writer so they do not stop themselves. Call
// cleanup at request exit. It disables beats under the same mutex used for
// writing and restores the original writer only when its own wrapper is still
// installed.
func StartOpenAIImagesJSONKeepalive(c *gin.Context, interval time.Duration) func() {
	if c == nil || c.Writer == nil || interval <= 0 {
		return func() {}
	}
	originalWriter := c.Writer
	k := &openAIImagesJSONKeepalive{
		writer: originalWriter,
		stop:   make(chan struct{}),
	}
	c.Set(openAIImagesJSONKeepaliveKey, k)
	wrappedWriter := &openAIImagesJSONKeepaliveWriter{
		keepaliveResponseWriter: keepaliveResponseWriter{
			ResponseWriter: originalWriter,
			stop:           k.Stop,
			mu:             &k.mu,
		},
	}
	c.Writer = wrappedWriter

	var reqDone <-chan struct{}
	if c.Request != nil {
		reqDone = c.Request.Context().Done()
	}
	// The timer exits on explicit stopping or request cancellation. Writes use the
	// saved writer so heartbeat output cannot trigger the final-output wrapper.
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-k.stop:
				return
			case <-reqDone:
				return
			case <-timer.C:
			}
			if !k.beat() {
				return
			}
			timer.Reset(interval)
		}
	}()

	// Stop before restoring the writer so a final response cannot race a padding
	// write; retain a replacement wrapper installed by another owner.
	return func() {
		k.Stop()
		if current, ok := c.Writer.(*openAIImagesJSONKeepaliveWriter); ok && current == wrappedWriter {
			c.Writer = originalWriter
		}
	}
}

// beat writes and flushes one JSON whitespace heartbeat unless stopping has
// already won. It returns false after stopping or a writer error, allowing
// the timer loop to exit. The first heartbeat attempt sets JSON headers and
// commits status 200 before writing padding.
//
// The mutex covers header changes, writing, byte accounting, and flushing.
// Stop and final response access wait for this sequence, so padding cannot
// interleave with a final JSON document. Accepted bytes remain counted even
// when Write also reports an error.
func (k *openAIImagesJSONKeepalive) beat() bool {
	// Keep header commitment and padding accounting inside the stopping lock so
	// final output begins after this heartbeat finishes writing and flushing.
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return false
	}
	if !k.started {
		header := k.writer.Header()
		header.Set("Content-Type", "application/json; charset=utf-8")
		header.Set("Cache-Control", "no-cache")
		header.Set("X-Accel-Buffering", "no")
		k.writer.WriteHeader(http.StatusOK)
		k.started = true
	}
	n, err := k.writer.Write([]byte(" \n"))
	k.bytes += n
	// A failed client write ends this loop, while accepted padding stays counted
	// for subsequent error and output observations on the same response.
	if err != nil {
		k.stopped = true
		return false
	}
	k.writer.Flush()
	return true
}

// Stop disables further heartbeat writes and signals the timer loop when
// stopping first occurs. It waits for any heartbeat holding the mutex;
// repeated calls leave the current state unchanged.
func (k *openAIImagesJSONKeepalive) Stop() {
	k.mu.Lock()
	k.markStoppedLocked()
	k.mu.Unlock()
}

// markStoppedLocked disables beats and closes their exit channel on the first
// ordinary stop. Callers hold mu so concurrent cleanup and writer access
// cannot close that channel twice.
func (k *openAIImagesJSONKeepalive) markStoppedLocked() {
	if k.stopped {
		return
	}
	// Publish stopping and notify the timer within one locked sequence so multiple
	// cleanup paths cannot close the exit channel more than once.
	k.stopped = true
	close(k.stop)
}

// StopOpenAIImagesJSONKeepaliveCommitted disables beats and reports whether a
// heartbeat started the status-200 response. Missing heartbeat state returns
// false. The shared mutex waits for an active heartbeat write, while the
// timer goroutine observes stopping separately.
func StopOpenAIImagesJSONKeepaliveCommitted(c *gin.Context) bool {
	k := openAIImagesJSONKeepaliveFromContext(c)
	if k == nil {
		return false
	}
	k.mu.Lock()
	k.markStoppedLocked()
	committed := k.started
	k.mu.Unlock()
	return committed
}

// OpenAIImagesJSONKeepalivePresent reports whether Gin contains typed image
// heartbeat state. Presence includes the interval before the first beat and
// the retained state after stopping, allowing error writers to interpret
// padding.
func OpenAIImagesJSONKeepalivePresent(c *gin.Context) bool {
	return openAIImagesJSONKeepaliveFromContext(c) != nil
}

// openAIImagesJSONKeepaliveFromContext retrieves typed heartbeat state
// retained on the request. Missing contexts, absent keys, and values of
// another type return nil, letting response helpers work before installation.
func openAIImagesJSONKeepaliveFromContext(c *gin.Context) *openAIImagesJSONKeepalive {
	if c == nil {
		return nil
	}
	value, ok := c.Get(openAIImagesJSONKeepaliveKey)
	if !ok {
		return nil
	}
	k, _ := value.(*openAIImagesJSONKeepalive)
	return k
}
