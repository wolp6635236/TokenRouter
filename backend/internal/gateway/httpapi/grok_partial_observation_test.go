package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 读取器在输出内容和用量后报错，测试检查返回结果。
type grokObservationErrorReader struct{ err error }

func (r grokObservationErrorReader) Read([]byte) (int, error) { return 0, r.err }

// TestGrokNativeObservationRetainsPartialUsageAfterReadError 验证可见输出后的读取错误仍保留用量。
func TestGrokNativeObservationRetainsPartialUsageAfterReadError(t *testing.T) {
	failure := errors.New("fixture truncated stream")
	payload := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\",\"usage\":{\"input_tokens\":9,\"output_tokens\":2}}\n\n"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 470, Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(io.MultiReader(strings.NewReader(payload), grokObservationErrorReader{failure}))}
	service := newResponseOutputForTest(OpenAIResponseOptions{})
	result, err := service.ReadStreamObservation(context.Background(), response, c, provider, time.Now(), "grok-fixture", "grok-fixture", "")
	require.Error(t, err)
	require.Contains(t, recorder.Body.String(), "visible")
	require.NotNil(t, result)
	require.True(t, result.Served)
	require.True(t, result.HasUsage)
	require.True(t, result.HttpCommitted)
	require.NotNil(t, result.FirstSemanticOutput)
	require.False(t, result.ObservedOnly)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
}
