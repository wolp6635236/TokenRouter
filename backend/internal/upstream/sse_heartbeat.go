package upstream

import (
	"bytes"
	"strings"

	"github.com/tidwall/gjson"
)

// sse_heartbeat.go consumes upstream heartbeat frames before protocol output.
// Downstream connections have their own idle heartbeat policy, so upstream
// comments and heartbeat events must not become client content or close a
// provider retry window. Event classification gives JSON type precedence over
// the SSE event name, preserving real payloads inside mislabeled ping frames.
//
// The line filter forwards ordinary lines immediately and retains only possible
// heartbeat frames. Candidate storage has both line and byte limits; unknown
// fields or exceeded limits release the candidate unchanged. Retained lines
// are copied because scanners reuse token storage. EOF releases an incomplete
// candidate without guessing whether missing later fields would change its type.

const (
	// SSEHeartbeatFrameMaxLines and SSEHeartbeatFrameMaxBytes bound candidate
	// storage before uncertain frames revert to ordinary line-preserving forwarding.
	SSEHeartbeatFrameMaxLines = 16
	SSEHeartbeatFrameMaxBytes = 16 * 1024
)

// SSEHeartbeatLineFilter removes complete upstream heartbeat frames from ordered lines.
// One consumer owns a filter for one stream; Push results borrow input lines
// unless a candidate was retained, and Flush releases unfinished candidate bytes.
type SSEHeartbeatLineFilter struct {
	// pending and pendingBytes retain copied candidate lines until a delimiter or
	// size limit resolves them. event and data describe only that retained candidate.
	pending      [][]byte // copied heartbeat candidate lines
	pendingBytes int      // retained candidate byte count
	event        string   // candidate SSE event name
	data         []string // candidate SSE data fields
	// emitted records ordinary output in this frame so comment-only separators
	// can disappear. passthrough prevents another candidate after a failed classification.
	emitted     bool // current frame produced output
	passthrough bool // current frame bypasses classification
}

// IsSSEHeartbeatEvent identifies empty data and explicit upstream heartbeat types.
// JSON type takes precedence over event when present, preserving non-heartbeat
// application data whose SSE event name incorrectly identifies it as ping.
func IsSSEHeartbeatEvent(event, data string) bool {
	data = strings.TrimSpace(data)
	if data == "" {
		return true
	}
	kind := gjson.Get(data, "type").String()
	if kind == "" {
		kind = strings.TrimSpace(event)
	}
	return kind == "ping" || kind == "heartbeat" || kind == "keepalive"
}

// Push consumes one borrowed line including its original line ending.
// Returned lines are ready for synchronous forwarding; callers copy any input
// line they retain after the next scanner advance.
//
// Comments disappear immediately, including their final separator when no
// other field was emitted. An event name waits for its payload because JSON
// type can identify a heartbeat even under another event name. Complete
// application payloads are released immediately; heartbeat candidates wait for
// the separator before being discarded. Named errors retain their existing
// control-event handling, including errors with no data. Unknown fields or
// exceeded bounds preserve the candidate and pass through its remainder, avoiding unbounded
// buffering or reinterpretation of an application frame with extra metadata.
func (f *SSEHeartbeatLineFilter) Push(line []byte) [][]byte {
	value := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
	if bytes.HasPrefix(value, []byte(":")) {
		return nil
	}
	if len(value) == 0 {
		var out [][]byte
		if len(f.pending) > 0 {
			if !IsSSEHeartbeatEvent(f.event, strings.Join(f.data, "\n")) {
				out = append(f.pending, line)
			}
		} else if f.emitted {
			out = [][]byte{line}
		}
		*f = SSEHeartbeatLineFilter{}
		return out
	}
	if f.passthrough {
		f.emitted = true
		return [][]byte{line}
	}
	data, isData := strings.CutPrefix(string(value), "data:")
	event, isEvent := strings.CutPrefix(string(value), "event:")
	if len(f.pending) > 0 {
		if isData && len(f.pending) < SSEHeartbeatFrameMaxLines && f.pendingBytes+len(line) <= SSEHeartbeatFrameMaxBytes {
			f.retain(line)
			f.data = append(f.data, strings.TrimPrefix(data, " "))
			payload := strings.Join(f.data, "\n")
			if !IsSSEHeartbeatEvent(f.event, payload) && (gjson.Valid(payload) || strings.TrimSpace(payload) == "[DONE]") {
				// A complete application payload resolves an ambiguous event name.
				// Release it immediately so ordinary content does not wait for EOF.
				out := f.pending
				f.pending = nil
				f.data = nil
				f.pendingBytes = 0
				f.emitted = true
				f.passthrough = true
				return out
			}
			return nil
		}
		// Unrecognized fields or excess storage make classification unsafe. Preserve
		// the retained lines and the current field before bypassing the remaining frame.
		out := append(f.pending, line)
		f.pending = nil
		f.data = nil
		f.pendingBytes = 0
		f.emitted = true
		f.passthrough = true
		return out
	}
	if !f.emitted && ((isEvent && strings.TrimSpace(event) != "error") || (isData && IsSSEHeartbeatEvent("", data))) && len(line) <= SSEHeartbeatFrameMaxBytes {
		f.retain(line)
		if isEvent {
			f.event = strings.TrimSpace(event)
		}
		if isData {
			f.data = append(f.data, strings.TrimPrefix(data, " "))
		}
		return nil
	}
	f.emitted = true
	return [][]byte{line}
}

// retain copies a candidate line before its scanner storage can be reused.
// Callers enforce both storage limits before retaining; pendingBytes includes
// the original line ending because replay preserves the accepted byte sequence.
func (f *SSEHeartbeatLineFilter) retain(line []byte) {
	f.pending = append(f.pending, append([]byte(nil), line...))
	f.pendingBytes += len(line)
}

// Flush releases an incomplete candidate and resets all per-frame observations.
// Call when upstream ends, including read failure. Missing separators leave
// classification unresolved, so residual lines retain their original spelling.
func (f *SSEHeartbeatLineFilter) Flush() [][]byte {
	out := f.pending
	*f = SSEHeartbeatLineFilter{}
	return out
}
