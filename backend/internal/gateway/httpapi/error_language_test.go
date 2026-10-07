package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGatewayErrorsUseEnglish 检查请求语言变化时错误类型和英文提示保持一致。
func TestGatewayErrorsUseEnglish(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, stream := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil).WithContext(locale.WithLanguage(context.Background(), language))
			c.Request.Header.Set("Accept-Language", language)
			WriteAnthropicFailover(c, &forward.UpstreamFailoverError{StatusCode: 529}, "anthropic", stream, nil, func([]byte) bool { return false }, "")
			require.Contains(t, recorder.Body.String(), `"type":"overloaded_error"`)
			require.Contains(t, recorder.Body.String(), "Upstream service overloaded")
		}
	}
	status, _, message, _ := BillingErrorDetails(billing.ErrAPIKeyRateLimit5hExceeded)
	require.Equal(t, 429, status)
	require.Equal(t, "The API key five-hour limit has been reached", message)
}
