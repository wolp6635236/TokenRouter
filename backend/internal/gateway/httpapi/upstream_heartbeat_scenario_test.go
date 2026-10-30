package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/openaiforward"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// upstream_heartbeat_scenario_test.go covers Anthropic stream.go and
// stream_passthrough.go, OpenAI response_stream.go, response_passthrough.go and
// response_raw_chat.go, native anthropic_direct_output.go, Gemini response.go,
// and Antigravity response_stream.go through their actual HTTP output boundary.

// TestUpstreamHeartbeatReaders surrounds two content frames with upstream
// heartbeats. Every changed reader must consume the heartbeat bytes while
// preserving both content markers in their original delivery order.
func TestUpstreamHeartbeatReaders(t *testing.T) {
	anthropicBody := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"model\",\"content\":[],\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"first-content\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"second-content\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	responsesBody := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"first-content\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"second-content\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"response_test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n"
	chatBody := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first-content\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"second-content\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	geminiBody := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"first-content\"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"second-content\"}]},\"finishReason\":\"STOP\"}]}\n\n"
	for _, name := range []string{"messages", "messages passthrough", "compatible messages", "responses", "grok responses", "responses passthrough", "raw chat", "gemini", "antigravity"} {
		t.Run(name, func(t *testing.T) {
			payload := responsesBody
			switch name {
			case "messages", "messages passthrough", "compatible messages":
				payload = anthropicBody
			case "raw chat":
				payload = chatBody
			case "gemini", "antigravity":
				payload = geminiBody
			}
			heartbeat := ": upstream-only-marker\n\nevent: ping\ndata: {\"type\":\"ping\",\"marker\":\"upstream-only-marker\"}\n\ndata:\n\ndata: {\"type\":\"keepalive\",\"marker\":\"upstream-only-marker\"}\n\n"
			payload = heartbeat + strings.ReplaceAll(payload, "\n\n", "\n\n"+heartbeat)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}
			defer func() { require.NoError(t, resp.Body.Close()) }()
			out := upstream.NewOutputContext(NewResponseSink(c))
			target := &gatewayprovider.ExecutionProvider{Record: providercore.Record{Platform: capability.PlatformOpenAI}}
			fixture := newResponsesFixture(responsesFixtureInputs{})
			var err error
			switch name {
			case "messages":
				_, err = anthropic.StreamResponse(c.Request.Context(), resp, out, anthropic.StreamOptions{}, time.Now(), "model", "model", false)
			case "messages passthrough":
				_, err = anthropic.StreamResponsePassthrough(c.Request.Context(), resp, out, anthropic.StreamOptions{}, time.Now(), "model")
			case "compatible messages":
				options := openaiforward.NativeAnthropicOptions{
					UpdateWindow: func(context.Context, http.Header) {}, CopyHeaders: func(http.Header, http.Header) {},
					MaxLineSize: func() int { return upstream.DefaultSSELineLimit }, StreamInterval: func() time.Duration { return 0 },
					ReverseTools: func(p []byte) []byte { return p }, ExtractData: anthropic.ExtractSSEDataLine, IsTerminal: anthropic.StreamEventIsTerminal,
				}
				_, err = openaiforward.NativeAnthropicStreaming(c.Request.Context(), resp, out, options, "model", "model", "model", nil, time.Now())
			case "responses", "grok responses":
				if name == "grok responses" {
					target.Record.Platform = capability.PlatformGrok
				}
				_, err = fixture.Output.ReadStreamObservation(c.Request.Context(), resp, c, target, time.Now(), "model", "model", "")
			case "responses passthrough":
				_, err = openai.ReadPassthroughStreaming(c.Request.Context(), resp, out, fixture.Output.PassthroughOptions(c.Request.Context(), c, target), time.Now(), "model", "model")
			case "raw chat":
				_, err = openai.ReadRawChatStreaming(out, resp, fixture.Output.RawOptions(c, resp, target, "model", "model", nil, WriteForwardChatError), "model", "model", nil, time.Now(), 0)
			case "gemini":
				adapter := gemini.ResponseAdapter{Options: gemini.ResponseOptions{ObserveImages: func([]byte) {}}}
				_, err = adapter.HandleNativeStreamingResponse(out, resp, time.Now(), false)
			case "antigravity":
				adapter := antigravity.ResponseAdapter{}
				_, err = adapter.HandleGeminiStreamingResponse(out, resp, time.Now())
			}
			require.NoError(t, err)
			body := rec.Body.String()
			require.NotContains(t, body, "upstream-only-marker")
			require.NotContains(t, body, "event: ping")
			require.Contains(t, body, "first-content")
			require.Contains(t, body, "second-content")
			require.Less(t, strings.Index(body, "first-content"), strings.Index(body, "second-content"))
		})
	}
}
