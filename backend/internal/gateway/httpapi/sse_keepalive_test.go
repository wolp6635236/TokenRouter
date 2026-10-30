package httpapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/messageforward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/timing"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// keepaliveTestTransport supplies deterministic HTTP header waits inside synctest.
type keepaliveTestTransport func(*http.Request) (*http.Response, error)

// keepaliveDelayedBody delays its first read while preserving the fixture payload.
type keepaliveDelayedBody struct {
	io.Reader         // remaining upstream payload bytes
	beforeRead func() // first-read wait assertion
}

// RoundTrip runs the fixture's synchronous transport without opening a network connection.
func (f keepaliveTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestDownstreamSSEKeepaliveFraming exercises complete frames, split boundaries,
// terminal events and client protocols. Idle beats must use the selected wire
// format and never enter an incomplete or already terminated SSE event stream.
func TestDownstreamSSEKeepaliveFraming(t *testing.T) {
	for _, tc := range []struct {
		name     string              // framing scenario label
		protocol protocol.ProtocolID // client wire protocol
		pieces   []string            // separately delivered frame fragments
		want     string              // expected idle heartbeat suffix
	}{
		{"responses", protocol.ProtocolOpenAIResponses, []string{"data: {}\n\n"}, ": keepalive\n\n"},
		{"chat", protocol.ProtocolOpenAIChatCompletions, []string{"data: {}\n\n"}, ": keepalive\n\n"},
		{"gemini", protocol.ProtocolGeminiGenerateContent, []string{"data: {}\n\n"}, "\n\n"},
		{"qoder", protocol.ProtocolQoderChat, []string{"data: {}\n\n"}, ": keepalive\n\n"},
		{"messages", protocol.ProtocolAnthropicMessages, []string{"data: {}\n\n"}, "event: ping\ndata: {\"type\": \"ping\"}\n\n"},
		{"compact excluded", protocol.ProtocolResponsesCompact, []string{"data: {}\n\n"}, ""},
		{"partial", protocol.ProtocolOpenAIResponses, []string{"data: {"}, ""},
		{"mixed LF and CRLF", protocol.ProtocolOpenAIResponses, []string{"data: {}\n", "\r", "\n"}, ": keepalive\n\n"},
		{"split CRLF", protocol.ProtocolOpenAIResponses, []string{"data: {}\r", "\n\r", "\n"}, ": keepalive\n\n"},
		{"done", protocol.ProtocolOpenAIChatCompletions, []string{"data: [DONE]\n\n"}, ""},
		{"message stop", protocol.ProtocolAnthropicMessages, []string{"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"}, ""},
		{"failed", protocol.ProtocolOpenAIResponses, []string{"event: response.failed\ndata: {}\n\n"}, ""},
		{"truncated terminal", protocol.ProtocolOpenAIResponses, []string{"event: response.completed\ndata: {\"text\":\"" + strings.Repeat("x", 5000) + "\"}\n\n"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runSSEKeepaliveTest(t, tc.protocol, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
				c.Header("Content-Type", "text/event-stream")
				for _, piece := range tc.pieces {
					_, err := c.Writer.WriteString(piece)
					require.NoError(t, err)
				}
				time.Sleep(25 * time.Millisecond)
				synctest.Wait()
				suffix := strings.TrimPrefix(readKeepaliveBody(t, c, rec), strings.Join(tc.pieces, ""))
				if tc.want == "" {
					require.Empty(t, suffix)
				} else {
					require.Equal(t, strings.Repeat(tc.want, 2), suffix)
					require.Equal(t, len(suffix), StreamHeartbeatBytes(c))
				}
			})
		})
	}
}

