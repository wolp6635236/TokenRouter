package googleforward_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/googleforward"
	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

type antigravityCompatErrorReader struct {
	data []byte
	off  int
	err  error
}

// antigravityFailingWriter 模拟客户端断开连接的 gin.ResponseWriter。
type antigravityFailingWriter struct {
	gin.ResponseWriter
	failAfter int // 允许成功写入的次数，之后所有写入返回错误
	writes    int
}

// cancelReadCloser 模拟读取层直接返回取消错误。
type cancelReadCloser struct{}

func TestAntigravityCompatEmptyStreamTriggersFailover(t *testing.T) {
	tests := []struct {
		name string
		run  func(*googleforward.Antigravity, *gin.Context, *http.Response) (*antigravity.StreamResult, error)
	}{
		{
			name: "chat completions",

			run: func(svc *googleforward.Antigravity, c *gin.Context, resp *http.Response) (*antigravity.StreamResult, error) {
				return googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleChatCompletionsStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", true)
			},
		},

		{
			name: "responses",

			run: func(svc *googleforward.Antigravity, c *gin.Context, resp *http.Response) (*antigravity.StreamResult, error) {
				return googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleResponsesStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", bridge.ResponsesClientToolMapping{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newAntigravityCompatibilityFixture(googleforward.Options{MaxLineSize: 500 * 1024 * 1024}, nil)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,

				Header: http.Header{"Content-Type": []string{"text/event-stream"}},

				Body: io.NopCloser(strings.NewReader("data: malformed\n\ndata: [DONE]\n\n")),
			}

			result, err := tt.run(svc, c, resp)

			require.Nil(t, result)
			var failoverErr *forwardcore.UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameProvider)
			require.Empty(t, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestAntigravityCompatUsageOnlyStreamTriggersFailover(t *testing.T) {
	tests := []struct {
		name string
		run  func(*googleforward.Antigravity, *gin.Context, *http.Response) (*antigravity.StreamResult, error)
	}{
		{
			name: "chat completions",

			run: func(svc *googleforward.Antigravity, c *gin.Context, resp *http.Response) (*antigravity.StreamResult, error) {
				return googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleChatCompletionsStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", true)
			},
		},

		{
			name: "responses",

			run: func(svc *googleforward.Antigravity, c *gin.Context, resp *http.Response) (*antigravity.StreamResult, error) {
				return googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleResponsesStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", bridge.ResponsesClientToolMapping{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newAntigravityCompatibilityFixture(googleforward.Options{MaxLineSize: 500 * 1024 * 1024}, nil)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,

				Header: http.Header{"Content-Type": []string{"text/event-stream"}},

				Body: io.NopCloser(strings.NewReader(
					`data: {"response":{"responseId":"resp_3757","usageMetadata":{"promptTokenCount":8}}}` + "\n\n",
				)),
			}

			result, err := tt.run(svc, c, resp)

			require.Nil(t, result)
			var failoverErr *forwardcore.UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameProvider)
			require.Empty(t, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestAntigravityCompatChatStreamMapsToolCallAndUsage(t *testing.T) {
	svc := newAntigravityCompatibilityFixture(googleforward.Options{MaxLineSize: 500 * 1024 * 1024}, nil)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", nil)
	body := `data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"functionCall":{"id":"call_3757","name":"get_weather","args":{"city":"Tokyo"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,

		Header: http.Header{"Content-Type": []string{"text/event-stream"}},

		Body: io.NopCloser(strings.NewReader(body)),
	}

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleChatCompletionsStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", true)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), `"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"get_weather"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":8`)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
}

func TestAntigravityCompatFirstEventTimeoutTriggersFailover(t *testing.T) {
	svc := newAntigravityCompatibilityFixture(
		googleforward.Options{MaxLineSize: 500 * 1024 * 1024, StreamInterval: 1},
		nil,
	)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", nil)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	type outcome struct {
		result *antigravity.StreamResult
		err    error
	}
	done := make(chan outcome, 1)

	go func() {
		result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleChatCompletionsStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", false)
		done <- outcome{result: result, err: err}
	}()

	select {
	case got := <-done:
		require.Nil(t, got.result)
		var failoverErr *forwardcore.UpstreamFailoverError
		require.ErrorAs(t, got.err, &failoverErr)
		require.True(t, failoverErr.RetryableOnSameProvider)
		require.Empty(t, recorder.Header().Get("Content-Type"))
	case <-time.After(2 * time.Second):
		_ = writer.Close()
		_ = reader.Close()
		t.Fatal("compat stream ignored StreamInterval")
	}
	_ = writer.Close()
	_ = reader.Close()
}

func TestAntigravityCompatClientDisconnectDrainsUsage(t *testing.T) {
	svc := newAntigravityCompatibilityFixture(googleforward.Options{MaxLineSize: 500 * 1024 * 1024}, nil)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", nil)
	c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
	body := strings.Join([]string{
		`data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}`,

		"",

		`data: {"response":{"responseId":"resp_3757","candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":15}}}`,

		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,

		Header: http.Header{"Content-Type": []string{"text/event-stream"}},

		Body: io.NopCloser(strings.NewReader(body)),
	}

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleChatCompletionsStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 15, result.Usage.OutputTokens)
}

func TestAntigravityCompatStreamErrorCommitsSingleTerminalFrame(t *testing.T) {
	svc := newAntigravityCompatibilityFixture(googleforward.Options{MaxLineSize: 500 * 1024 * 1024}, nil)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/responses", nil)
	body := []byte(`data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}` + "\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,

		Header: http.Header{"Content-Type": []string{"text/event-stream"}},

		Body: &antigravityCompatErrorReader{
			data: body,
			err:  io.ErrUnexpectedEOF,
		},
	}

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleResponsesStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", bridge.ResponsesClientToolMapping{})

	require.Error(t, err)
	require.Nil(t, result)
	require.True(t, gatewayhttp.IsResponseCommitted(c))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: error"))
}

func TestAntigravityCompatKeepaliveAfterFirstEvent(t *testing.T) {
	svc := newAntigravityCompatibilityFixture(
		googleforward.Options{MaxLineSize: 500 * 1024 * 1024, StreamKeepalive: 1},
		nil,
	)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/responses", nil)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	done := make(chan error, 1)

	go func() {
		_, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleResponsesStreamingFromAntigravity(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-3.1-pro-high", bridge.ResponsesClientToolMapping{})
		done <- err
	}()
	_, err := io.WriteString(
		writer,
		`data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}`+"\n\n",
	)
	require.NoError(t, err)
	time.Sleep(1200 * time.Millisecond)
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	require.Contains(t, recorder.Body.String(), ": ping\n\n")
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.NoError(t, reader.Close())
}

func TestHandleGeminiStreamingResponse_NormalComplete(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		// 第一个 chunk（部分内容）
		fmt.Fprintln(pw, `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`)
		fmt.Fprintln(pw, "")
		// 最后一个 chunk 携带最终内容和完整 usage。
		fmt.Fprintln(pw, `data: {"candidates":[{"content":{"parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"cachedContentTokenCount":2}}`)
		fmt.Fprintln(pw, "")
	}()

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleGeminiStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now())
	_ = pr.Close()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.ClientDisconnect, "normal completion should not set clientDisconnect")
	require.NotNil(t, result.Usage)
	// Gemini usage: promptTokenCount=10, candidatesTokenCount=8, cachedContentTokenCount=2
	// → InputTokens=10-2=8, OutputTokens=8, CacheReadInputTokens=2
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.NotNil(t, result.FirstTokenMs, "should record first token time")

	// 验证数据被透传到客户端
	body := rec.Body.String()
	require.Contains(t, body, "Hello")
	require.Contains(t, body, "world")
	// 不应包含错误事件
	require.NotContains(t, body, "event: error")
}

// TestHandleClaudeStreamingResponse_NormalComplete
// 验证：正常 Claude 流式转发（Gemini→Claude 转换），数据正确转换并输出。
func TestHandleClaudeStreamingResponse_NormalComplete(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		// v1internal 包装格式：Gemini 数据嵌套在 "response" 字段下
		// ProcessLine 先尝试反序列化为 V1InternalResponse，裸格式会导致 Response.UsageMetadata 为空
		fmt.Fprintln(pw, `data: {"response":{"candidates":[{"content":{"parts":[{"text":"Hi there"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}}`)
		fmt.Fprintln(pw, "")
	}()

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleClaudeStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "claude-sonnet-4-5")
	_ = pr.Close()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.ClientDisconnect, "normal completion should not set clientDisconnect")
	require.NotNil(t, result.Usage)
	// Gemini→Claude 转换的 usage：promptTokenCount=5→InputTokens=5, candidatesTokenCount=3→OutputTokens=3
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.NotNil(t, result.FirstTokenMs, "should record first token time")

	// 验证输出是 Claude SSE 格式（processor 会转换）
	body := rec.Body.String()
	require.Contains(t, body, "event: message_start", "should contain Claude message_start event")
	require.Contains(t, body, "event: message_stop", "should contain Claude message_stop event")
	// 不应包含错误事件
	require.NotContains(t, body, "event: error")
}

