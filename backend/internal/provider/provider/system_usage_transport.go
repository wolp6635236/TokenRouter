package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// system_usage_transport.go observes individual internal model exchanges
// without changing the test event stream or retaining request content. Only
// requests carrying trusted internal attribution and targeting a model endpoint
// enter this transport's accounting path. Token refresh, model discovery, and
// ordinary gateway traffic pass through without producing system usage.
//
// Each dispatched request gets a fresh identity and captures its provider
// multiplier before execution. Reading tees response bytes into a bounded
// protocol observer. Closing collects trailing usage under a short deadline,
// closes the original body, and submits the observation once. Cancellation
// retains partial observations, while provider limits are checked before any
// automatic model exchange. Manual diagnostics deliberately bypass that check.

// These limits bound transient parsing and trailing usage collection without
// storing prompts, credentials, response text, or generated media in usage rows.
const (
	systemUsageFrameLimit  = 16 << 20        // maximum transient response frame bytes
	systemUsageDrainBudget = 2 * time.Second // trailing response reading budget
)

// SystemUsageTransport decorates the shared transport for internal model calls.
// Production binds one instance to tests and active quota probes only.
type SystemUsageTransport struct {
	// Background admits accounting into the application lifecycle. Nil or rejected
	// admission writes synchronously, ensuring shutdown cannot silently discard work.
	Background func(func()) bool

	Base     httpclient.UpstreamTransport                           // shared upstream request transport
	Read     func(context.Context, int64) (*provider.Record, error) // fresh provider snapshot reader
	Recorder *usage.SystemRecorder                                  // shared provider-only accounting service
}

// systemUsageBody streams bytes to the caller while observing usage snapshots.
// Reading follows net/http's single-reader rule; Close is idempotent and occurs
// after the caller has stopped reading, as required by the test executors.
type systemUsageBody struct {
	body         io.ReadCloser         // original transport response body
	ctx          context.Context       // upstream execution lifetime
	cancel       context.CancelFunc    // releases derived request resources
	row          *usage.UsageLog       // exclusively owned immutable-at-completion observation
	providerType string                // captured provider credential type
	transport    *SystemUsageTransport // atomic accounting service
	observer     systemUsageObserver   // bounded protocol observation state
	started      time.Time             // upstream dispatch observation time
	once         sync.Once             // serializes completion and persistence
	closeErr     error                 // first close or recording error
}

// Do executes a plain request with internal accounting when attribution exists.
// The caller closes successful response bodies after reading, just as with Base.
// Proxy and concurrency settings retain their original transport meanings.
func (t *SystemUsageTransport) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return t.exchange(req, id, func(request *http.Request) (*http.Response, error) {
		return t.Base.Do(request, proxy, id, concurrency)
	})
}

// DoWithTLS retains fingerprint selection while recording scoped model exchanges.
// A nil TLS profile keeps the underlying transport's ordinary request behavior.
// The caller still owns closure of every returned response body.
func (t *SystemUsageTransport) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return t.exchange(req, id, func(request *http.Request) (*http.Response, error) {
		return t.Base.DoWithTLS(request, proxy, id, concurrency, profile)
	})
}

