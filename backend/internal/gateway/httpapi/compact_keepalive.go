package httpapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// compact_keepalive.go coordinates SSE heartbeats with response takeover and
// tracks heartbeat bytes for retry decisions.
// Compact JSON work and image response-header waits can leave an SSE client
// idle before model output arrives. The first heartbeat waits one interval,
// then commits HTTP 200 and sends a comment. Later errors consequently need
// stream-compatible output.
// Heartbeat bytes are tracked separately because they keep the connection active
// while the upstream attempt may still be eligible for a pre-output retry.
//
// The request writer wrapper stops heartbeat generation before response-building
// operations take over its writer. A mutex protects heartbeat writes and state
// reads, and stopping waits for any heartbeat currently holding that mutex.
// Shared accounting subtracts all recorded heartbeat sources, returning
// Gin's unwritten sentinel when recorded comments account for all written bytes.
// The request output path still serializes response writes and cleanup.
// Successive waits retain committed framing and cumulative comment bytes so a
// heartbeat from an earlier attempt cannot become apparent model output later.

// Keepalive context keys distinguish the compact heartbeat controller from the
// separately accumulated bytes written by ordinary response-reading heartbeats.
const (
	// openAIStreamKeepaliveBytesContextKey stores ordinary SSE heartbeat byte counts.
	openAIStreamKeepaliveBytesContextKey = "openai_stream_keepalive_bytes"

	// openAICompactSSEKeepaliveKey stores the shared Compact and Images SSE heartbeat.
	openAICompactSSEKeepaliveKey = "openai_compact_sse_keepalive"
)

// openAICompactSSEKeepalive sends SSE comments while waiting for an upstream
// result or response header. The first heartbeat waits one interval, allowing
// earlier failures to use JSON and their HTTP status. Once heartbeat output
// begins, error handlers select a stream-compatible terminal error.
type openAICompactSSEKeepalive struct {
	// mu protects writer heartbeat operations and all mutable controller state.
	// Stop acquires the same mutex so a response producer waits for any ongoing beat.
	mu sync.Mutex // heartbeat state lock
	// writer is the response writer captured before installing the request wrapper.
	// beat uses it directly so a heartbeat does not stop its own controller.
	writer gin.ResponseWriter // captured response writer
	// started records that this request selected SSE headers and HTTP 200,
	// including an earlier wait. Errors retain that framing across attempts.
	started bool // first heartbeat setup ran
	// stopped prevents subsequent beats. Explicit stopping closes stop, while a
	// failed heartbeat write sets this flag and lets its worker exit directly.
	stopped bool // later beats are disabled
	// writeFailed latches a downstream heartbeat failure across waits. A later
	// attempt may collect upstream usage but cannot restart header-wait heartbeats.
	writeFailed bool // downstream heartbeat write failed
	// bytes counts all reported heartbeat-comment writes for this request under
	// mu, including previous waits. Adjusted-size checks subtract these comments.
	bytes int // reported heartbeat byte count
	// stop wakes the timer loop on explicit stopping. A heartbeat write failure
	// terminates the worker through beat's result and can leave this channel open.
	// Startup initializes the channel before publishing this controller.
	stop chan struct{} // worker stop notification
}

// compactKeepaliveWriter identifies one installed SSE heartbeat wrapper and
// delegates response access to its associated controller's shared writer adapter.
type compactKeepaliveWriter struct {
	keepaliveResponseWriter // shared heartbeat takeover operations
	// k identifies the controller whose captured writer a later SSE wait may
	// recover. Wrappers belonging to another controller retain their existing ownership.
	k *openAICompactSSEKeepalive
}

// StartOpenAICompactSSEKeepalive starts heartbeats for compact requests marked as client-streaming and returns an
// idempotent stop function.
// A missing marker or nonpositive interval disables heartbeats. The wrapper stops them
// before response construction, including local rejections written inside Forward.
func StartOpenAICompactSSEKeepalive(c *gin.Context, interval time.Duration) func() {
	if !OpenAICompactClientWantsStream(c) {
		return func() {}
	}
	return StartOpenAISSEKeepalive(c, interval)
}