// TestDownstreamSSEKeepaliveClaudeToolDelta keeps an open Claude Code tool block
// idle and then closes it. The worker must emit empty tool-input deltas only
// while that block remains open and return to ping after its stop event.
func TestDownstreamSSEKeepaliveClaudeToolDelta(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolAnthropicMessages, "claude-cli/2.1.193 (external, cli)", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":3,\"content_block\":{\"type\":\"tool_use\"}}\n\n")
		require.NoError(t, err)
		time.Sleep(15 * time.Millisecond)
		synctest.Wait()
		require.Contains(t, readKeepaliveBody(t, c, rec), `"type":"input_json_delta","partial_json":""`)
		require.Contains(t, readKeepaliveBody(t, c, rec), `"index":3`)
		_, err = c.Writer.WriteString("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":3}\n\n")
		require.NoError(t, err)
		before := len(readKeepaliveBody(t, c, rec))
		time.Sleep(15 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, "event: ping\ndata: {\"type\": \"ping\"}\n\n", readKeepaliveBody(t, c, rec)[before:])
	})
}

// TestDownstreamSSEKeepaliveHeaderWait exercises dispatch before dependency timing
// is enabled. Streaming waits may commit SSE, while nonstream requests, explicit
// error statuses, handler header access and non-SSE metadata must remain uncommitted.
func TestDownstreamSSEKeepaliveHeaderWait(t *testing.T) {
	for _, mode := range []string{"stream", "buffered", "status", "header access", "json"} {
		t.Run(mode, func(t *testing.T) {
			runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", mode != "buffered", 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
				if mode == "status" {
					c.Writer.WriteHeader(http.StatusTooManyRequests)
				}
				if mode == "json" {
					c.Header("Content-Type", "application/json")
				}
				client := &http.Client{Transport: keepaliveTestTransport(func(*http.Request) (*http.Response, error) {
					if mode == "header access" {
						c.Writer.Header().Set("X-Handler", "owns headers")
					}
					time.Sleep(25 * time.Millisecond)
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
				})}
				req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.test", nil)
				require.NoError(t, err)
				resp, err := timing.Do(client, req)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				synctest.Wait()
				if mode != "stream" {
					require.False(t, c.Writer.Written())
					require.Empty(t, readKeepaliveBody(t, c, rec))
					return
				}
				require.True(t, c.Writer.Written())
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
				require.Equal(t, strings.Repeat(": keepalive\n\n", 2), readKeepaliveBody(t, c, rec))
				require.Equal(t, -1, HeartbeatAdjustedWrittenSize(c))
			})
		})
	}
}

// TestDownstreamSSEKeepaliveStallLimit holds a committed body beyond its inactivity
// budget. Only beats before the cutoff may reach the client; observing another
// upstream byte renews activity and permits downstream heartbeats again.
func TestDownstreamSSEKeepaliveStallLimit(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", true, 25*time.Millisecond, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		w, ok := c.Writer.(*sseKeepaliveWriter)
		require.True(t, ok)
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.Write([]byte("data: {}\n\n"))
		require.NoError(t, err)
		time.Sleep(80 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, "data: {}\n\n"+strings.Repeat(": keepalive\n\n", 2), readKeepaliveBody(t, c, rec))
		w.stages.ObserveUpstreamActivity(time.Now())
		time.Sleep(15 * time.Millisecond)
		synctest.Wait()
		require.Greater(t, StreamHeartbeatBytes(c), 2*len(": keepalive\n\n"))
	})
}

// TestDownstreamSSEKeepaliveInitialOutputPreservesWait initializes both eager and
// deferred codec output after dispatch. Read-only header snapshots must preserve
// waiting heartbeats until the codec begins actual output or handler metadata.
func TestDownstreamSSEKeepaliveInitialOutputPreservesWait(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
			w, ok := c.Writer.(*sseKeepaliveWriter)
			require.True(t, ok)
			w.openHeaderWait(c.Request.Context())
			var out *upstream.OutputContext
			sink := &gateway.OutputTracker{Sink: NewResponseSink(c)}
			if deferred {
				out = upstream.NewDeferredOutputContext(sink)
			} else {
				out = upstream.NewOutputContext(sink)
			}
			out.Writer.Header().Set("X-Upstream", "pending")
			time.Sleep(25 * time.Millisecond)
			synctest.Wait()
			require.Equal(t, strings.Repeat(": keepalive\n\n", 2), readKeepaliveBody(t, c, rec))
			require.True(t, out.Writer.Written())
			require.False(t, out.Writer.ContentWritten())
			_, err := out.Writer.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
			require.NoError(t, err)
			require.Empty(t, rec.Header().Get("X-Upstream"))
		})
	}
}

