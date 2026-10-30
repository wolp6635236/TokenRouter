package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// gemini_sdk_scenario_test.go connects gateway_middleware.go, gateway_google.go,
// and gateway/httpapi/routes.go to isolated official SDK processes. Synthetic
// stores retain production forwarding and output behavior, while optional live
// traffic obtains credentials only from the explicitly enabled test environment.
// Ordinary route failures run without installing any SDK. The opt-in client
// processes exercise each library's real SSE decoder, including heartbeats that
// arrive before model output and errors that follow partially delivered content.
//
// Credentials for live checks remain inside the local upstream proxy. Diagnostic
// output records field names, counters, and outcomes instead of prompts, model
// text, or secrets. Registered routes own admission, provider execution, retry
// commitment, and completion exactly as in the application; test stores replace
// persistent side effects and use bounded fixture transport budgets.

// geminiSDKClient selects one isolated executable and its fixed script arguments.
type geminiSDKClient struct {
	name       string   // SDK implementation label
	executable string   // client process path
	arguments  []string // fixed script arguments
}

// TestGeminiRouteRetryCommitment compares waiting heartbeats with actual delivered content.
// Registered routes must switch providers after heartbeat-only authentication failure,
// but a stream error following content must terminate without contacting the fallback.
func TestGeminiRouteRetryCommitment(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/v1beta/models/gemini-2.5-flash:streamGenerateContent"} {
		for _, delivered := range []bool{false, true} {
			name := "heartbeat only"
			if delivered {
				name = "content delivered"
			}
			t.Run(path+"/"+name, func(t *testing.T) {
				var firstCalls, secondCalls atomic.Int32
				fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					secondCalls.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"second-provider\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}\n\n")
				}))
				defer fallback.Close()
				first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					firstCalls.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
					time.Sleep(60 * time.Millisecond)
					w.Header().Set("X-Request-ID", "first-attempt")
					if !delivered {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, `{"error":{"code":401,"message":"fixture authentication"}}`)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"first-provider\"}]}}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}\n\n")
					_ = http.NewResponseController(w).Flush()
					_, _ = io.WriteString(w, "data: {\"error\":{\"code\":503,\"message\":\"fixture stream failure\"}}\n\n")
				}))
				defer first.Close()
				router, _ := newHeartbeatRouteRouter(t, first.URL, "gemini", 10*time.Millisecond, heartbeatRouteOptions{FallbackURL: fallback.URL})
				server := httptest.NewServer(router)
				defer server.Close()
				body := `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"stream":true}`
				if path == "/v1/responses" {
					body = `{"model":"gemini-2.5-flash","input":"hi","stream":true}`
				}
				if strings.Contains(path, "streamGenerateContent") {
					body = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`
				}
				client := server.Client()
				client.Timeout = 5 * time.Second
				response, err := client.Post(server.URL+path, "application/json", strings.NewReader(body))
				require.NoError(t, err)
				defer func() { require.NoError(t, response.Body.Close()) }()
				payload, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, int32(1), firstCalls.Load())
				if delivered {
					require.Zero(t, secondCalls.Load())
					require.Contains(t, string(payload), "first-provider")
					require.Contains(t, string(payload), `"error"`)
					require.NotContains(t, string(payload), "second-provider")
				} else {
					require.Equal(t, int32(1), secondCalls.Load(), string(payload))
					require.Contains(t, string(payload), "second-provider")
					require.NotContains(t, string(payload), `"error"`)
				}
			})
		}
	}
}

// TestGeminiRouteStreamFailures presents native failures through every client protocol.
// Heartbeat-only responses can exhaust existing retry policy, while partially
// delivered content must receive one error without a fabricated successful terminal.
func TestGeminiRouteStreamFailures(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/v1beta/models/gemini-2.5-flash:streamGenerateContent"} {
		for _, scenario := range []string{"http failure", "quota", "empty", "missing finish", "invalid json"} {
			t.Run(path+"/"+scenario, func(t *testing.T) {
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("X-Request-ID", "route-failure-id")
					status := http.StatusOK
					if scenario == "http failure" {
						status = http.StatusBadRequest
					}
					w.WriteHeader(status)
					_ = http.NewResponseController(w).Flush()
					time.Sleep(60 * time.Millisecond)
					switch scenario {
					case "http failure":
						_, _ = io.WriteString(w, `{"error":{"code":400,"message":"fixture bad request"}}`)
					case "quota":
						_, _ = io.WriteString(w, "data: {\"error\":{\"code\":429,\"message\":\"fixture quota\"}}\n\n")
					case "missing finish":
						_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial-content\"}]}}],\"usageMetadata\":{\"promptTokenCount\":2}}\n\n")
					case "invalid json":
						_, _ = io.WriteString(w, "data: {invalid}\n\n")
					}
				}))
				defer provider.Close()
				router, _ := newHeartbeatRouteRouter(t, provider.URL, "gemini", 10*time.Millisecond)
				server := httptest.NewServer(router)
				defer server.Close()
				body := `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"stream":true}`
				if path == "/v1/responses" {
					body = `{"model":"gemini-2.5-flash","input":"hi","stream":true}`
				}
				if strings.Contains(path, "streamGenerateContent") {
					body = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`
				}
				client := server.Client()
				client.Timeout = 5 * time.Second
				response, err := client.Post(server.URL+path, "application/json", strings.NewReader(body))
				require.NoError(t, err)
				defer func() { require.NoError(t, response.Body.Close()) }()
				payload, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.StatusCode, string(payload))
				require.Contains(t, string(payload), `"error"`)
				require.NotContains(t, string(payload), "event: message_stop")
				require.NotContains(t, string(payload), "response.completed")
				require.NotContains(t, string(payload), "data: [DONE]")
				if scenario == "missing finish" {
					require.Contains(t, string(payload), "partial-content")
				}
			})
		}
	}
}

