package openaiattempt

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	textflow "github.com/TokenFlux/TokenRouter/internal/gateway/text"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// heartbeatRetryPorts runs real retry decisions inside the ordinary provider loop.
type heartbeatRetryPorts struct {
	textflow.ResponsePorts              // real protocol retry adapter
	t                      *testing.T   // active assertion scope
	c                      *gin.Context // shared client response
	attempts               int          // selected provider count
	completedSwitches      int          // successful switch count
}

// heartbeat_retry_scenario_test.go covers openai_chat_attempts.go and
// openai_messages_attempts.go with queue commitment before a provider failure.

// TestTextAttemptsRetryAfterQueueHeartbeat starts both attempt bridges after a
// queue heartbeat commits HTTP 200. More heartbeat bytes preserve replay, while
// the first business bytes prevent the next provider from extending that stream.
func TestTextAttemptsRetryAfterQueueHeartbeat(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Header("Content-Type", "text/event-stream")
			n, err := c.Writer.Write([]byte(": queued\n\n"))
			require.NoError(t, err)
			gatewayhttp.RecordStreamHeartbeat(c, n)
			c.Writer.Flush()
			require.True(t, c.Writer.Written())
			base := responsesAttemptBridge{
				c: c, reqLog: zap.NewNop(), provider: &gatewayprovider.ExecutionProvider{},
				writerSizeBeforeForward: gatewayhttp.HeartbeatAdjustedWrittenSize(c),
				fixed: &openAIExecutionDependencies{
					recordOpenAICyberWarning: func(*gin.Context, *zap.Logger, *apikey.APIKey, *gatewayprovider.ExecutionProvider, string, int, []byte, string) {
					},
					reportOpenAIProviderScheduleResult: func(*gatewayprovider.ExecutionProvider, string, bool, *int, ...error) bool { return true },
				},
			}
			n, err = c.Writer.Write([]byte(": queued again\n\n"))
			require.NoError(t, err)
			gatewayhttp.RecordStreamHeartbeat(c, n)
			ports := &heartbeatRetryPorts{t: t, c: c}
			if protocol == "chat" {
				ports.ResponsePorts = &openAIChatAttemptBridge{responsesAttemptBridge: base}
			} else {
				ports.ResponsePorts = &openAIMessageAttemptBridge{responsesAttemptBridge: base}
			}
			textflow.RunResponses(textflow.ResponseOptions{MaxSwitches: 1}, ports)
			require.Equal(t, 2, ports.attempts)
			require.Equal(t, 1, ports.completedSwitches)
			require.Contains(t, rec.Body.String(), "data: second provider")
			require.NotEqual(t, base.writerSizeBeforeForward, gatewayhttp.HeartbeatAdjustedWrittenSize(c))
		})
	}
}

// CanAttempt permits both local providers in this connected request fixture.
func (p *heartbeatRetryPorts) CanAttempt() bool { return true }

// Select returns a distinct provider and checks the failed target was excluded.
// The fixture accepts exactly two attempts, preventing an accidental retry loop
// from silently selecting additional providers or hanging the test.
func (p *heartbeatRetryPorts) Select(excluded map[int64]struct{}) (textflow.ResponseSelection, error) {
	p.attempts++
	require.LessOrEqual(p.t, p.attempts, 2)
	if p.attempts == 2 {
		require.Contains(p.t, excluded, int64(1))
	}
	return textflow.ResponseSelection{Available: true, Selection: textflow.Selection{Provider: provider.ProviderSnapshot{ID: int64(p.attempts)}}}, nil
}

// Acquire supplies immediate capacity for the selected local provider fixture.
func (p *heartbeatRetryPorts) Acquire() bool { return true }

// Forward fails the first provider after a heartbeat and completes the second.
// Both attempts share one HTTP response, reproducing the output boundary that
// previously prevented provider switching after queue heartbeats committed headers.
func (p *heartbeatRetryPorts) Forward() textflow.ResponseOutcome {
	if p.attempts == 1 {
		n, err := p.c.Writer.Write([]byte(": waiting for first provider\n\n"))
		require.NoError(p.t, err)
		gatewayhttp.RecordStreamHeartbeat(p.c, n)
		failure := &forwardcore.UpstreamFailoverError{StatusCode: http.StatusBadGateway}
		return textflow.ResponseOutcome{Outcome: textflow.Outcome{Err: failure, Failure: &textflow.AttemptFailure{Cause: failure, Policy: failure.RetryFailure()}}}
	}
	_, err := p.c.Writer.Write([]byte("data: second provider\n\n"))
	require.NoError(p.t, err)
	return textflow.ResponseOutcome{}
}

// Switched leaves scheduling feedback outside this output-boundary test fixture.
func (p *heartbeatRetryPorts) Switched() {}

// Switching leaves provider transition logging outside this output-boundary fixture.
func (p *heartbeatRetryPorts) Switching(*textflow.AttemptFailure, int, int) {}

// Success accepts the healthy second provider without recording production feedback.
func (p *heartbeatRetryPorts) Success() {}

// Complete leaves billing outside this test of response retry eligibility.
func (p *heartbeatRetryPorts) Complete() {}

// Completed retains the switch count observed by the successful outer loop.
func (p *heartbeatRetryPorts) Completed(switches int) { p.completedSwitches = switches }