// TestDownstreamSSEKeepaliveConcurrentHeaders exercises late handler header edits
// while timer writes remain possible. Returned header copies must prevent data
// races without changing the committed wire headers or interleaving partial frames.
func TestDownstreamSSEKeepaliveConcurrentHeaders(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolOpenAIChatCompletions, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		w, ok := c.Writer.(*sseKeepaliveWriter)
		require.True(t, ok)
		w.openHeaderWait(c.Request.Context())
		time.Sleep(15 * time.Millisecond)
		for range 100 {
			c.Writer.Header().Set("X-Late", "ignored")
			_, err := c.Writer.Write([]byte("data: {}\n\n"))
			require.NoError(t, err)
			time.Sleep(11 * time.Millisecond)
		}
		synctest.Wait()
		require.Empty(t, rec.Header().Get("X-Late"))
		require.Positive(t, StreamHeartbeatBytes(c))
	})
}

// TestDownstreamSSEKeepaliveSkipsUntimedRequests confirms non-text requests never
// acquire a writer or dispatch listener. The route remains ordinary HTTP even
// when the caller marks a protocol on its request context.
func TestDownstreamSSEKeepaliveSkipsUntimedRequests(t *testing.T) {
	for _, path := range []string{"/media", "/v1/responses/compact", "/responses/compact", "/backend-api/codex/responses/compact"} {
		engine := gin.New()
		engine.POST(path, TextRequestTiming(), DownstreamSSEKeepalive(time.Millisecond, 0), func(c *gin.Context) {
			_, installed := c.Get(sseKeepaliveWriterKey)
			require.False(t, installed)
			c.String(http.StatusOK, "plain")
		})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil))
		require.Equal(t, "plain", rec.Body.String())
	}
}

// readKeepaliveBody copies recorder output under the production writer mutex.
// Tests use it while the handler is active so future timer writes have the
// same synchronization boundary as the recorder read being asserted.
func readKeepaliveBody(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) string {
	t.Helper()
	value, _ := c.Get(sseKeepaliveWriterKey)
	w, ok := value.(*sseKeepaliveWriter)
	require.True(t, ok)
	w.mu.Lock()
	defer w.mu.Unlock()
	return rec.Body.String()
}

// Read invokes the first-read hook once before consuming any upstream payload.
func (r *keepaliveDelayedBody) Read(p []byte) (int, error) {
	if r.beforeRead != nil {
		r.beforeRead()
		r.beforeRead = nil
	}
	return r.Reader.Read(p)
}

// TestDownstreamSSEKeepaliveThinkingWait returns headers immediately but delays
// the first upstream event. Staged Responses, Grok and Anthropic conversion
// must deliver downstream heartbeats before any provider content becomes available.
func TestDownstreamSSEKeepaliveThinkingWait(t *testing.T) {
	for _, name := range []string{"responses", "grok", "anthropic conversion"} {
		conversion := name == "anthropic conversion"
		t.Run(name, func(t *testing.T) {
			runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", true, 0, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
				payload := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
				if conversion {
					payload = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"role\":\"assistant\",\"model\":\"model\",\"content\":[],\"usage\":{\"input_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
				}
				client := &http.Client{Transport: keepaliveTestTransport(func(*http.Request) (*http.Response, error) {
					body := &keepaliveDelayedBody{Reader: strings.NewReader(payload), beforeRead: func() {
						time.Sleep(25 * time.Millisecond)
						synctest.Wait()
						if !strings.Contains(readKeepaliveBody(t, c, rec), ": keepalive\n\n") {
							t.Error("downstream heartbeat missing before the first upstream event")
						}
					}}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(body)}, nil
				})}
				req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.test", nil)
				require.NoError(t, err)
				resp, err := timing.Do(client, req)
				require.NoError(t, err)
				defer func() { require.NoError(t, resp.Body.Close()) }()
				timing.ObserveResponse(req.Context(), resp)
				if conversion {
					boundary := &MessageForwardBoundary{context: c}
					output := boundary.ConversionOutput(true, &messageforward.AttemptState{})
					_, err = forwardcore.ResponsesStreaming(forwardcore.Response{
						Lines: bufio.NewScanner(resp.Body), Headers: resp.Header,
						Runtime: bridge.Runtime{Now: time.Now, ReadRandom: rand.Read},
					}, output, "model", "model", nil, time.Now(), bridge.ResponsesClientToolMapping{})
				} else {
					fixture := newResponsesFixture(responsesFixtureInputs{})
					platform := capability.PlatformOpenAI
					if name == "grok" {
						platform = capability.PlatformGrok
					}
					_, err = fixture.Output.ReadStreamObservation(req.Context(), resp, c, &gatewayprovider.ExecutionProvider{Record: providercore.Record{Platform: platform}}, time.Now(), "model", "model", "")
				}
				require.NoError(t, err)
			})
		})
	}
}

