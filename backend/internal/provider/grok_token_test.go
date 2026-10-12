package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
