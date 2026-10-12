package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

func (p *OpenAIResponseOutput) RecordMessagesStreamError(c *gin.Context, provider *gatewayprovider.ExecutionProvider, upstreamRequestID, kind, message string) {
	if c == nil {
		return
	}
	message = logredact.SanitizeUpstreamQueries(message)
	SetOpsUpstreamError(c, http.StatusBadGateway, message, "")
	event := ops.OpsUpstreamErrorEvent{
		Platform:           capability.PlatformOpenAI,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               kind,
		Message:            message,
	}
	if provider != nil {
		event.Platform = provider.Record.Platform
		event.ProviderID = provider.Record.ID
		event.ProviderName = provider.Record.Name
	}
	AppendOpsUpstreamError(c, event)
}

func (p *OpenAIResponseOutput) ReadBufferedTerminal(
	resp *http.Response,
	c *gin.Context,
	logPrefix string,
	requestID string,
) (*protocolopenai.ResponsesResponse, protocolopenai.ForwardUsage, *bridge.BufferedResponseAccumulator, error) {
	return openai.ReadCompatBufferedTerminal(resp, p.BufferedOptions(c, logPrefix, requestID))
}

func (p *OpenAIResponseOutput) BufferedOptions(c *gin.Context, logPrefix, requestID string) openai.CompatBufferedOptions {
	maxLineSize := OpenAIResponseDefaultMaxLineSize
	if p.Options.Configured && p.Options.MaxLineSize > 0 {
		maxLineSize = p.Options.MaxLineSize
	}
	return openai.CompatBufferedOptions{
		MaxLineSize: maxLineSize,
		StreamInterval: func() time.Duration {
			if p.Options.Configured && p.Options.StreamDataIntervalTimeout > 0 {
				return time.Duration(p.Options.StreamDataIntervalTimeout) * time.Second
			}
			return 0
		},
		RestoreToolNames: func(body []byte) []byte { return RestoreCodexToolNamesFromContext(c, body) },
		Observe:          func(body []byte, event string) { ObserveOpenAIServiceTierInContext(c, body, event) },
		Log: func(message string, err error, interval time.Duration) {
			if err != nil {
				logging.L().Warn(logPrefix+": "+message, zap.Error(err), zap.String("request_id", requestID))
				return
			}
			logging.L().Warn(logPrefix+": "+message, zap.String("request_id", requestID), zap.Duration("interval", interval))
		},
	}
}