// TestHandleGeminiStreamingResponse_ThoughtsTokenCount
// 验证：Gemini 流式转发时 thoughtsTokenCount 被计入 OutputTokens。
func TestHandleGeminiStreamingResponse_ThoughtsTokenCount(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		fmt.Fprintln(pw, `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"thoughtsTokenCount":50}}`)
		fmt.Fprintln(pw, "")
		fmt.Fprintln(pw, `data: {"candidates":[{"content":{"parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":30,"thoughtsTokenCount":80,"cachedContentTokenCount":10}}`)
		fmt.Fprintln(pw, "")
	}()

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleGeminiStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now())
	_ = pr.Close()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Usage)
	// promptTokenCount=100, cachedContentTokenCount=10 → InputTokens=90
	require.Equal(t, 90, result.Usage.InputTokens)
	// candidatesTokenCount=30 + thoughtsTokenCount=80 → OutputTokens=110
	require.Equal(t, 110, result.Usage.OutputTokens)
	require.Equal(t, 10, result.Usage.CacheReadInputTokens)
}

// TestHandleClaudeStreamingResponse_ThoughtsTokenCount
// 验证：Gemini→Claude 流式转换时 thoughtsTokenCount 被计入 OutputTokens。
func TestHandleClaudeStreamingResponse_ThoughtsTokenCount(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		fmt.Fprintln(pw, `data: {"response":{"candidates":[{"content":{"parts":[{"text":"Hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":10,"thoughtsTokenCount":25}}}`)
		fmt.Fprintln(pw, "")
	}()

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleClaudeStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "gemini-2.5-pro")
	_ = pr.Close()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Usage)
	// promptTokenCount=50 → InputTokens=50
	require.Equal(t, 50, result.Usage.InputTokens)
	// candidatesTokenCount=10 + thoughtsTokenCount=25 → OutputTokens=35
	require.Equal(t, 35, result.Usage.OutputTokens)
}

