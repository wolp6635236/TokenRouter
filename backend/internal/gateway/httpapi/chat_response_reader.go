package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

func (p *OpenAIResponseOutput) ChatBuffered(
	resp *http.Response,
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*forwardcore.OpenAIResult, error) {
	result, err := openai.ReadChatBuffered(resp, upstream.NewDeferredOutputContext(ResponseSink{Writer: c.Writer}), p.ChatOptions(c, provider, resp, originalModel, billingModel, upstreamModel), originalModel, upstreamModel, startTime)
	return gatewayprovider.ChatForwardResult(result, billingModel), err
}

func (p *OpenAIResponseOutput) ChatStreaming(
	resp *http.Response,
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
	requestBodyLen int,
) (*forwardcore.OpenAIResult, error) {
	result, err := openai.ReadChatStreaming(resp, upstream.NewDeferredOutputContext(ResponseSink{Writer: c.Writer}), p.ChatOptions(c, provider, resp, originalModel, billingModel, upstreamModel), originalModel, upstreamModel, startTime, requestBodyLen)
	return gatewayprovider.ChatForwardResult(result, billingModel), err
}
