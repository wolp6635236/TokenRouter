package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	upstreamopenai "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

func TestOpenAIResponsesRequestPathSuffix(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "exact v1 responses", path: "/v1/responses", want: ""},
		{name: "compact v1 responses", path: "/v1/responses/compact", want: "/compact"},
		{name: "compact alias responses", path: "/responses/compact/", want: "/compact"},
		{name: "nested suffix", path: "/openai/v1/responses/compact/detail", want: "/compact/detail"},
		{name: "unrelated path", path: "/v1/chat/completions", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)
			require.Equal(t, tt.want, OpenAIResponsesRequestPathSuffix(c))
		})
	}
}

// TestOpenAIResponsesRequestPathSuffixRejectsNonConformingSubpaths 锁定不变式：
// /responses/*subpath 的子路径不得改变上游请求的路径结构；不合规时既不参与拼接，
// 也不会被误判成 compact 请求。
func TestOpenAIResponsesRequestPathSuffixRejectsNonConformingSubpaths(t *testing.T) {
	nonConformingPaths := []string{
		"/v1/responses/../../x/y",
		"/v1/responses/..%2f..%2fx/y",
		"/v1/responses/%2e%2e/%2e%2e/x",
		"/responses/%2e%2e%2fx",
		"/backend-api/codex/responses/../../../x",
		`/v1/responses/..\..\x`,
		"/v1/responses/%3fa=b",
		"/v1/responses/x%23frag",
		"/v1/responses//double",
	}
	for _, path := range nonConformingPaths {
		t.Run(path, func(t *testing.T) {
			c := newResponsesSuffixTestContext(t, path)

			require.False(t, IsForwardableOpenAIResponsesRequestPath(c),
				"path %q must be rejected at the gateway edge", path)
			require.Empty(t, OpenAIResponsesRequestPathSuffix(c),
				"path %q must never contribute an upstream path suffix", path)
			require.Equal(t, "https://chatgpt.com/backend-api/codex/responses",
				upstreamopenai.AppendResponsesPathSuffix("https://chatgpt.com/backend-api/codex/responses", OpenAIResponsesRequestPathSuffix(c)))
			require.False(t, IsOpenAIResponsesCompactPath(c))
		})
	}

	// 合法子路径原样转发。
	for path, want := range map[string]string{
		"/v1/responses":                        "",
		"/v1/responses/input_tokens":           "/input_tokens",
		"/v1/responses/compact":                "/compact",
		"/responses/compact/":                  "/compact",
		"/backend-api/codex/responses/compact": "/compact",
	} {
		t.Run("forwardable_"+path, func(t *testing.T) {
			c := newResponsesSuffixTestContext(t, path)
			require.True(t, IsForwardableOpenAIResponsesRequestPath(c))
			require.Equal(t, want, OpenAIResponsesRequestPathSuffix(c))
		})
	}
}

func TestIsOpenAIResponsesInputTokensRequestPath(t *testing.T) {
	for _, path := range []string{"/v1/responses/input_tokens", "/responses/input_tokens", "/backend-api/codex/responses/input_tokens"} {
		c := newResponsesSuffixTestContext(t, path)
		require.True(t, IsOpenAIResponsesInputTokensRequestPath(c), "path=%s", path)
	}
	c := newResponsesSuffixTestContext(t, "/v1/responses/compact")
	require.False(t, IsOpenAIResponsesInputTokensRequestPath(c))
}

func newResponsesSuffixTestContext(t *testing.T, path string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return c
}
