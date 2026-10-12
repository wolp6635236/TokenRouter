package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/egress"
)

func TestNewOpenAIGatewayService_InitializesOpenAIWSResolver(t *testing.T) {
	options := &wsFixtureOptions{}
	fixture := newWSFixture(wsFixtureInputs{options: options})
	svc := NewOpenAIWebSocketExecutor(fixture.OpenAIWSDependencies)

	decision := svc.Selection.ResolveTransport(nil)
	require.Equal(t, egress.OpenAIUpstreamTransportHTTPSSE, decision.Transport)
	require.Equal(t, "protocol_http", decision.Reason)
}
