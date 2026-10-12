package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWriteOpenAIUpstreamClientError_UsesSafeFallbacks(t *testing.T) {
	c, recorder := newOpenAIUpstreamClientErrorTestContext()

	WriteOpenAIUpstreamClientError(c, http.StatusBadRequest, []byte(`<html>bad request</html>`), "")

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, openAIUpstreamClientErrorFallbackType, gjson.Get(recorder.Body.String(), "error.type").String())
	require.Equal(t, openAIUpstreamClientErrorFallbackMessage, gjson.Get(recorder.Body.String(), "error.message").String())
	require.False(t, gjson.Get(recorder.Body.String(), "error.code").Exists())
	require.False(t, gjson.Get(recorder.Body.String(), "error.param").Exists())
}

func TestWriteOpenAIUpstreamClientError_UsesSanitizedMessage(t *testing.T) {
	c, recorder := newOpenAIUpstreamClientErrorTestContext()
	body := []byte(`{"error":{"message":"failed at https://example.test?key=secret123","type":"invalid_request_error","code":"bad_value","param":"input"}}`)

	WriteOpenAIUpstreamClientError(c, http.StatusBadRequest, body, "failed at https://example.test?key=***")

	require.Equal(t, "failed at https://example.test?key=***", gjson.Get(recorder.Body.String(), "error.message").String())
	require.Equal(t, "bad_value", gjson.Get(recorder.Body.String(), "error.code").String())
	require.Equal(t, "input", gjson.Get(recorder.Body.String(), "error.param").String())
	require.NotContains(t, recorder.Body.String(), "secret123")
}

func TestIsOpenAIDeterministicClientError(t *testing.T) {
	require.True(t, IsOpenAIDeterministicClientError(http.StatusBadRequest, false))
	require.False(t, IsOpenAIDeterministicClientError(http.StatusBadRequest, true))
	for _, statusCode := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests,
		http.StatusBadGateway,
	} {
		require.False(t, IsOpenAIDeterministicClientError(statusCode, false))
	}
}
