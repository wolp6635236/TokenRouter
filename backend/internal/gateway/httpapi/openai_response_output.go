package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

const OpenAIResponseDefaultMaxLineSize = 500 * 1024 * 1024

// OpenAIResponseOptions 接收应用的静态配置，TTFT 设置在请求时读取。
type OpenAIResponseOptions struct {
	ImageStreamDataIntervalTimeout, ImageStreamKeepaliveInterval               int
	Configured                                                                 bool
	MaxLineSize                                                                int
	StreamDataIntervalTimeout, StreamKeepaliveInterval                         int
	OpenAIFirstOutputTimeoutSeconds, OpenAIHighEffortFirstOutputTimeoutSeconds int
	LogUpstreamErrorBody                                                       bool
	LogUpstreamErrorBodyMaxBytes                                               int
	ResponseHeadersEnabled                                                     bool
	ReadLimit                                                                  int64
}

// OpenAIResponseOutput 组合原生响应读取器与 HTTP 输出、健康和诊断，不持有选号或结算循环。
type OpenAIResponseOutput struct {
	Reasoning    *session.ReasoningHistory
	Options      OpenAIResponseOptions
	Health       *provideradapter.OpenAIResponseHealth
	GrokHealth   *provideradapter.GrokHealth
	Observer     *provideradapter.UpstreamHealth
	Headers      *egress.CompiledHeaderFilter
	Turns        *CodexTurnStateHeaders
	Corrector    *openai.CodexToolCorrector
	ProxyCircuit *egress.ProxyStreamCircuit
	TTFT         func(context.Context) string
	Redact       func(context.Context, *gatewayadapter.ExecutionProvider, []byte) []byte
	Responses    session.OpenAIWSStateStore
	ResponseTTL  func() time.Duration
}

func (p *OpenAIResponseOutput) TTFTMode(ctx context.Context) string {
	mode := gateway.OpenAITTFTModeSemantic
	if p != nil && p.TTFT != nil {
		mode = p.TTFT(ctx)
	}
	return gateway.NormalizeOpenAITTFTMode(mode)
}

func (p *OpenAIResponseOutput) redact(ctx context.Context, target *gatewayadapter.ExecutionProvider, body []byte) []byte {
	if p == nil || p.Redact == nil {
		return body
	}
	return p.Redact(ctx, target, body)
}

// ExecutionErrorProvider 返回当前尝试的提供商诊断字段。
func ExecutionErrorProvider(value *gatewayadapter.ExecutionProvider) *UpstreamErrorProvider {
	if value == nil {
		return nil
	}
	return &UpstreamErrorProvider{ID: value.Record.ID, Name: value.Record.Name, Platform: value.Record.Platform}
}

// ReadStreamObservation 返回已读取的流结果和读取错误，由入口判断是否完成。
func (p *OpenAIResponseOutput) ReadStreamObservation(ctx context.Context, resp *http.Response, c *gin.Context, target *gatewayadapter.ExecutionProvider, started time.Time, original, mapped, effort string) (*openai.StreamingResult, error) {
	return openai.ReadStreamingResponse(ctx, resp, upstream.NewOutputContext(ResponseSink{Writer: c.Writer}), p.StreamOptions(ctx, c, target, effort), started, original, mapped, effort)
}

func (p *OpenAIResponseOutput) NonStream(ctx context.Context, resp *http.Response, c *gin.Context, target *gatewayadapter.ExecutionProvider, original, mapped string) (*openai.NonStreamingResult, error) {
	return openai.ReadNonStreamingResponse(ctx, resp, upstream.NewOutputContext(ResponseSink{Writer: c.Writer}), p.NonStreamOptions(ctx, c, target), original, mapped)
}
