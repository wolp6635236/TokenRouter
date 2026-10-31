package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
)

// SystemOne composite-prefix failures use the decision endpoint's model error
// envelope, including its machine-readable code. Endpoint matching accepts a
// trailing slash while keeping unrelated SystemOne-like paths out of that response choice.

// TestSystemOneCompositeErrorUsesDecisionEndpoint exercises the decision path
// with and without a trailing slash. Both requests must abort with a model
// parameter error and the composite-prefix-required code, while other paths
// retain their own endpoint classification.
func TestSystemOneCompositeErrorUsesDecisionEndpoint(t *testing.T) {
	for _, path := range []string{EndpointSystemOne, EndpointSystemOne + "/"} {
		require.True(t, IsSystemOneEndpoint(path))
		require.False(t, IsOpenAICompositeEndpoint(path))
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		AbortCompositeKeyError(c, apikey.ErrCompositeKeyPrefixRequired)
		require.True(t, c.IsAborted())
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), `"code":"COMPOSITE_KEY_MODEL_PREFIX_REQUIRED"`)
		require.Contains(t, rec.Body.String(), `"param":"model"`)
	}
	require.False(t, IsSystemOneEndpoint("/v1/chat/completions"))
	require.False(t, IsSystemOneEndpoint("/other/systemone"))
	require.True(t, IsOpenAICompositeEndpoint("/v1/chat/completions"))
}

// TestSystemOneModelExtractionDefersQuestionValidation preserves admission's
// missing-model error while reading routing identity from incomplete questions.
// The SystemOne endpoint owns full question validation after key selection.
func TestSystemOneModelExtractionDefersQuestionValidation(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, EndpointSystemOne, strings.NewReader(`{"model":"GPT/alias","questions":{}}`))
	model, err := CompositeModelFromRequest(request)
	require.NoError(t, err)
	require.Equal(t, "GPT/alias", model)
	request = httptest.NewRequest(http.MethodPost, EndpointSystemOne, strings.NewReader(`{"state":"test"}`))
	_, err = CompositeModelFromRequest(request)
	require.ErrorIs(t, err, apikey.ErrCompositeKeyPrefixRequired)
}
