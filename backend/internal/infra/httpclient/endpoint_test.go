package httpclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildOpenAIChatCompletionsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		want string
	}{
		// 已是 /chat/completions：原样返回
		{"already chat/completions", "https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		// 以 /v1 结尾：追加 /chat/completions
		{"bare /v1", "https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		// 其他情况：追加 /v1/chat/completions
		{"bare domain", "https://api.openai.com", "https://api.openai.com/v1/chat/completions"},
		{"domain with trailing slash", "https://api.openai.com/", "https://api.openai.com/v1/chat/completions"},
		// 第三方上游常见形式
		{"third-party bare domain", "https://api.deepseek.com", "https://api.deepseek.com/v1/chat/completions"},
		{"third-party with path prefix", "https://api.gptgod.online/api", "https://api.gptgod.online/api/v1/chat/completions"},
		{"third-party versioned path", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		// 带空白字符
		{"whitespace trimmed", "  https://api.openai.com/v1  ", "https://api.openai.com/v1/chat/completions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BuildOpenAIEndpointURL(tt.base, "/v1/chat/completions")
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildOpenAIResponsesInputTokensURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "root base",
			base: "https://api.openai.com",
			want: "https://api.openai.com/v1/responses/input_tokens",
		},
		{
			name: "versioned base",
			base: "https://compat.example/v4",
			want: "https://compat.example/v4/responses/input_tokens",
		},
		{
			name: "responses endpoint base",
			base: "https://compat.example/v1/responses",
			want: "https://compat.example/v1/responses/input_tokens",
		},
		{
			name: "input tokens endpoint base",
			base: "https://compat.example/v1/responses/input_tokens",
			want: "https://compat.example/v1/responses/input_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildOpenAIResponsesInputTokensURL(tt.base); got != tt.want {
				t.Fatalf("buildOpenAIResponsesInputTokensURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestBuildOpenAIEndpointURLPreservesURLComponents(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		endpoint string
		want     string
	}{
		{name: "root", base: "https://upstream.example", endpoint: "/v1/models", want: "https://upstream.example/v1/models"},
		{name: "v1", base: "https://upstream.example/v1", endpoint: "/v1/responses", want: "https://upstream.example/v1/responses"},
		{name: "prefix", base: "https://upstream.example/openai", endpoint: "/v1/chat/completions", want: "https://upstream.example/openai/v1/chat/completions"},
		{name: "version", base: "https://upstream.example/openai/v2", endpoint: "/v1/embeddings", want: "https://upstream.example/openai/v2/embeddings"},
		{name: "query", base: "https://upstream.example/v1?redirect=/", endpoint: "/v1/embeddings", want: "https://upstream.example/v1/embeddings?redirect=/"},
		{name: "fragment is removed", base: "https://upstream.example/v1#stale", endpoint: "/v1/alpha/search", want: "https://upstream.example/v1/alpha/search"},
		{name: "ipv6", base: "http://[2001:db8::1]:8080/v1?tenant=a#stale", endpoint: "/v1/responses/input_tokens", want: "http://[2001:db8::1]:8080/v1/responses/input_tokens?tenant=a"},
		{name: "already complete", base: "https://upstream.example/v1/images/generations?tenant=a", endpoint: "/v1/images/generations", want: "https://upstream.example/v1/images/generations?tenant=a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, BuildOpenAIEndpointURL(tt.base, tt.endpoint))
		})
	}
}

// TestBuildOpenAIResponsesURL_ProbeURL 检查不同 OpenAI base URL 的探测端点均为 /v1/responses。
func TestBuildOpenAIResponsesURL_ProbeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		want string
	}{
		{"bare domain", "https://api.openai.com", "https://api.openai.com/v1/responses"},
		{"domain trailing slash", "https://api.openai.com/", "https://api.openai.com/v1/responses"},
		{"bare /v1", "https://api.openai.com/v1", "https://api.openai.com/v1/responses"},
		{"already /responses", "https://api.openai.com/v1/responses", "https://api.openai.com/v1/responses"},
		{"third-party bare domain", "https://api.deepseek.com", "https://api.deepseek.com/v1/responses"},
		{"third-party versioned path", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/responses"},
		{"only domain, no scheme", "api.gptgod.online", "api.gptgod.online/v1/responses"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BuildOpenAIEndpointURL(tt.base, "/v1/responses")
			require.Equal(t, tt.want, got)
		})
	}
}
