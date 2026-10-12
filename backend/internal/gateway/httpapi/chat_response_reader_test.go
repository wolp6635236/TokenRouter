package httpapi

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

type openAICompatBufferedReadErrorCloser struct{ err error }

func TestHandleChatStreamingResponse_SilentRefusalReasoningSummaryExempt(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	upstreamBody := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_reasoning","model":"gpt-5.5"}}`,
		"",
		`data: {"type":"response.reasoning_summary_text.delta","delta":"thinking only"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_reasoning","model":"gpt-5.5","status":"completed"}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_reasoning"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	svc := newResponsesFixture(responsesFixtureInputs{options: protocolHTTPOptions()})

	result, err := svc.Output.ChatStreaming(
		resp,
		c,
		rawChatCompletionsTestProvider(),
		"gpt-5.5",
		"gpt-5.5",
		"gpt-5.5",
		time.Now(),
		openai.SilentRefusalMinRequestBodyBytes,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), `"reasoning_content":"thinking only"`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestHandleChatStreamingResponse_ClassifiesHTTP2ReadError(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"upstream-rid"},
		},
		Body: &openAIChatStreamReadErrorCloser{
			payload: []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"),
			err:     errors.New("stream error: stream ID 5; INTERNAL_ERROR; received from peer"),
		},
	}
	svc := newResponsesFixture(responsesFixtureInputs{options: &responsesFixtureOptions{}})

	result, err := svc.Output.ChatStreaming(
		resp,
		c,
		&gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 1, Name: "openai-oauth", Platform: capability.PlatformOpenAI}},
		"gpt-5.6-sol",
		"gpt-5.6-sol",
		"gpt-5.6-sol",
		time.Now(),
		0,
	)

	require.Error(t, err)
	require.NotNil(t, result)
	require.True(t, c.Writer.Written(), "partial output must make replay unsafe")
	code, message, ok := openai.OpenAIUpstreamStreamReadErrorDetails(err)
	require.True(t, ok)
	require.Equal(t, openai.OpenAIUpstreamHTTP2StreamErrorCode, code)
	require.Equal(t, "Upstream HTTP/2 stream failed", message)
	require.NotContains(t, message, "stream ID")
	require.NotContains(t, message, "INTERNAL_ERROR")
}

func (r *openAICompatBufferedReadErrorCloser) Read([]byte) (int, error) { return 0, r.err }

func (r *openAICompatBufferedReadErrorCloser) Close() error { return nil }

func TestChatCompletionsBufferedResponsesReadErrorReturnsFailover(t *testing.T) {
	readErrors := []struct {
		name string
		err  error
		code string
	}{
		{name: "unexpected_eof", err: io.ErrUnexpectedEOF, code: openai.OpenAIUpstreamStreamReadErrorCode},
		{name: "http2_reset", err: errors.New("stream error: stream ID 7; INTERNAL_ERROR; received from peer"), code: openai.OpenAIUpstreamHTTP2StreamErrorCode},
	}
	for _, test := range readErrors {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"upstream-rid"}},
				Body:       &openAICompatBufferedReadErrorCloser{err: test.err},
			}
			result, err := newResponsesFixture(responsesFixtureInputs{}).Output.ChatBuffered(
				resp, c, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 40, Name: "openai-oauth", Platform: capability.PlatformOpenAI}},
				"gpt-5.6-sol", "gpt-5.6-sol", "gpt-5.6-sol", time.Now(),
			)
			require.Error(t, err)
			require.Nil(t, result)
			var failoverErr *forwardcore.UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
			require.Equal(t, "upstream-rid", http.Header(failoverErr.ResponseHeaders).Get("x-request-id"))
			require.Equal(t, test.code, gjson.GetBytes(failoverErr.ResponseBody, "error.code").String())
			require.Empty(t, recorder.Body.String())
			require.False(t, c.Writer.Written())
		})
	}
}

func TestChatCompletionsBufferedResponsesReadErrorDoesNotFailoverAfterClientCancel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &openAICompatBufferedReadErrorCloser{err: io.ErrUnexpectedEOF}}
	result, err := newResponsesFixture(responsesFixtureInputs{}).Output.ChatBuffered(
		resp, c, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 40, Name: "openai-oauth", Platform: capability.PlatformOpenAI}},
		"gpt-5.6-sol", "gpt-5.6-sol", "gpt-5.6-sol", time.Now(),
	)
	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *forwardcore.UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
}

func TestChatCompletionsBufferedResponsesOversizedLineDoesNotFailover(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &openAICompatBufferedReadErrorCloser{err: bufio.ErrTooLong}}
	result, err := newResponsesFixture(responsesFixtureInputs{}).Output.ChatBuffered(
		resp, c, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 40, Name: "openai-oauth", Platform: capability.PlatformOpenAI}},
		"gpt-5.6-sol", "gpt-5.6-sol", "gpt-5.6-sol", time.Now(),
	)
	require.ErrorIs(t, err, bufio.ErrTooLong)
	require.Nil(t, result)
	var failoverErr *forwardcore.UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
}