// TestDownstreamSSEKeepaliveHTTPFailover exercises real network header waiting
// for every public text protocol. The client must receive a heartbeat before
// the first provider fails and then receive content from a second provider.
func TestDownstreamSSEKeepaliveHTTPFailover(t *testing.T) {
	for _, tc := range []struct {
		path      string              // public client endpoint
		protocol  protocol.ProtocolID // downstream wire protocol
		heartbeat string              // first expected SSE frame
	}{
		{"/v1/messages", protocol.ProtocolAnthropicMessages, "event: ping\ndata: {\"type\": \"ping\"}\n\n"},
		{"/v1/chat/completions", protocol.ProtocolOpenAIChatCompletions, ": keepalive\n\n"},
		{"/v1/responses", protocol.ProtocolOpenAIResponses, ": keepalive\n\n"},
		{"/v1beta/models/test:streamGenerateContent", protocol.ProtocolGeminiGenerateContent, "\n\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			releaseHeaders := make(chan struct{}, 1)
			var attempts atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attempts.Add(1) == 1 {
					select {
					case <-releaseHeaders:
					case <-r.Context().Done():
						return
					}
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"content\":\"second provider\"}\n\n")
			}))
			engine := gin.New()
			engine.POST(tc.path, TextRequestTiming(), DownstreamSSEKeepalive(10*time.Millisecond, time.Second), func(c *gin.Context) {
				c.Request = c.Request.WithContext(requeststate.WithClientProtocol(c.Request.Context(), tc.protocol))
				timing.StagesFromContext(c.Request.Context()).SetStream(true)
				baseline := HeartbeatAdjustedWrittenSize(c)
				for range 2 {
					req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, provider.URL, nil)
					if err != nil {
						t.Error(err)
						return
					}
					resp, err := timing.Do(provider.Client(), req)
					if err != nil {
						t.Error(err)
						return
					}
					timing.ObserveResponse(req.Context(), resp)
					if resp.StatusCode == http.StatusBadGateway {
						_ = resp.Body.Close()
						if HeartbeatAdjustedWrittenSize(c) != baseline {
							t.Error("heartbeat closed the retry window")
							return
						}
						continue
					}
					output := upstream.NewOutputContext(NewResponseSink(c))
					output.Header("Content-Type", "text/event-stream")
					_, err = io.Copy(output.Writer, resp.Body)
					_ = resp.Body.Close()
					if err != nil {
						t.Error(err)
					}
					return
				}
			})
			gateway := httptest.NewServer(engine)
			t.Cleanup(func() {
				close(releaseHeaders)
				gateway.Close()
				provider.Close()
			})
			client := gateway.Client()
			client.Timeout = 3 * time.Second
			resp, err := client.Post(gateway.URL+tc.path, "application/json", strings.NewReader(`{"stream":true}`))
			require.NoError(t, err)
			defer func() { require.NoError(t, resp.Body.Close()) }()
			first := make([]byte, len(tc.heartbeat))
			_, err = io.ReadFull(resp.Body, first)
			require.NoError(t, err)
			require.Equal(t, tc.heartbeat, string(first))
			require.Equal(t, http.StatusOK, resp.StatusCode)
			releaseHeaders <- struct{}{}
			rest, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Contains(t, string(rest), `"content":"second provider"`)
			require.Equal(t, int32(2), attempts.Load())
		})
	}
}

