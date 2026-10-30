package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tidwall/gjson"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	geminiwire "github.com/TokenFlux/TokenRouter/internal/protocol/gemini"
)

// gemini_stream.go consumes bounded Gemini SSE frames and distinguishes model
// completion from transport closure. Native REST, wrapped OAuth, and protocol
// conversion share these rules so an empty connection or a provider error cannot
// become a successful generated response. Platform adapters supply their own
// envelope extraction and synchronously consume compact JSON payloads. Candidate
// completion is accumulated across frames; usage-only tails remain available to
// those consumers after the final candidate has finished.
//
// One reader worker owns framing while the caller owns delivery and completion
// state. Actual body reads renew the idle clock, including heartbeat bytes. A
// completed generation permits five seconds of trailing observations, independent
// of subsequent activity. Every return closes and joins the worker, and replacing
// the response body with an idempotent closer preserves executor cleanup without
// releasing the underlying transport twice.

const (
	// geminiStreamTailLimit bounds trailing usage collection after completion,
	// preventing an upstream that never closes from delaying client EOF indefinitely.
	geminiStreamTailLimit = 5 * time.Second
	// geminiStreamReadBuffer bounds ordinary I/O chunks independently of the
	// configured whole-frame limit, so partial lines continue renewing activity.
	geminiStreamReadBuffer = 32 * 1024
)

// GeminiStreamOptions supplies framing limits and platform envelope extraction.
type GeminiStreamOptions struct {
	// IdleTimeout bounds gaps between actual upstream reads. Nonpositive values disable only
	// this deadline; cancellation and the post-completion tail limit still apply.
	IdleTimeout time.Duration
	// MaxFrameBytes bounds all bytes in a frame, including ignored SSE fields.
	// Nonpositive values select the existing shared SSE line-size default.
	MaxFrameBytes int
	// Candidates is the final upstream request's candidateCount. Nonpositive
	// values select one; repeated indices never increase the completion count.
	Candidates int
	// Unwrap removes a platform envelope before validation. Nil selects native
	// REST payloads, and returned bytes need remain valid only during delivery.
	Unwrap func([]byte) ([]byte, error)
}

// GeminiStreamEvent carries one normalized generation payload or completion notification.
type GeminiStreamEvent struct {
	Data []byte // compact JSON payload
	// Terminal marks the first whole-response completion, after all requested
	// candidates finish or a blocking response or compatible DONE is accepted.
	Terminal bool
}

// GeminiStreamResult retains completion evidence even when later consumption fails.
type GeminiStreamResult struct {
	Completed bool // valid generation completion
	// UsageJSON retains present, nonnull usage fields across sparse frames.
	// Adapters project this snapshot with their existing token-accounting rules.
	UsageJSON []byte
	// TailIncomplete reports transport loss or expiry after valid completion.
	// The reader logs the cause without turning completed generation into failure.
	TailIncomplete bool
}

// GeminiStreamError preserves an upstream status or classified local stream failure.
type GeminiStreamError struct {
	Status  int    // intended HTTP status
	Kind    string // stable failure classification
	Message string // upstream or local explanation
	// Body retains an explicit upstream error for the existing provider policy.
	// Local protocol failures leave it empty so presentation can construct an envelope.
	Body  []byte
	Cause error // wrapped transport failure
}

// geminiStreamBody records activity and releases its borrowed transport once.
type geminiStreamBody struct {
	io.ReadCloser              // underlying response body
	last          atomic.Int64 // last read Unix nanoseconds
	once          sync.Once    // transport closure guard
	err           error        // retained close error
}

// geminiStreamFrame transfers one bounded SSE event from the read worker.
type geminiStreamFrame struct {
	event string // upstream event label
	data  []byte // joined data fields
	err   error  // final read failure
}

// geminiStreamState accumulates candidate completion independently of content delivery.
type geminiStreamState struct {
	options  GeminiStreamOptions        // normalized read options
	finished map[int]struct{}           // completed candidate indices
	content  bool                       // observed candidate content
	usage    map[string]json.RawMessage // accumulated usage fields
	result   GeminiStreamResult         // retained stream outcome
}

// Error identifies the stream failure while preserving its selected diagnostic message.
func (e *GeminiStreamError) Error() string {
	return fmt.Sprintf("gemini %s: %s", e.Kind, e.Message)
}

// Unwrap exposes the original read failure for cancellation and transport classification.
func (e *GeminiStreamError) Unwrap() error { return e.Cause }

