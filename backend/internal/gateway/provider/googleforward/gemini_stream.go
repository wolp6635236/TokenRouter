package googleforward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// gemini_stream.go applies existing provider policy to failed Gemini streams.
// A successful HTTP status does not establish a successful generation: the
// validated reader can discover an embedded provider error, an incomplete body,
// or an idle timeout. Those failures retain their status and partial accounting
// while selecting the same configured retry and error rules as ordinary HTTP
// failures. Downstream heartbeat bytes remain outside output commitment.
//
// Presentation is supplied by the selected client protocol. Failover is returned
// only while the output sink reports no non-heartbeat delivery; otherwise the
// same failure ends the existing response. Cancellation and downstream write
// errors bypass provider policy, preserving their original classification and
// avoiding a second response to a client that has already disconnected.

// streamFailure reconciles a classified stream failure with provider policy and output.
// The callback presents one final protocol error after retries are no longer
// eligible. Unclassified errors and cancellation return unchanged for their existing owner.
//
// A live sink supplies commitment without accessing mutable response headers.
// Unknown sinks conservatively prohibit retry. The synthetic response is used
// only for policy selection; it does not replace the successful transport's
// stored headers or discard observations already captured by the executor.
// The original stream error remains available through the returned error chain.
func (s *Gemini) streamFailure(ctx context.Context, c *attempt, provider *gatewayprovider.ExecutionProvider, result upstream.AttemptResult, failure error, present func(int, []byte) error) error {
	var streamError *upstream.GeminiStreamError
	if !errors.As(failure, &streamError) || ctx.Err() != nil || result.ClientDisconnect {
		return failure
	}
	body := streamError.Body
	if len(body) == 0 {
		body, _ = json.Marshal(map[string]any{"error": map[string]any{"code": streamError.Status, "message": streamError.Message}})
	}
	response := &http.Response{StatusCode: streamError.Status, Header: result.UpstreamHeaders.Clone(), Body: io.NopCloser(bytes.NewReader(body))}
	defer func() { _ = response.Body.Close() }()
	outcome := s.buildGeminiFailureOutcome(ctx, provider, response, body, result.UpstreamModel)
	committed := true
	if state, ok := c.Sink().(interface {
		// OutputState reports current delivery without taking response-header ownership.
		OutputState() upstream.OutputHead
	}); ok {
		committed = state.OutputState().ContentCommitted
	}
	if outcome.failover != nil && !committed {
		// Reuse configured retry policy only before real protocol output. A queued
		// or waiting heartbeat may already have selected downstream HTTP 200 SSE.
		c.Observe(ops.OpsUpstreamErrorEvent{
			Platform: provider.Record.Platform, ProviderID: provider.Record.ID, ProviderName: provider.Record.Name,
			UpstreamStatusCode: streamError.Status, UpstreamRequestID: result.RequestID,
			Kind: "failover", Message: logredact.SanitizeUpstreamQueries(streamError.Message),
		})
		return outcome.failover
	}
	if outcome.policy == providercore.ErrorPolicyCustomSkipped {
		presented := c.GeminiCustomCodeSkippedError(provider, streamError.Status, result.RequestID, body, func() {
			fallback, _ := json.Marshal(map[string]any{"error": map[string]any{"code": http.StatusInternalServerError, "message": geminiCustomCodeSkippedClientMessage}})
			_ = present(http.StatusInternalServerError, fallback)
		})
		return errors.Join(failure, presented)
	}
	return errors.Join(failure, present(streamError.Status, body))
}