// exchange captures attribution and wraps one dispatched model response.
// Requests outside internal model scope pass through untouched. A fresh provider
// read or local limit rejection returns before dispatch and creates no expense.
//
// The provider snapshot supplies cost identity even when settings change while
// the model runs. Network failures after dispatch retain an unknown-cost row;
// successful transport returns transfer observation ownership to the response
// body. Closing that body releases its child context after the remaining usage
// has been observed, independently of whether the preview sink succeeded.
func (t *SystemUsageTransport) exchange(req *http.Request, id int64, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	scope, scoped := usage.SystemRequestFromContext(req.Context())
	if !scoped || !systemModelEndpoint(req) || t.Recorder == nil {
		return send(req)
	}
	value, err := t.Read(req.Context(), id)
	if err != nil {
		return nil, err
	}
	if scope.Automatic && value.IsAPIKeyOrBedrock() && value.IsQuotaExceeded() {
		return nil, errors.New("automatic model request skipped: provider spending limit reached")
	}
	ctx, cancel := context.WithCancel(req.Context())
	request := req.Clone(ctx)
	model, stream, size, tier, effort, imageModel := systemRequestDimensions(req, scope.RequestedModel)
	started := time.Now()
	multiplier := value.BillingRateMultiplier()
	endpoint := req.URL.Path
	row := &usage.UsageLog{
		RequestID: uuid.NewString(), RequestSource: scope.Source, ProviderID: id, Platform: value.Platform, Model: model, RequestedModel: strings.TrimSpace(scope.RequestedModel), UpstreamModel: &model, Stream: stream, CreatedAt: started, ProviderRateMultiplier: &multiplier, UpstreamEndpoint: &endpoint,
		SystemUsage: &usage.SystemUsage{MappedModel: strings.TrimSpace(scope.MappedModel), PricingModel: imageModel, ParentRequestID: scope.ParentRequestID, OperatorUserID: scope.OperatorUserID, Outcome: "failed", UsageState: "missing", CostState: "unknown"},
	}
	if row.RequestedModel == "" {
		row.RequestedModel = model
	}
	row.Model = row.RequestedModel
	if imageModel == "" {
		// Hosted images have a separate carrier model, not a model-mapping step.
		// Ordinary tests retain each distinct routing identity for filters and audit.
		chain := make([]string, 0, 3)
		for _, name := range []string{row.RequestedModel, row.SystemUsage.MappedModel, model} {
			if name != "" && (len(chain) == 0 || chain[len(chain)-1] != name) {
				chain = append(chain, name)
			}
		}
		if len(chain) > 1 {
			value := strings.Join(chain, " → ")
			row.ModelMappingChain = &value
		}
	}
	if scope.GroupID > 0 {
		row.GroupID = &scope.GroupID
	}
	if size != "" {
		row.ImageSize = &size
	}
	if tier != "" {
		row.ServiceTier = &tier
	}
	if effort != "" {
		row.ReasoningEffort = &effort
	}
	response, err := send(request)
	if err != nil {
		if ctx.Err() != nil {
			row.SystemUsage.Outcome = "canceled"
		}
		cancel()
		duration := int(time.Since(started).Milliseconds())
		row.DurationMs = &duration
		_ = t.record(req.Context(), row, value.Type)
		return response, err
	}
	row.SystemUsage.HTTPStatus = response.StatusCode
	requestIDHeader := provider.UpstreamRequestIDHeaderName(value)
	if requestIDHeader == "" {
		requestIDHeader = "X-Request-ID"
	}
	// Use the provider-selected header and the usage field's existing byte cap;
	// a missing configured header must not borrow a different upstream identity.
	if requestID := logredact.TruncateUTF8Value(response.Header.Get(requestIDHeader), 128); requestID != "" {
		row.UpstreamRequestID = &requestID
	}
	body := &systemUsageBody{body: response.Body, ctx: ctx, cancel: cancel, row: row, providerType: value.Type, transport: t, started: started}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	body.observer.sse = stream
	// Streaming requests can fail with ordinary JSON carrying real usage.
	// The response media type takes precedence over the requested stream flag.
	if strings.Contains(contentType, "event-stream") {
		body.observer.sse = true
	} else if strings.Contains(contentType, "json") {
		body.observer.sse = false
	}
	body.observer.platform = value.Platform
	// Cache fields also occur in OpenAI-compatible replies. Request endpoints
	// identify Messages semantics even when a JSON error omits its message type.
	body.observer.messages = strings.Contains(endpoint, "/messages") ||
		strings.Contains(endpoint, "rawPredict") ||
		strings.Contains(endpoint, "RawPredict") ||
		strings.Contains(endpoint, "/invoke") ||
		req.Header.Get("anthropic-version") != ""
	body.observer.image = strings.Contains(endpoint, "/images/") || imageModel != ""
	response.Body = body
	return response, nil
}

// Read forwards the original bytes and separately observes accounting evidence.
// Transient observer buffers are bounded; a parser failure never changes the
// preview bytes or replaces the original transport error returned to the caller.
func (b *systemUsageBody) Read(dst []byte) (int, error) {
	n, err := b.body.Read(dst)
	if n > 0 {
		b.observer.consume(dst[:n])
	}
	if err != nil {
		b.observer.finish(err)
	}
	return n, err
}

// Close collects trailing accounting evidence and submits this attempt once.
// Call after stopping reads; repeated Close returns the first completion error.
// Already canceled exchanges skip draining and retain only observed quantities.
//
// Preview parsers may stop on finish_reason before a final usage-only frame.
// A short cancellation timer bounds that trailing read without launching a
// second model request. Persistence has its own bounded cancellation-independent
// budget, so a client disconnect cannot erase counters already returned upstream.
func (b *systemUsageBody) Close() error {
	b.once.Do(func() {
		defer b.cancel()
		completeUsage := b.observer.terminal && b.observer.seenInput && b.observer.seenOutput
		if !b.observer.ended && b.ctx.Err() == nil && !completeUsage {
			// The request context cancels a blocked network read after the drain
			// budget; the byte limit also bounds peers that keep sending data.
			timer := time.AfterFunc(systemUsageDrainBudget, b.cancel)
			buffer := make([]byte, 32*1024)
			remaining := systemUsageFrameLimit
			for remaining > 0 {
				n, err := b.Read(buffer[:min(len(buffer), remaining)])
				remaining -= n
				if n == 0 && err == nil {
					b.observer.truncated = true
					break
				}
				if err != nil || (b.observer.terminal && b.observer.seenInput && b.observer.seenOutput) {
					break
				}
			}
			timer.Stop()
		}
		b.closeErr = b.body.Close()
		b.observer.apply(b.row, b.ctx.Err())
		duration := int(time.Since(b.started).Milliseconds())
		b.row.DurationMs = &duration
		b.closeErr = errors.Join(b.closeErr, b.transport.record(b.ctx, b.row, b.providerType))
	})
	return b.closeErr
}

