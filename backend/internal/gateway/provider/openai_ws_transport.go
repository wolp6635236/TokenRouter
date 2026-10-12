package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// ResolveOpenAIWSTransport 根据上游协议集合选择 Responses 传输。
func ResolveOpenAIWSTransport(value *provider.Record) egress.OpenAIWSProtocolDecision {
	if value != nil && value.SupportsResponsesWS() {
		return egress.OpenAIWSProtocolDecision{Transport: egress.OpenAIUpstreamTransportResponsesWebsocketV2, Reason: "native_protocol"}
	}
	return egress.OpenAIWSHTTPDecision("protocol_http")
}