// StartOpenAISSEKeepalive starts heartbeats for callers that have already selected SSE output.
// Callers include Compact output and Images response-header waits.
// Use HeartbeatAdjustedWrittenSize to exclude heartbeat bytes from failover checks (#3887).
// A nil context or writer, or a nonpositive interval, returns a no-op cleanup.
// A missing request supplies no cancellation channel, so cleanup ends its worker.
// Callers start and clean up from their request output path, because both steps
// can replace c.Writer while the controller owns its separate heartbeat work.
//
// A previous controller is stopped before replacement, retaining its commitment
// and cumulative byte observations. A failed previous write or canceled request
// prevents another worker from starting. The worker observes cancellation or
// explicit stop between timer beats. Heartbeats and response takeover share the
// controller mutex, so stopping prevents subsequent heartbeat writes after any
// in-progress beat finishes. Cleanup restores the captured writer only while
// its own wrapper remains installed; later wrappers retain their ownership.
func StartOpenAISSEKeepalive(c *gin.Context, interval time.Duration) func() {
	if c == nil || c.Writer == nil || interval <= 0 {
		return func() {}
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return func() {}
	}
	originalWriter := c.Writer
	k := &openAICompactSSEKeepalive{
		writer: originalWriter,
		stop:   make(chan struct{}),
	}
	// Each completed wait leaves its state available for error framing and retry
	// checks. Transfer it before publishing another controller so old comments
	// remain excluded from output decisions and no earlier worker can still write.
	if value, ok := c.Get(openAICompactSSEKeepaliveKey); ok {
		if previous, valid := value.(*openAICompactSSEKeepalive); valid && previous != nil {
			previous.Stop()
			previous.mu.Lock()
			k.started = previous.started
			k.bytes = previous.bytes
			k.writeFailed = previous.writeFailed
			previous.mu.Unlock()
			if k.writeFailed {
				return func() {}
			}
			if wrapped, wrappedOK := originalWriter.(*compactKeepaliveWriter); wrappedOK && wrapped.k == previous {
				originalWriter = wrapped.ResponseWriter
				k.writer = originalWriter
			}
		}
	}
	c.Set(openAICompactSSEKeepaliveKey, k)
	wrappedWriter := &compactKeepaliveWriter{
		keepaliveResponseWriter: keepaliveResponseWriter{
			ResponseWriter: originalWriter,
			stop:           k.Stop,
			mu:             &k.mu,
		},
		k: k,
	}
	c.Writer = wrappedWriter

	var reqDone <-chan struct{}
	if c.Request != nil {
		reqDone = c.Request.Context().Done()
	}
	// Read cancellation and the stop channel between beats so the timer worker
	// can leave independently of the request's eventual response-building and writer restoration.
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-k.stop:
				return
			case <-reqDone:
				// Cancellation ends this worker while cleanup still owns controller
				// stopping and writer restoration. Keep the framing state available to later error handling.
				return
			case <-timer.C:
			}
			// A tick and cancellation can become ready together. Give an already
			// canceled request precedence before attempting another downstream write.
			select {
			case <-reqDone:
				return
			default:
			}
			if !k.beat() {
				return
			}
			timer.Reset(interval)
		}
	}()
	return func() {
		k.Stop()
		// A caller may have installed another writer after this wrapper. Restore
		// the captured middleware writer only when this cleanup still owns c.Writer.
		if current, ok := c.Writer.(*compactKeepaliveWriter); ok && current == wrappedWriter {
			c.Writer = originalWriter
		}
	}
}

// beat selects SSE framing on the first beat and writes a comment under the mutex.
// A false result tells the goroutine to exit after stopping or a downstream write failure.
// The controller holds mu through header selection, write accounting, and flush,
// allowing Stop to wait for the complete heartbeat operation before takeover.
//
// The first beat selects SSE status even if its body write later fails. A failed
// write marks the controller stopped and returns false directly to the worker;
// explicit stop-channel closure belongs to markStoppedLocked. Reported bytes
// remain available for adjusted-size checks so a partially written comment does
// not become model output merely because the heartbeat failed.
func (k *openAICompactSSEKeepalive) beat() bool {
	// Keep Flush inside the protected beat. Stopping between Write and Flush
	// would let heartbeat delivery overlap the caller's takeover of the captured writer.
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return false
	}
	if !k.started {
		// Header commitment is delayed until this first beat. An earlier local
		// rejection can still use its own HTTP status before heartbeat framing is chosen.
		header := k.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		k.writer.WriteHeader(http.StatusOK)
		k.started = true
	}
	n, err := k.writer.Write([]byte(": keepalive\n\n"))
	k.bytes += n
	if err != nil {
		// A failed heartbeat cannot keep the client connection active. Mark it
		// stopped and return directly so the worker exits without waiting on the stop channel.
		k.stopped = true
		k.writeFailed = true
		return false
	}
	k.writer.Flush()
	return true
}