func TestHandleGeminiStreamingResponse_ClientDisconnect(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		fmt.Fprintln(pw, `data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":10}}`)
		fmt.Fprintln(pw, "")
	}()

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleGeminiStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now())
	_ = pr.Close()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.NotContains(t, rec.Body.String(), "write_failed")
}

// TestHandleGeminiStreamingResponse_ContextCanceled
// 验证：context 取消时不注入错误事件。
func TestHandleGeminiStreamingResponse_ContextCanceled(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)

	resp := &http.Response{StatusCode: http.StatusOK, Body: cancelReadCloser{}, Header: http.Header{}}

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleGeminiStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.NotContains(t, rec.Body.String(), "event: error")
}

// TestHandleClaudeStreamingResponse_ClientDisconnect
// 验证：Claude 流式转发中客户端断开后继续 drain 上游。
func TestHandleClaudeStreamingResponse_ClientDisconnect(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		// v1internal 包装格式
		fmt.Fprintln(pw, `data: {"response":{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":15}}}`)
		fmt.Fprintln(pw, "")
	}()

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleClaudeStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "claude-sonnet-4-5")
	_ = pr.Close()

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
}

// TestHandleClaudeStreamingResponse_EmptyStream
// 上游返回的 SSE 行均无法解析时，返回 UpstreamFailoverError。
func TestHandleClaudeStreamingResponse_EmptyStream(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr, Header: http.Header{}}

	go func() {
		defer func() { _ = pw.Close() }()
		// 所有行均为无法 JSON 解析的内容，ProcessLine 全部返回 nil
		fmt.Fprintln(pw, "data: not-valid-json")
		fmt.Fprintln(pw, "")
		fmt.Fprintln(pw, "data: also-invalid")
		fmt.Fprintln(pw, "")
	}()

	_, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleClaudeStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "claude-sonnet-4-5")
	_ = pr.Close()

	// 返回 UpstreamFailoverError，由上层触发 failover。
	require.Error(t, err)
	var failoverErr *forwardcore.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.RetryableOnSameProvider)

	// 客户端不应收到任何 SSE 事件（既无 message_start 也无 message_stop）
	body := rec.Body.String()
	require.NotContains(t, body, "event: message_start")
	require.NotContains(t, body, "event: message_stop")
	require.NotContains(t, body, "event: message_delta")
}