// systemModelEndpoint restricts recording to supported inference endpoint shapes.
// Scope alone cannot turn a credential refresh or model-list request into usage.
// Only POST model paths qualify; token counting is explicitly excluded.
func systemModelEndpoint(req *http.Request) bool {
	if req.Method != http.MethodPost || strings.Contains(req.URL.Path, "count_tokens") {
		return false
	}
	path := req.URL.Path
	for _, endpoint := range []string{"responses", "chat/completions", "messages", "generateContent", "streamGenerateContent", "streamRawPredict", "rawPredict", "/invoke", "/images/", "system-one", "system_one", "systemone"} {
		if strings.Contains(path, endpoint) {
			return true
		}
	}
	return req.Header.Get("x-model-key") != ""
}

// systemRequestDimensions reads only routing and pricing scalars from a copy
// of the outbound body. Request content is discarded immediately, and a missing
// model uses the explicit run identity or the provider model URL. Results are
// the outbound model, streaming flag, image size, service tier, reasoning
// effort, and optional hosted-image model, in that order.
func systemRequestDimensions(req *http.Request, fallback string) (string, bool, string, string, string, string) {
	var raw []byte
	if req.GetBody != nil {
		if body, err := req.GetBody(); err == nil {
			raw, _ = io.ReadAll(io.LimitReader(body, systemUsageFrameLimit))
			_ = body.Close()
		}
	}
	model := gjson.GetBytes(raw, "model").String()
	if model == "" {
		model = req.Header.Get("x-model-key")
	}
	if model == "" {
		if _, after, ok := strings.Cut(req.URL.EscapedPath(), "/model/"); ok {
			model, _ = url.PathUnescape(strings.Split(after, "/")[0])
		}
	}
	if model == "" {
		if _, after, ok := strings.Cut(req.URL.EscapedPath(), "/models/"); ok {
			model, _ = url.PathUnescape(strings.Split(after, ":")[0])
		}
	}
	if model == "" {
		model = fallback
	}
	if model == "" {
		model = "unknown"
	}
	effort := gjson.GetBytes(raw, "reasoning.effort").String()
	if effort == "" {
		effort = gjson.GetBytes(raw, "reasoning_effort").String()
	}
	return model, gjson.GetBytes(raw, "stream").Bool() || strings.Contains(req.URL.Path, "streamGenerateContent"), gjson.GetBytes(raw, "size").String(), gjson.GetBytes(raw, "service_tier").String(), effort, gjson.GetBytes(raw, `tools.#(type=="image_generation").model`).String()
}

// systemDataPayload removes an SSE data prefix without retaining content.
// Empty lines and other event fields are ignored by the usage observer.
// Returned payload bytes alias the supplied line and are consumed synchronously
// before that frame buffer is reused.
func systemDataPayload(line []byte) []byte {
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("data:")) {
		return nil
	}
	return bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
}

// record submits an immutable observation without extending a probe's health
// measurement. The application waits for admitted callbacks before closing storage;
// a missing or stopped task owner falls back to a bounded synchronous write.
//
// Cancellation is detached before submission, so closing the response and its
// request context cannot erase already observed work. The writer reports errors
// through sanitized diagnostics and never retries inference. Only rejected task
// admission selects synchronous execution; an accepted task owns the row until
// its pricing and idempotent persistence attempt has finished.
func (t *SystemUsageTransport) record(ctx context.Context, row *usage.UsageLog, providerType string) error {
	writeCtx := context.WithoutCancel(ctx)
	write := func() error {
		err := t.Recorder.Record(writeCtx, row, providerType)
		if err != nil {
			slog.ErrorContext(writeCtx, "system usage recording failed", "request_id", row.RequestID, "parent_request_id", row.SystemUsage.ParentRequestID, "provider_id", row.ProviderID, "request_source", row.RequestSource, "error", err)
		}
		return err
	}
	if t.Background != nil && t.Background(func() { _ = write() }) {
		return nil
	}
	return write()
}
