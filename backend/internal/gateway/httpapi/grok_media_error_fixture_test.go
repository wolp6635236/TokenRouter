package httpapi

import (
	"context"
	"net/http"

	forward "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	provider "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/gin-gonic/gin"
)

// mediaErrorResponseFixture 通过生产媒体入口处理给定响应，仍使用场景原健康实例。
func mediaErrorResponseFixture(executor *GrokExecutor, ctx context.Context, resp *http.Response, c *gin.Context, target *gatewayadapter.ExecutionProvider, requestID, model string) (*forward.OpenAIResult, error) {
	local := *executor
	local.Transport = &auxiliaryHTTPRecorder{resp: resp}
	local.Credentials = testkit.RequestCredentials(nil, &provider.OpenAIExecutionCredentials{Grok: func(context.Context, *provider.Record) (string, error) { return "fixture-token", nil }}, nil, nil)
	local.Credentials.HasGrokTokenSource = true
	if target.Record.Credentials == nil {
		target.Record.Credentials = map[string]any{}
	}
	target.Record.Credentials["api_key"] = "fixture-token"
	resp.Header.Set("x-request-id", requestID)
	return local.ForwardGrokMedia(ctx, c, target, grok.GrokMediaEndpointImagesGenerations, "", []byte(`{"model":"`+model+`","prompt":"fixture"}`), "application/json")
}