// TestDownstreamSSEKeepaliveIgnoresUpstreamHeartbeatCadence sends upstream comments
// more frequently than downstream beats. Those reads renew the stall clock,
// while clients still receive local heartbeats before the first business event.
func TestDownstreamSSEKeepaliveIgnoresUpstreamHeartbeatCadence(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", true, 12*time.Millisecond, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		reader, writer := io.Pipe()
		defer func() { require.NoError(t, reader.Close()) }()
		go func() {
			defer func() { _ = writer.Close() }()
			for range 8 {
				time.Sleep(5 * time.Millisecond)
				_, _ = io.WriteString(writer, ": upstream-only-marker\n\n")
			}
			_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"content\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_active\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
		}()
		client := &http.Client{Transport: keepaliveTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
		})}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.test", nil)
		require.NoError(t, err)
		resp, err := timing.Do(client, req)
		require.NoError(t, err)
		timing.ObserveResponse(req.Context(), resp)
		fixture := newResponsesFixture(responsesFixtureInputs{})
		_, err = fixture.Output.ReadStreamObservation(req.Context(), resp, c, &gatewayprovider.ExecutionProvider{Record: providercore.Record{Platform: capability.PlatformOpenAI}}, time.Now(), "model", "model", "")
		require.NoError(t, err)
		synctest.Wait()
		body := readKeepaliveBody(t, c, rec)
		require.NotContains(t, body, "upstream-only-marker")
		require.GreaterOrEqual(t, strings.Count(body, ": keepalive\n\n"), 2)
		require.Less(t, strings.Index(body, ": keepalive"), strings.Index(body, "response.output_text.delta"))
	})
}

// TestDownstreamSSEKeepaliveLongHeaderWait keeps the transport pending beyond
// several body inactivity budgets. Heartbeats must continue after their own
// HTTP commitment, then respect a fresh body budget when upstream headers arrive.
func TestDownstreamSSEKeepaliveLongHeaderWait(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", true, 25*time.Millisecond, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		client := &http.Client{Transport: keepaliveTestTransport(func(*http.Request) (*http.Response, error) {
			time.Sleep(85 * time.Millisecond)
			synctest.Wait()
			require.GreaterOrEqual(t, strings.Count(readKeepaliveBody(t, c, rec), ": keepalive"), 8)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.test", nil)
		require.NoError(t, err)
		resp, err := timing.Do(client, req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		time.Sleep(20 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, 10, strings.Count(readKeepaliveBody(t, c, rec), ": keepalive"))
		before := StreamHeartbeatBytes(c)
		time.Sleep(60 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, before, StreamHeartbeatBytes(c), "local beats must not renew the body stall clock")
	})
}

// TestDownstreamSSEKeepaliveHandlerActivity covers an output path without HTTP
// transport observations. Accepted handler bytes must renew the body budget,
// while subsequent timer heartbeats cannot keep a silent response alive forever.
func TestDownstreamSSEKeepaliveHandlerActivity(t *testing.T) {
	runSSEKeepaliveTest(t, protocol.ProtocolOpenAIResponses, "", true, 25*time.Millisecond, func(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
		stages := timing.StagesFromContext(c.Request.Context())
		stages.ObserveUpstreamActivity(time.Now().Add(-time.Hour))
		c.Header("Content-Type", "text/event-stream")
		for range 3 {
			_, err := c.Writer.Write([]byte("data: {\"text\":\"active\"}\n\n"))
			require.NoError(t, err)
			before := StreamHeartbeatBytes(c)
			time.Sleep(20 * time.Millisecond)
			synctest.Wait()
			require.Greater(t, StreamHeartbeatBytes(c), before)
		}
		time.Sleep(30 * time.Millisecond)
		synctest.Wait()
		before := StreamHeartbeatBytes(c)
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, before, StreamHeartbeatBytes(c))
		require.Equal(t, 3, strings.Count(readKeepaliveBody(t, c, rec), `"active"`))
	})
}