// GeminiCandidateCount reads candidateCount from the final REST or wrapped request.
// Missing, nonpositive, or nonnumeric values retain the single-candidate default.
// Both accepted generation-config spellings are supported before platform dispatch.
func GeminiCandidateCount(body []byte) int {
	for _, prefix := range []string{"", "request."} {
		for _, config := range []string{"generationConfig.", "generation_config."} {
			for _, key := range []string{"candidateCount", "candidate_count"} {
				value := gjson.GetBytes(body, prefix+config+key)
				if value.Type == gjson.Number && value.Int() > 0 {
					return int(value.Int())
				}
			}
		}
	}
	return 1
}

// ReadGeminiStream validates and delivers ordered Gemini events until completion
// and EOF, a bounded tail, or failure. The response and callback are required;
// callback bytes are borrowed, and callback errors return without reclassification.
//
// The worker only reads frames; delivery and candidate state stay on this goroutine.
// Cancellation and deadlines close the transport before joining that worker, so
// the supplied body's Close must interrupt blocked reads. HTTP response bodies
// satisfy that requirement. Replacing resp.Body makes repeated executor cleanup
// safe. Local heartbeats never renew the read deadline, and explicit provider
// errors remain failures even after a generation terminal was observed.
// @project-doc docs/interfaces/gemini_upstream.md#gemini_stream_lifecycle
func ReadGeminiStream(ctx context.Context, resp *http.Response, options GeminiStreamOptions, consume func(GeminiStreamEvent) error) (GeminiStreamResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.MaxFrameBytes <= 0 {
		options.MaxFrameBytes = DefaultSSELineLimit
	}
	if options.Candidates <= 0 {
		options.Candidates = 1
	}
	state := geminiStreamState{options: options, finished: make(map[int]struct{})}
	if resp == nil || resp.Body == nil {
		return state.result, newGeminiStreamError("stream_empty", "Upstream returned an empty response", nil)
	}
	body := &geminiStreamBody{ReadCloser: resp.Body}
	body.last.Store(time.Now().UnixNano())
	resp.Body = body
	if err := ctx.Err(); err != nil {
		_ = body.Close()
		return state.result, err
	}
	frames := make(chan geminiStreamFrame, 1)
	stop := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		readGeminiFrames(body, options.MaxFrameBytes, frames, stop)
	}()
	defer func() {
		// Release a worker blocked on either network reads or frame delivery before
		// waiting for its exit; the executor may safely close the installed body again.
		close(stop)
		_ = body.Close()
		<-joined
	}()
	var idle *time.Timer
	var idleC <-chan time.Time
	if options.IdleTimeout > 0 {
		idle = time.NewTimer(options.IdleTimeout)
		idleC = idle.C
		defer idle.Stop()
	}
	var tail *time.Timer
	var tailC <-chan time.Time
	defer func() {
		if tail != nil {
			tail.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return state.result, ctx.Err()
		case <-tailC:
			return state.incompleteTail(ctx, errors.New("usage tail exceeded five seconds")), nil
		case <-idleC:
			remaining := options.IdleTimeout - time.Since(time.Unix(0, body.last.Load()))
			if remaining > 0 {
				idle.Reset(remaining)
				continue
			}
			if state.result.Completed {
				return state.incompleteTail(ctx, errors.New("usage tail became idle")), nil
			}
			return state.result, newGeminiStreamError("stream_timeout", "Upstream stream became idle", nil)
		case frame := <-frames:
			if frame.err != nil {
				if ctx.Err() != nil {
					return state.result, ctx.Err()
				}
				if errors.Is(frame.err, context.Canceled) {
					return state.result, frame.err
				}
				if errors.Is(frame.err, io.EOF) {
					if state.result.Completed {
						return state.result, nil
					}
					if !state.content {
						return state.result, newGeminiStreamError("stream_empty", "Upstream stream contained no completed response", nil)
					}
					return state.result, newGeminiStreamError("stream_incomplete", "Upstream stream ended before completion", nil)
				}
				if _, ok := errors.AsType[*GeminiStreamError](frame.err); ok {
					return state.result, frame.err
				}
				if state.result.Completed {
					return state.incompleteTail(ctx, frame.err), nil
				}
				return state.result, newGeminiStreamError("stream_read_error", "Failed to read upstream stream", frame.err)
			}
			wasCompleted := state.result.Completed
			event, done, err := state.accept(frame)
			if err != nil {
				return state.result, err
			}
			if len(event.Data) > 0 || event.Terminal {
				if err := consume(event); err != nil {
					return state.result, err
				}
			}
			if done {
				return state.result, nil
			}
			if !wasCompleted && state.result.Completed {
				// Completion starts one absolute tail budget. Later usage or heartbeats
				// cannot extend it and hold the already completed client response open.
				tail = time.NewTimer(geminiStreamTailLimit)
				tailC = tail.C
			}
		}
	}
}