// geminiSDKClients builds the pinned Go client and resolves the other SDK runners.
// The opt-in fixture requires its documented Python and Node dependencies. SDK
// modules remain isolated from the application's dependency graph and test process.
func geminiSDKClients(t *testing.T) []geminiSDKClient {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "tools", "gemini_sdk")
	binary := filepath.Join(t.TempDir(), "gemini-go-client")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(root, "go")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	python := os.Getenv("TOKENROUTER_GEMINI_SDK_PYTHON")
	if python == "" {
		python = "python3"
	}
	return []geminiSDKClient{
		{name: "go", executable: binary},
		{name: "python", executable: python, arguments: []string{filepath.Join(root, "python", "client.py")}},
		{name: "typescript", executable: "node", arguments: []string{filepath.Join(root, "typescript", "client.mjs")}},
	}
}

// runGeminiSDK waits for one bounded SDK process and reports its actual decoder result.
// The process receives only a local fixture URL and synthetic authentication;
// provider credentials are retained by the optional upstream proxy, never arguments.
func runGeminiSDK(t *testing.T, client geminiSDKClient, url, outcome, model, prompt string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	args := append(append([]string{}, client.arguments...), url, outcome, model, prompt)
	command := exec.CommandContext(ctx, client.executable, args...)
	output, err := command.CombinedOutput()
	t.Logf("%s: %s", client.name, strings.TrimSpace(string(output)))
	require.NoError(t, err, string(output))
	compact := strings.ReplaceAll(string(output), " ", "")
	require.Contains(t, compact, `"ok":true`)
}

// TestGeminiSDKRoutes exercises native generation and failures through registered routes.
// All three real SDKs must ignore waiting heartbeats, retain ordinary model output,
// and recognize Google API errors instead of JSON framing or empty-stream failures.
func TestGeminiSDKRoutes(t *testing.T) {
	if os.Getenv("TOKENROUTER_GEMINI_SDK_TESTS") != "1" {
		t.Skip("enable isolated Google SDK route tests explicitly")
	}
	clients := geminiSDKClients(t)
	for _, scenario := range []string{"success", "quota", "truncated", "empty"} {
		for _, client := range clients {
			t.Run(scenario+"/"+client.name, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("X-Request-ID", "sdk-upstream-request")
					w.WriteHeader(http.StatusOK)
					_ = http.NewResponseController(w).Flush()
					time.Sleep(50 * time.Millisecond)
					if scenario == "empty" {
						return
					}
					if scenario == "quota" {
						_, _ = io.WriteString(w, "data: {\"error\":{\"code\":429,\"status\":\"RESOURCE_EXHAUSTED\",\"message\":\"fixture quota\"}}\n\n")
						return
					}
					_, _ = io.WriteString(w, ": upstream heartbeat\n\ndata: {\"candidates\":[]}\n\ndata: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"sdk-content\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":1}}\n\n")
					_ = http.NewResponseController(w).Flush()
					if scenario == "truncated" {
						return
					}
					time.Sleep(40 * time.Millisecond)
					_, _ = io.WriteString(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":3,\"totalTokenCount\":13}}\n\n")
				}))
				defer upstream.Close()
				router, _ := newHeartbeatRouteRouter(t, upstream.URL, "gemini", 10*time.Millisecond)
				gateway := httptest.NewServer(router)
				defer gateway.Close()
				outcome := "success"
				if scenario != "success" {
					outcome = "error"
				}
				runGeminiSDK(t, client, gateway.URL, outcome, "gemini-2.5-flash", "Reply with exactly OK.")
			})
		}
	}
}