// TestHandleClaudeStreamingResponse_ContextCanceled
// 验证：context 取消时不注入错误事件。
func TestHandleClaudeStreamingResponse_ContextCanceled(t *testing.T) {
	svc := newAntigravityStreamFixture(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)

	resp := &http.Response{StatusCode: http.StatusOK, Body: cancelReadCloser{}, Header: http.Header{}}

	result, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{Output: gatewayhttp.NewGoogleBoundary(c, svc.Options, true)}).HandleClaudeStreamingResponse(upstream.NewOutputContext(gatewayhttp.NewGoogleBoundary(c, svc.Options, true).Sink()), resp, time.Now(), "claude-sonnet-4-5")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.NotContains(t, rec.Body.String(), "event: error")
}

func TestAntigravityClientWriter(t *testing.T) {
	t.Run("normal write succeeds", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		flusher, _ := c.Writer.(http.Flusher)
		cw := antigravity.NewClientWriter(c.Writer, flusher, "test")

		ok := cw.Write([]byte("hello"))
		require.True(t, ok)
		require.False(t, cw.Disconnected())
		require.Contains(t, rec.Body.String(), "hello")
	})

	t.Run("write failure marks disconnected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		fw := &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
		flusher, _ := c.Writer.(http.Flusher)
		cw := antigravity.NewClientWriter(fw, flusher, "test")

		ok := cw.Write([]byte("hello"))
		require.False(t, ok)
		require.True(t, cw.Disconnected())
	})

	t.Run("subsequent writes are no-op", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		fw := &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
		flusher, _ := c.Writer.(http.Flusher)
		cw := antigravity.NewClientWriter(fw, flusher, "test")

		cw.Write([]byte("first"))
		ok := cw.Fprintf("second %d", 2)
		require.False(t, ok)
		require.True(t, cw.Disconnected())
	})
}

// TestUnwrapV1InternalResponse 测试 unwrapV1InternalResponse 的各种输入场景。
func TestUnwrapV1InternalResponse(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	// 构造 >50KB 的大型 JSON
	largePadding := strings.Repeat("x", 50*1024)
	largeInput := []byte(fmt.Sprintf(`{"response":{"id":"big","pad":"%s"}}`, largePadding))
	largeExpected := fmt.Sprintf(`{"id":"big","pad":"%s"}`, largePadding)

	tests := []struct {
		name     string
		input    []byte
		expected string
		wantErr  bool
	}{
		{
			name: "正常 response 包装",

			input: []byte(`{"response":{"id":"123","content":"hello"}}`),

			expected: `{"id":"123","content":"hello"}`,
		},

		{
			name:     "无 response 透传",
			input:    []byte(`{"id":"456"}`),
			expected: `{"id":"456"}`,
		},

		{
			name:     "空 JSON",
			input:    []byte(`{}`),
			expected: `{}`,
		},

		{
			name:     "response 为 null",
			input:    []byte(`{"response":null}`),
			expected: `null`,
		},

		{
			name:     "response 为基础类型 string",
			input:    []byte(`{"response":"hello"}`),
			expected: `"hello"`,
		},

		{
			name:     "非法 JSON",
			input:    []byte(`not json`),
			expected: `not json`,
		},

		{
			name: "嵌套 response 只解一层",

			input: []byte(`{"response":{"response":{"inner":true}}}`),

			expected: `{"response":{"inner":true}}`,
		},

		{
			name:     "大型 JSON >50KB",
			input:    largeInput,
			expected: largeExpected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).UnwrapV1InternalResponse(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, strings.TrimSpace(string(got)))
		})
	}
}

func BenchmarkUnwrapV1Internal_Old_Small(b *testing.B) {
	body := []byte(`{"response":{"candidates":[{"content":{"parts":[{"text":"hello world"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}}`)
	b.ResetTimer()
	for range b.N {
		_, _ = unwrapV1InternalResponseOld(body)
	}
}

func BenchmarkUnwrapV1Internal_New_Small(b *testing.B) {
	body := []byte(`{"response":{"candidates":[{"content":{"parts":[{"text":"hello world"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}}`)
	svc := newAntigravityFixture(antigravityDependencies{})
	b.ResetTimer()
	for range b.N {
		_, _ = googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).UnwrapV1InternalResponse(body)
	}
}