// incompleteTail records bounded collection loss without revoking model completion.
// Call only after a valid terminal. The diagnostic contains the transport cause,
// while the returned result retains the completed generation and partial observations.
func (s *geminiStreamState) incompleteTail(ctx context.Context, cause error) GeminiStreamResult {
	s.result.TailIncomplete = true
	logging.FromContext(ctx).Warn("gemini.stream_tail_incomplete", zap.String("error", logredact.SanitizeUpstreamQueries(cause.Error())))
	return s.result
}

// accept validates one complete frame and advances whole-response completion.
// A compact payload borrows storage until the next consumer call; DONE emits only
// a terminal notification. Explicit errors never enter generation conversion.
//
// Completion requires every expected candidate index, an explicit blocking result,
// or DONE after actual candidate content. Metadata and usage alone cannot satisfy
// DONE. Unknown JSON fields survive compaction and platform unwrapping. Repeated
// finish reasons do not create additional terminal notifications, while trailing
// payloads can still update accounting after the first terminal was delivered.
func (s *geminiStreamState) accept(frame geminiStreamFrame) (GeminiStreamEvent, bool, error) {
	data := bytes.TrimSpace(frame.data)
	if bytes.Equal(data, []byte("[DONE]")) {
		if !s.result.Completed && !s.content {
			return GeminiStreamEvent{}, false, newGeminiStreamError("stream_empty", "Upstream ended without candidate content", nil)
		}
		event := GeminiStreamEvent{Terminal: !s.result.Completed}
		s.result.Completed = true
		return event, true, nil
	}
	business := gjson.GetBytes(data, "candidates").Exists() || gjson.GetBytes(data, "response").Exists() || gjson.GetBytes(data, "error").Exists() || gjson.GetBytes(data, "promptFeedback").Exists()
	if !business && frame.event != "error" && IsSSEHeartbeatEvent(frame.event, string(data)) {
		return GeminiStreamEvent{}, false, nil
	}
	if !json.Valid(data) {
		return GeminiStreamEvent{}, false, newGeminiStreamError("stream_invalid_json", "Upstream stream contained invalid JSON", nil)
	}
	outerError := gjson.GetBytes(data, "error")
	if s.options.Unwrap != nil && (!outerError.Exists() || outerError.Type == gjson.Null) {
		var err error
		data, err = s.options.Unwrap(data)
		if err != nil || !json.Valid(data) {
			return GeminiStreamEvent{}, false, newGeminiStreamError("stream_invalid_json", "Invalid wrapped upstream response", err)
		}
	}
	root := gjson.ParseBytes(data)
	if !root.IsObject() {
		return GeminiStreamEvent{}, false, newGeminiStreamError("stream_invalid_json", "Upstream stream payload must be an object", nil)
	}
	fields := root.Map()
	candidates := root.Get("candidates")
	emptyCandidates := candidates.Type == gjson.Null || (candidates.IsArray() && len(candidates.Array()) == 0)
	if len(fields) == 0 || (len(fields) == 1 && candidates.Exists() && emptyCandidates) {
		// Some Gemini relays send empty candidate arrays while thinking. These
		// placeholders renew upstream activity without becoming SDK response chunks.
		return GeminiStreamEvent{}, false, nil
	}
	if candidates.Exists() && candidates.Type != gjson.Null && !candidates.IsArray() {
		return GeminiStreamEvent{}, false, newGeminiStreamError("stream_invalid_json", "Invalid upstream candidate list", nil)
	}
	errorValue := root.Get("error")
	if frame.event == "error" || (errorValue.Exists() && errorValue.Type != gjson.Null) {
		status := int(errorValue.Get("code").Int())
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		message := logredact.SanitizeUpstreamQueries(ExtractErrorMessage(data))
		if message == "" {
			message = "Upstream stream failed"
		}
		return GeminiStreamEvent{}, false, &GeminiStreamError{Status: status, Kind: "upstream_error", Message: message, Body: bytes.Clone(data)}
	}
	observed := geminiwire.ObservePayload(data)
	if usage := root.Get("usageMetadata"); usage.IsObject() {
		if s.usage == nil {
			s.usage = make(map[string]json.RawMessage)
		}
		usage.ForEach(func(key, value gjson.Result) bool {
			if value.Type != gjson.Null {
				s.usage[key.String()] = json.RawMessage(value.Raw)
			}
			return true
		})
		// Sparse tail frames cannot erase prior input or cache counters. Keep the
		// accounting snapshot separate from native payloads, which retain upstream fields.
		s.result.UsageJSON, _ = json.Marshal(map[string]any{"usageMetadata": s.usage})
	}
	s.content = s.content || observed.Semantic
	candidates.ForEach(func(index, candidate gjson.Result) bool {
		value := candidate.Get("finishReason")
		finish := strings.TrimSpace(value.String())
		if value.Type != gjson.String || finish == "" || finish == "FINISH_REASON_UNSPECIFIED" {
			return true
		}
		candidateIndex := int(index.Int())
		if value := candidate.Get("index"); value.Type == gjson.Number {
			candidateIndex = int(value.Int())
		}
		if candidateIndex >= 0 {
			s.finished[candidateIndex] = struct{}{}
		}
		return true
	})
	blocked := root.Get("promptFeedback.blockReason")
	completed := len(s.finished) >= s.options.Candidates || (blocked.Type == gjson.String && blocked.String() != "" && blocked.String() != "BLOCK_REASON_UNSPECIFIED")
	event := GeminiStreamEvent{Terminal: !s.result.Completed && completed}
	s.result.Completed = s.result.Completed || completed
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return GeminiStreamEvent{}, false, newGeminiStreamError("stream_invalid_json", "Invalid upstream response", err)
	}
	event.Data = compact.Bytes()
	return event, false, nil
}

