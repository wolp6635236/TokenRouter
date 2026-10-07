package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected string
	}{
		{
			name: "with_project_id",
			provider: &Record{
				ID: 100,
				Credentials: map[string]any{
					"project_id": "my-project-123",
				},
			},
			expected: "gemini:my-project-123",
		},
		{
			name: "project_id_with_whitespace",
			provider: &Record{
				ID: 101,
				Credentials: map[string]any{
					"project_id": "  project-with-spaces  ",
				},
			},
			expected: "gemini:project-with-spaces",
		},
		{
			name: "empty_project_id_fallback_to_provider_id",
			provider: &Record{
				ID: 102,
				Credentials: map[string]any{
					"project_id": "",
				},
			},
			expected: "gemini:provider:102",
		},
		{
			name: "whitespace_only_project_id_fallback_to_provider_id",
			provider: &Record{
				ID: 103,
				Credentials: map[string]any{
					"project_id": "   ",
				},
			},
			expected: "gemini:provider:103",
		},
		{
			name: "no_project_id_key_fallback_to_provider_id",
			provider: &Record{
				ID:          104,
				Credentials: map[string]any{},
			},
			expected: "gemini:provider:104",
		},
		{
			name: "nil_credentials_fallback_to_provider_id",
			provider: &Record{
				ID:          105,
				Credentials: nil,
			},
			expected: "gemini:provider:105",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GeminiOAuthTokenCacheKey(tt.provider)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAntigravityTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected string
	}{
		{
			name: "with_project_id",
			provider: &Record{
				ID: 200,
				Credentials: map[string]any{
					"project_id": "ag-project-456",
				},
			},
			expected: "ag:ag-project-456",
		},
		{
			name: "project_id_with_whitespace",
			provider: &Record{
				ID: 201,
				Credentials: map[string]any{
					"project_id": "  ag-project-spaces  ",
				},
			},
			expected: "ag:ag-project-spaces",
		},
		{
			name: "empty_project_id_fallback_to_provider_id",
			provider: &Record{
				ID: 202,
				Credentials: map[string]any{
					"project_id": "",
				},
			},
			expected: "ag:provider:202",
		},
		{
			name: "whitespace_only_project_id_fallback_to_provider_id",
			provider: &Record{
				ID: 203,
				Credentials: map[string]any{
					"project_id": "   ",
				},
			},
			expected: "ag:provider:203",
		},
		{
			name: "no_project_id_key_fallback_to_provider_id",
			provider: &Record{
				ID:          204,
				Credentials: map[string]any{},
			},
			expected: "ag:provider:204",
		},
		{
			name: "nil_credentials_fallback_to_provider_id",
			provider: &Record{
				ID:          205,
				Credentials: nil,
			},
			expected: "ag:provider:205",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AntigravityTokenCacheKey(tt.provider)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestOpenAITokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected string
	}{
		{
			name: "basic_provider",
			provider: &Record{
				ID: 300,
			},
			expected: "openai:provider:300",
		},
		{
			name: "provider_with_credentials",
			provider: &Record{
				ID: 301,
				Credentials: map[string]any{
					"access_token": "test-token",
				},
			},
			expected: "openai:provider:301",
		},
		{
			name: "provider_id_zero",
			provider: &Record{
				ID: 0,
			},
			expected: "openai:provider:0",
		},
		{
			name: "large_provider_id",
			provider: &Record{
				ID: 9999999999,
			},
			expected: "openai:provider:9999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OpenAITokenCacheKey(tt.provider)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGrokTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected string
	}{
		{
			name: "basic_provider",
			provider: &Record{
				ID: 350,
			},
			expected: "grok:provider:350",
		},
		{
			name: "provider_with_email_uses_provider_id",
			provider: &Record{
				ID: 351,
				Credentials: map[string]any{
					"email": "same-user@example.com",
				},
			},
			expected: "grok:provider:351",
		},
		{
			name: "provider_id_zero",
			provider: &Record{
				ID: 0,
			},
			expected: "grok:provider:0",
		},
		{
			name:     "nil_provider",
			provider: nil,
			expected: "grok:provider:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GrokTokenCacheKey(tt.provider)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGrokTokenCacheKeySeparatesProvidersWithSameEmail(t *testing.T) {
	first := &Record{
		ID: 351,
		Credentials: map[string]any{
			"email": "same-user@example.com",
		},
	}
	second := &Record{
		ID: 352,
		Credentials: map[string]any{
			"email": "same-user@example.com",
		},
	}

	require.NotEqual(t, GrokTokenCacheKey(first), GrokTokenCacheKey(second))
}

func TestClaudeTokenCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected string
	}{
		{
			name: "basic_provider",
			provider: &Record{
				ID: 400,
			},
			expected: "claude:provider:400",
		},
		{
			name: "provider_with_credentials",
			provider: &Record{
				ID: 401,
				Credentials: map[string]any{
					"access_token": "claude-token",
				},
			},
			expected: "claude:provider:401",
		},
		{
			name: "provider_id_zero",
			provider: &Record{
				ID: 0,
			},
			expected: "claude:provider:0",
		},
		{
			name: "large_provider_id",
			provider: &Record{
				ID: 9999999999,
			},
			expected: "claude:provider:9999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClaudeTokenCacheKey(tt.provider)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestCacheKeyUniqueness(t *testing.T) {
	// 不同平台使用各自的缓存键前缀。
	provider := &Record{ID: 123}

	openaiKey := OpenAITokenCacheKey(provider)
	claudeKey := ClaudeTokenCacheKey(provider)

	require.NotEqual(t, openaiKey, claudeKey, "OpenAI and Claude cache keys should be different")
	require.Contains(t, openaiKey, "openai:")
	require.Contains(t, claudeKey, "claude:")
}