func BenchmarkUnwrapV1Internal_Old_Large(b *testing.B) {
	body := generateLargeUnwrapJSON(10 * 1024) // ~10KB
	b.ResetTimer()
	for range b.N {
		_, _ = unwrapV1InternalResponseOld(body)
	}
}

func BenchmarkUnwrapV1Internal_New_Large(b *testing.B) {
	body := generateLargeUnwrapJSON(10 * 1024) // ~10KB
	svc := newAntigravityFixture(antigravityDependencies{})
	b.ResetTimer()
	for range b.N {
		_, _ = googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).UnwrapV1InternalResponse(body)
	}
}

// TestExtractImageSize_ValidSizes 测试有效尺寸解析。
func TestExtractImageSize_ValidSizes(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	// 1K
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"1K"}}}`)
	require.Equal(t, "1K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	// 2K
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"2K"}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	// 4K
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"4K"}}}`)
	require.Equal(t, "4K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))
}

// TestExtractImageSize_CaseInsensitive 测试大小写不敏感。
func TestExtractImageSize_CaseInsensitive(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"1k"}}}`)
	require.Equal(t, "1K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"4k"}}}`)
	require.Equal(t, "4K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))
}

// TestExtractImageSize_Default 测试无 imageConfig 返回默认 2K。
func TestExtractImageSize_Default(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	// 无 generationConfig
	body := []byte(`{"contents":[]}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	// 有 generationConfig 但无 imageConfig
	body = []byte(`{"generationConfig":{"temperature":0.7}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	// 有 imageConfig 但无 imageSize
	body = []byte(`{"generationConfig":{"imageConfig":{}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))
}

// TestExtractImageSize_InvalidJSON 测试非法 JSON 返回默认 2K。
func TestExtractImageSize_InvalidJSON(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	body := []byte(`not valid json`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	body = []byte(`{"broken":`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))
}

// TestExtractImageSize_EmptySize 测试空 imageSize 返回默认 2K。
func TestExtractImageSize_EmptySize(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":""}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	// 空格
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"   "}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))
}

// TestExtractImageSize_InvalidSize 测试无效尺寸返回默认 2K。
func TestExtractImageSize_InvalidSize(t *testing.T) {
	svc := newAntigravityFixture(antigravityDependencies{})

	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"3K"}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"8K"}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"invalid"}}}`)
	require.Equal(t, "2K", pricing.NormalizeImageBillingTierOrDefault(googleforward.AntigravityResponseForTest(svc, &googleforward.AttemptForTest{}).ExtractImageInputSize(body)))
}

func (r *antigravityCompatErrorReader) Read(p []byte) (int, error) {
	if r.off < len(r.data) {
		n := copy(p, r.data[r.off:])
		r.off += n
		return n, nil
	}
	return 0, r.err
}

func (r *antigravityCompatErrorReader) Close() error { return nil }

func (w *antigravityFailingWriter) Write(p []byte) (int, error) {
	if w.writes >= w.failAfter {
		return 0, errors.New("write failed: client disconnected")
	}
	w.writes++
	return w.ResponseWriter.Write(p)
}

// unwrapV1InternalResponseOld 旧实现：Unmarshal+Marshal 双重开销（仅用于 benchmark 对照）。
func unwrapV1InternalResponseOld(body []byte) ([]byte, error) {
	var outer map[string]any
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, err
	}
	if resp, ok := outer["response"]; ok {
		return json.Marshal(resp)
	}
	return body, nil
}

// generateLargeUnwrapJSON 生成指定最小大小的包含 response 包装的 JSON。
func generateLargeUnwrapJSON(minSize int) []byte {
	parts := make([]map[string]string, 0)
	current := 0
	for current < minSize {
		text := fmt.Sprintf("这是第 %d 段内容，用于填充 JSON 到目标大小。", len(parts)+1)
		parts = append(parts, map[string]string{"text": text})
		current += len(text) + 20 // 估算 JSON 编码开销
	}
	inner := map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{"parts": parts}},
		},

		"usageMetadata": map[string]any{
			"promptTokenCount":     100,
			"candidatesTokenCount": 50,
		},
	}
	outer := map[string]any{"response": inner}
	b, _ := json.Marshal(outer)
	return b
}

func (cancelReadCloser) Read([]byte) (int, error) { return 0, context.Canceled }

func (cancelReadCloser) Close() error { return nil }
