package httpapi

import (
	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

// grokMediaFixture 只装配媒体场景使用的固定依赖。
func grokMediaFixture(transport httpclient.UpstreamTransport) *GrokExecutor {
	return &GrokExecutor{Credentials: testkit.RequestCredentials(nil, nil, nil, nil), Transport: transport, Output: &OpenAIResponseOutput{Options: OpenAIResponseOptions{ReadLimit: 128 * 1024 * 1024}}, Health: &provideradapter.GrokHealth{}, Routes: gatewayadapter.GrokRoutes{Validate: (egress.OperatorURLPolicy{}).Validate}, Failure: &UpstreamTransportFailure{}}
}
