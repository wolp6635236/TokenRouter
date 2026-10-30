package httpapi

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// output.go adapts synchronous upstream output events to the HTTP response
// writer. Begin copies supplied header slices into the destination before
// writing status, keeping those slices separate from the caller's header
// storage. Emit writes event bytes before any requested flush and returns
// write errors to the executor; flushing requires the writer to support
// http.Flusher.
//
// InitialOutput clones headers before reading status and commitment. Text
// keepalive snapshots preserve header waits during first-event staging; compact
// and image Header access stops and joins their pending heartbeat.
// Reversing those reads could pair the headers with commitment state captured
// before that handoff completed. Later OutputState calls inspect status and
// commitment through the writer's optional interfaces without accessing
// headers, so those observations do not repeat the heartbeat handoff.
// Header observers run only after Begin takes over preparation and confirms
// that metadata is still applicable. Upstream sinks bind the request's timing
// writer before emission. Local HTTP errors use the ordinary writer directly
// and cannot establish upstream delivery.

// MessagesOutput supplies synchronous HTTP output and request observations.
// Executors read identity, routing, body, and funding state through execution.Request.
type MessagesOutput struct {
	Concurrency  *ConcurrencyHelper // shared HTTP capacity helper
	ResponseSink                    // embedded synchronous output sink
	HTTP         *gin.Context       // current Gin request context
	Log          *zap.Logger        // request-scoped diagnostic logger
	// StreamStarted is shared with queue and concurrency observers. Those
	// callbacks set it when wait output starts, and callers read it when choosing
	// later stream error presentation.
	StreamStarted *bool
}

// ResponseSink writes synchronous upstream events to the client and flushes when requested.
type ResponseSink struct {
	Writer http.ResponseWriter // required downstream HTTP writer
	// ctx enables heartbeat-adjusted commitment for live HTTP requests. Literal
	// sinks without a context retain their existing HTTP commitment semantics.
	ctx *gin.Context
	// timing identifies the final writer beneath optional HTTP wrappers. Nil
	// disables source observation for requests outside HTTP text timing scope.
	timing *timingWriter
	// headersApplied observes provider metadata only when Begin can still set
	// response headers. It runs synchronously after copying, before payload emission.
	headersApplied func(http.Header)
}

// NewResponseSink binds upstream delivery to the current HTTP writer and timing.
// Callers supply a live Gin context and emit synchronously. Writer wrappers stay
// in place, so timing observes successfully delivered bytes after their rewrites.
func NewResponseSink(c *gin.Context) ResponseSink {
	value, _ := c.Get(requestTimingWriterKey)
	writer, _ := value.(*timingWriter)
	return ResponseSink{Writer: c.Writer, timing: writer, ctx: c}
}

// Begin copies response header value slices and writes the supplied status
// synchronously. The bound writer is required; its own commitment rules decide
// whether a status write takes effect, while input header slices remain
// separate from the output map.
//
// Header access closes the downstream early-commit window before checking
// whether metadata remains writable. The optional observer runs only when
// those headers were eligible for application, preventing a heartbeat that
// already committed HTTP from publishing an undelivered provider identity.
func (s ResponseSink) Begin(head upstream.OutputHead) error {
	// Header access takes ownership from the heartbeat writer before commitment
	// is sampled, so a timer cannot win between eligibility and metadata copying.
	headers := s.Writer.Header()
	observe := s.headersApplied
	if s.ctx == nil || s.ctx.Writer.Written() {
		observe = nil
	}
	for key, values := range head.Header {
		headers[key] = append([]string(nil), values...)
	}
	s.Writer.WriteHeader(head.Status)
	if observe != nil {
		observe(head.Header)
	}
	return nil
}

// Emit writes nonempty event bytes and returns a writer error before flushing.
// A short write becomes io.ErrShortWrite. Terminal events exclude timer output
// across the existing writer chain and publish completion only after successful
// delivery. A requested flush requires http.Flusher; callers retain event storage.
// The timing source scope lasts only for this emission; later local errors
// cannot inherit it, even after an upstream write or flush fails.
func (s ResponseSink) Emit(event upstream.OutputEvent) error {
	if event.Terminal && s.ctx != nil {
		if value, ok := s.ctx.Get(sseKeepaliveWriterKey); ok {
			if writer, ok := value.(*sseKeepaliveWriter); ok {
				return writer.emitTerminal(func() error { return s.emit(event) })
			}
		}
	}
	return s.emit(event)
}

// emit scopes upstream observations around checked delivery through all HTTP wrappers.
// Callers retain payload storage until return. Terminal callers already hold the
// keepalive emission lock; ordinary writer operations acquire their normal locks.
func (s ResponseSink) emit(event upstream.OutputEvent) error {
	if s.timing != nil {
		// Source publication surrounds the existing writer chain rather than
		// bypassing its transformations, capture, short-write handling or heartbeat locks.
		s.timing.upstreamWrites.Add(1)
		defer s.timing.upstreamWrites.Add(-1)
	}
	if len(event.Data) > 0 {
		n, err := s.Writer.Write(event.Data)
		if err != nil {
			return err
		}
		if n != len(event.Data) {
			return io.ErrShortWrite
		}
	}
	if event.Flush {
		if flusher, ok := s.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return nil
}

// InitialOutput clones current headers before reading writer commitment and
// status. Text snapshots preserve header waiting; other keepalive writers stop
// on Header access. Reading state afterward includes that handoff's final
// commitment; the returned header map and value slices are independent.
func (s ResponseSink) InitialOutput() upstream.OutputHead {
	var headers http.Header
	if s.ctx != nil {
		_, compact := s.ctx.Get(openAICompactSSEKeepaliveKey)
		_, images := s.ctx.Get(openAIImagesJSONKeepaliveKey)
		if value, ok := s.ctx.Get(sseKeepaliveWriterKey); ok && !compact && !images {
			if w, valid := value.(*sseKeepaliveWriter); valid {
				// Codec initialization must preserve header waiting while its first event
				// remains staged. Begin later closes that window before editing headers.
				headers = w.snapshotHeader()
			}
		}
	}
	if headers == nil {
		// Compact and image wrappers require Header to stop and join their waiting
		// controller before output takes over; retain that handoff for those formats.
		headers = s.Writer.Header().Clone()
	}
	h := s.OutputState()
	h.Header = headers
	return h
}

// OutputState reads optional Written and Status interfaces from the bound
// writer. Missing interfaces leave zero state fields, and this operation avoids
// Header access so a waiting keepalive can continue until an output handoff is
// requested. Content commitment excludes all recorded heartbeats when the sink
// has a request context; literal sinks retain their HTTP commitment behavior.
func (s ResponseSink) OutputState() upstream.OutputHead {
	h := upstream.OutputHead{}
	if w, ok := s.Writer.(interface {
		// Written reports whether this response writer has committed HTTP output.
		Written() bool
	}); ok {
		h.Committed = w.Written()
	}
	if w, ok := s.Writer.(interface {
		// Status returns the HTTP status currently retained by this writer.
		Status() int
	}); ok {
		h.Status = w.Status()
	}
	h.ContentCommitted = h.Committed
	if s.ctx != nil {
		h.ContentCommitted = h.Committed && HeartbeatAdjustedWrittenSize(s.ctx) > 0
	}
	return h
}
