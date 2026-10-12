package egress

const (
	OpenAIUpstreamTransportAny                  OpenAIUpstreamTransport = ""
	OpenAIUpstreamTransportHTTPSSE              OpenAIUpstreamTransport = "http_sse"
	OpenAIUpstreamTransportResponsesWebsocketV2 OpenAIUpstreamTransport = "responses_websockets_v2"
	// OpenAIUpstreamTransportResponsesWebsocketV2Ingress 仅用于 WS ingress 选号，允许 ctx_pool/passthrough/http_bridge。
	OpenAIUpstreamTransportResponsesWebsocketV2Ingress OpenAIUpstreamTransport = "responses_websockets_v2_ingress"
)

// OpenAIUpstreamTransport 表示 OpenAI 上游传输协议。
type OpenAIUpstreamTransport string

// OpenAIWSProtocolDecision 表示协议决策结果。
type OpenAIWSProtocolDecision struct {
	Transport OpenAIUpstreamTransport
	Reason    string
}

// OpenAIWSHTTPDecision 保留回退原因与 HTTP/SSE 传输值。
func OpenAIWSHTTPDecision(reason string) OpenAIWSProtocolDecision {
	return OpenAIWSProtocolDecision{
		Transport: OpenAIUpstreamTransportHTTPSSE,
		Reason:    reason,
	}
}
