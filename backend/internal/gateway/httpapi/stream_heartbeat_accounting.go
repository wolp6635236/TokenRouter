package httpapi

import "github.com/gin-gonic/gin"

// stream_heartbeat_accounting.go distinguishes delivered response content from
// bytes written only to keep the downstream connection alive. Queue observers,
// stream readers, and waiting controllers retain separate counters because
// their write lifetimes differ. Retry decisions subtract their combined count
// so a heartbeat from an earlier wait cannot become apparent provider output
// when a later attempt uses another heartbeat mechanism.
//
// Controller counts and the captured writer size are sampled while their
// heartbeat locks are held. Reading those values independently could observe
// a heartbeat between the two reads and incorrectly close a retry window.
// Ordinary request-side counters remain serialized by the request goroutine.

// StreamHeartbeatBytes returns the combined bytes accepted from all heartbeat
// sources for this request. Nil contexts return zero; callers serialize ordinary
// request-side recording before inspecting the accumulated count.
// @project-doc docs/architecture/gateway_request_lifecycle.md#sse_keepalive
func StreamHeartbeatBytes(c *gin.Context) int {
	count, _ := streamHeartbeatSnapshot(c)
	return count
}

// HeartbeatAdjustedWrittenSize reports response bytes after excluding every
// recorded heartbeat source. Nil output returns Gin's unwritten sentinel, -1.
//
// When no heartbeat bytes were recorded, the writer's size passes through,
// including zero after headers alone were committed. Positive heartbeat counts
// that exhaust the response size restore -1, keeping a heartbeat-only response
// eligible for provider recovery. Attempt baselines and subsequent observations
// must both use this function so a change in heartbeat mechanism cannot change
// the meaning of the comparison.
func HeartbeatAdjustedWrittenSize(c *gin.Context) int {
	heartbeats, size := streamHeartbeatSnapshot(c)
	if heartbeats == 0 {
		return size
	}
	if content := size - heartbeats; content > 0 {
		return content
	}
	return -1
}

// streamHeartbeatSnapshot samples heartbeat counters together with response
// size, preserving the atomic observations supplied by waiting controllers.
// Callers use the returned pair without rereading the live writer.
//
// Waiting controllers write through their captured writer while holding their
// own mutex. Sampling that same writer under the mutex avoids reentering the
// controller's wrapper and deadlocking its Size method. Compact/SSE and JSON
// controllers serve distinct response formats; request-side counters are read
// after their writes by the request goroutine and require no additional lock.
func streamHeartbeatSnapshot(c *gin.Context) (int, int) {
	if c == nil || c.Writer == nil {
		return 0, -1
	}
	count := 0
	for _, key := range []string{gatewayStreamHeartbeatBytesKey, openAIStreamKeepaliveBytesContextKey} {
		value, _ := c.Get(key)
		written, _ := value.(int)
		count += written
	}
	writer := c.Writer
	if value, ok := c.Get(openAICompactSSEKeepaliveKey); ok {
		if k, valid := value.(*openAICompactSSEKeepalive); valid && k != nil {
			// Hold the controller until size is captured, using its inner writer
			// so the wrapper cannot attempt to acquire this same mutex again.
			k.mu.Lock()
			defer k.mu.Unlock()
			count += k.bytes
			writer = k.writer
		}
	}
	if k := openAIImagesJSONKeepaliveFromContext(c); k != nil {
		// JSON padding uses the same observation ordering as SSE comments so
		// accepted whitespace cannot appear as content during a concurrent beat.
		k.mu.Lock()
		defer k.mu.Unlock()
		count += k.bytes
		writer = k.writer
	}
	if value, ok := c.Get(sseKeepaliveWriterKey); ok {
		if w, valid := value.(*sseKeepaliveWriter); valid && w != nil {
			// Lock beneath the waiting controllers in their write order. Read the
			// inner size directly so a concurrent beat cannot split this observation.
			w.mu.Lock()
			defer w.mu.Unlock()
			return count + downstreamSSEHeartbeatBytes(c), w.ResponseWriter.Size()
		}
	}
	return count, writer.Size()
}

// downstreamSSEHeartbeatBytes reads accepted bytes from the retained text controller.
// Missing or mistyped context values contribute zero; the counter remains
// available after middleware cleanup restores the original response writer.
func downstreamSSEHeartbeatBytes(c *gin.Context) int {
	value, _ := c.Get(sseKeepaliveWriterKey)
	w, _ := value.(*sseKeepaliveWriter)
	if w == nil {
		return 0
	}
	return int(w.bytes.Load())
}