// TestGeminiSDKLongWait exceeds the edge proxy's 125-second response timeout.
// A real Go SDK consumes production-route heartbeats throughout the controlled
// wait, then receives content and normal completion without synthetic chunks.
func TestGeminiSDKLongWait(t *testing.T) {
	if os.Getenv("TOKENROUTER_GEMINI_SDK_LONG_WAIT") != "1" {
		t.Skip("enable the 130-second route verification explicitly")
	}
	client := geminiSDKClients(t)[0]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-ID", "long-wait-upstream")
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		select {
		case <-time.After(130 * time.Second):
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"long-wait-completed\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1}}\n\n")
	}))
	defer upstream.Close()
	router, _ := newHeartbeatRouteRouter(t, upstream.URL, "gemini", 10*time.Second, heartbeatRouteOptions{Stall: 180 * time.Second, HeaderTimeout: 180 * time.Second})
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	start := time.Now()
	runGeminiSDK(t, client, gateway.URL, "success", "gemini-2.5-flash", "Reply with exactly OK.")
	require.GreaterOrEqual(t, time.Since(start), 130*time.Second)
}

// TestGeminiSDKLive forwards two bounded prompts through the actual local gateway.
// Explicit environment opt-in supplies the authorized upstream; credentials stay
// inside the proxy and never enter client arguments or fixture provider metadata.
func TestGeminiSDKLive(t *testing.T) {
	if os.Getenv("TOKENROUTER_GEMINI_SDK_LIVE") != "1" {
		t.Skip("real provider requests require explicit opt-in")
	}
	base, key := os.Getenv("TOKENROUTER_GEMINI_LIVE_BASE_URL"), os.Getenv("TOKENROUTER_GEMINI_LIVE_KEY")
	require.NotEmpty(t, base)
	require.NotEmpty(t, key)
	client := geminiSDKClients(t)[0]
	transport := &http.Client{Timeout: 180 * time.Second}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(base, "/")+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, "fixture request failed", http.StatusInternalServerError)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("x-goog-api-key", key)
		resp, err := transport.Do(req)
		if err != nil {
			http.Error(w, "fixture upstream failed", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for _, header := range []string{"Content-Type", "X-Request-ID"} {
			if value := resp.Header.Get(header); value != "" {
				w.Header().Set(header, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_ = http.NewResponseController(w).Flush()
		buffer := make([]byte, 4096)
		pending := ""
		for {
			n, err := resp.Body.Read(buffer)
			if n > 0 {
				pending += string(buffer[:n])
				for {
					end := strings.IndexByte(pending, '\n')
					if end < 0 {
						break
					}
					line := strings.TrimSpace(pending[:end])
					pending = pending[end+1:]
					if payload, ok := strings.CutPrefix(line, "data:"); ok {
						var fields []string
						gjson.Parse(payload).ForEach(func(key, value gjson.Result) bool { fields = append(fields, key.String()); return true })
						t.Logf("live upstream fields=%q candidates=%d", fields, gjson.Get(payload, "candidates.#").Int())
					}
				}
				if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
					return
				}
				_ = http.NewResponseController(w).Flush()
			}
			if err != nil {
				return
			}
		}
	}))
	defer proxy.Close()
	router, _ := newHeartbeatRouteRouter(t, proxy.URL, "gemini", 10*time.Second, heartbeatRouteOptions{Model: "gemini-4-argon", Stall: 180 * time.Second, HeaderTimeout: 180 * time.Second})
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	for index, prompt := range []string{"Reply with exactly OK.", "构思一个包含十二位角色、三条交叉时间线和六处反转的密室悬疑故事。认真核对行动时间、证词、动机与线索是否自洽。最后只输出不超过二百字的梗概，不输出思考过程。"} {
		if os.Getenv("TOKENROUTER_GEMINI_LIVE_CASE") == "high" && index == 0 {
			continue
		}
		runGeminiSDK(t, client, gateway.URL, "success", "gemini-4-argon", prompt)
	}
}
