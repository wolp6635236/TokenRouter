package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/googleforward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
)

// GeminiExecutor 为每个 HTTP 请求创建同步输出，各协议入口共用 Gemini 请求准备器。
type GeminiExecutor struct{ Runtime *googleforward.Gemini }

// AntigravityExecutor 提供 Antigravity 协议入口并处理 HTTP 输出和错误。
type AntigravityExecutor struct{ Runtime *googleforward.Antigravity }

func (s *GeminiExecutor) Forward(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, body []byte) (*forward.MessagesResult, error) {
	return s.Runtime.Forward(ctx, NewGoogleBoundary(c, s.Runtime.Options, false), a, body)
}

func (s *GeminiExecutor) ForwardNative(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, model, action string, stream bool, body []byte) (*forward.MessagesResult, error) {
	return s.Runtime.ForwardNative(ctx, NewGoogleBoundary(c, s.Runtime.Options, false), a, model, action, stream, body)
}

func (s *GeminiExecutor) ForwardAsResponses(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, body []byte, parsed *requeststate.ParsedRequest) (*forward.MessagesResult, error) {
	return s.Runtime.ForwardAsResponses(ctx, NewGoogleBoundary(c, s.Runtime.Options, false), a, body, parsed)
}

func (s *GeminiExecutor) ForwardAsChatCompletions(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, body []byte) (*forward.MessagesResult, error) {
	return s.Runtime.ForwardAsChatCompletions(ctx, NewGoogleBoundary(c, s.Runtime.Options, false), a, body)
}

func (s *AntigravityExecutor) Forward(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, body []byte, sticky bool) (*forward.MessagesResult, error) {
	return s.Runtime.Forward(ctx, NewGoogleBoundary(c, s.Runtime.Options, true), a, body, sticky)
}

func (s *AntigravityExecutor) ForwardGemini(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, model, action string, stream bool, body []byte, sticky bool, options ...forward.GeminiSessionOption) (*forward.MessagesResult, error) {
	return s.Runtime.ForwardGemini(ctx, NewGoogleBoundary(c, s.Runtime.Options, true), a, model, action, stream, body, sticky, options...)
}

func (s *AntigravityExecutor) ForwardAsResponses(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, body []byte, parsed *requeststate.ParsedRequest) (*forward.MessagesResult, error) {
	return s.Runtime.ForwardAsResponses(ctx, NewGoogleBoundary(c, s.Runtime.Options, true), a, body, parsed)
}

func (s *AntigravityExecutor) ForwardAsChatCompletions(ctx context.Context, c *gin.Context, a *gatewayadapter.ExecutionProvider, body []byte, parsed *requeststate.ParsedRequest) (*forward.MessagesResult, error) {
	return s.Runtime.ForwardAsChatCompletions(ctx, NewGoogleBoundary(c, s.Runtime.Options, true), a, body, parsed)
}

func (s *AntigravityExecutor) WriteMappedClaudeError(c *gin.Context, a *gatewayadapter.ExecutionProvider, status int, id string, body []byte) error {
	return NewGoogleBoundary(c, s.Runtime.Options, true).MappedClaudeError(a, status, id, body)
}