// Stop prevents later beats and waits for any current heartbeat operation.
// Repeated calls and concurrent controller stops are supported. The mutex
// covers heartbeat state; callers still serialize their own response writes.
// The controller needs the stop channel established during startup.
//
// When Stop obtains the mutex before a pending beat, that beat sees stopped
// and exits. When a beat obtains it first, Stop waits through that beat's write
// and flush before marking the controller stopped. This makes request output
// takeover safe from subsequent controller writes without requiring a worker join.
func (k *openAICompactSSEKeepalive) Stop() {
	k.mu.Lock()
	k.markStoppedLocked()
	k.mu.Unlock()
}

// markStoppedLocked marks explicit stopping and wakes the timer loop once.
// Callers hold mu and supply the initialized stop channel. An already stopped
// controller is left untouched, including one stopped by a write failure.
func (k *openAICompactSSEKeepalive) markStoppedLocked() {
	if k.stopped {
		return
	}
	k.stopped = true
	// The mutex makes explicit stopping single-winner. Closing the channel wakes
	// the timer loop, while repeated callers observe stopped and avoid closing it again.
	close(k.stop)
}

// StopOpenAICompactSSEKeepaliveCommitted stops waiting heartbeats and reports
// whether waiting or ordinary stream heartbeats selected SSE framing. Callers
// serialize ordinary heartbeat recording with this check. Missing controller
// state still permits recorded ordinary comments to establish commitment.
//
// Stopping and reading started share one lock, connecting the framing decision
// to the last permitted waiting heartbeat, including a failed comment write.
// Ordinary comments may have committed the response after a fast header wait
// ended; their recorded bytes retain that framing through retries. This function
// leaves the request wrapper installed; the cleanup returned by
// StartOpenAISSEKeepalive owns writer restoration.
func StopOpenAICompactSSEKeepaliveCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	streamCommitted := false
	if value, ok := c.Get(openAIStreamKeepaliveBytesContextKey); ok {
		written, _ := value.(int)
		streamCommitted = written > 0
	}
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	if !ok {
		return streamCommitted
	}
	k, ok := value.(*openAICompactSSEKeepalive)
	if !ok || k == nil {
		return streamCommitted
	}
	k.mu.Lock()
	k.markStoppedLocked()
	committed := k.started
	k.mu.Unlock()
	return committed || streamCommitted
}

// RecordOpenAIStreamKeepaliveBytes accumulates bytes written by stream heartbeats.
// Nil contexts and nonpositive writes are ignored. The caller records reported
// comment bytes after writing them and serializes this read-add-set sequence
// within its ordinary stream output loop.
func RecordOpenAIStreamKeepaliveBytes(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	current := 0
	if value, ok := c.Get(openAIStreamKeepaliveBytesContextKey); ok {
		current, _ = value.(int)
	}
	// Record the count reported by ordinary stream output separately from compact
	// controller bytes so adjusted-size checks can subtract each heartbeat source once.
	c.Set(openAIStreamKeepaliveBytesContextKey, current+written)
}

// OpenAIStreamClientOutputStarted combines the caller's output marker with
// adjusted writer state to decide whether protocol output has started.
// A true local marker wins before context inspection; otherwise a missing
// context or writer returns false.
//
// Recorded heartbeat-only bytes yield a negative adjusted size and keep this
// check false. With no positive heartbeat count, zero still counts as started:
// Gin uses zero after header commitment, even before body bytes arrive. The
// caller maintains its local marker separately from heartbeat framing and
// records ordinary comments before using the adjusted-size decision.
func OpenAIStreamClientOutputStarted(c *gin.Context, localStarted bool) bool {
	if localStarted {
		return true
	}
	if c == nil || c.Writer == nil {
		return false
	}
	// A compact heartbeat commits HTTP 200 but carries no model output. Excluding
	// its bytes keeps this output check from independently rejecting a pre-output retry.
	return HeartbeatAdjustedWrittenSize(c) >= 0
}