// newGeminiStreamError classifies local read and protocol failures as HTTP 502.
func newGeminiStreamError(kind, message string, cause error) *GeminiStreamError {
	return &GeminiStreamError{Status: http.StatusBadGateway, Kind: kind, Message: message, Cause: cause}
}

// Read records positive upstream reads before returning their bytes to framing.
func (b *geminiStreamBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.last.Store(time.Now().UnixNano())
	}
	return n, err
}

// Close releases the underlying transport once across worker and executor cleanup.
// Concurrent and repeated calls retain the same close result. The wrapped closer
// must interrupt pending reads so its owner can join the framing worker.
func (b *geminiStreamBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

// readGeminiFrames splits bounded SSE events without buffering the complete response.
// It is the only frame producer and always sends a final error, including EOF.
// The stop channel interrupts delivery; the owner closes body to interrupt reads.
//
// Framing includes ignored fields and comments in the byte limit, while partial
// lines are accumulated only up to that same bound. Data fields join with newline
// separators before JSON validation. EOF can complete a valid final event without
// an empty line; a non-EOF read failure never forwards its unfinished frame.
func readGeminiFrames(body io.Reader, limit int, frames chan<- geminiStreamFrame, stop <-chan struct{}) {
	reader := bufio.NewReaderSize(body, geminiStreamReadBuffer)
	var line, data []byte
	event := ""
	frameBytes := 0
	send := func(frame geminiStreamFrame) bool {
		select {
		case frames <- frame:
			return true
		case <-stop:
			return false
		}
	}
	emit := func() bool {
		if len(data) == 0 && event != "error" {
			return true
		}
		return send(geminiStreamFrame{event: event, data: bytes.Clone(data)})
	}
	for {
		fragment, err := reader.ReadSlice('\n')
		frameBytes += len(fragment)
		if frameBytes > limit {
			send(geminiStreamFrame{err: newGeminiStreamError("stream_frame_too_large", "Upstream SSE frame exceeds the configured limit", nil)})
			return
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			send(geminiStreamFrame{err: err})
			return
		}
		value := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		if len(value) == 0 {
			if !emit() {
				return
			}
			data = data[:0]
			event = ""
			frameBytes = 0
		} else if value[0] != ':' {
			field, content, _ := bytes.Cut(value, []byte(":"))
			content = bytes.TrimPrefix(content, []byte(" "))
			switch string(field) {
			case "data":
				if len(data) > 0 {
					data = append(data, '\n')
				}
				data = append(data, content...)
			case "event":
				event = string(content)
			}
		}
		line = line[:0]
		if errors.Is(err, io.EOF) {
			if !emit() {
				return
			}
			send(geminiStreamFrame{err: io.EOF})
			return
		}
	}
}
