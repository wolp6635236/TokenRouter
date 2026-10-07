package provider

import (
	"encoding/json"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGetPoolModeRetryCount(t *testing.T) {
	tests := []struct {
		name     string
		provider *Record
		expected int
	}{
		{
			name: "default_when_not_pool_mode",
			provider: &Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformOpenAI,
				Credentials: map[string]any{},
			},
			expected: DefaultPoolModeRetryCount,
		},
		{
			name: "default_when_missing_retry_count",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode": true,
				},
			},
			expected: DefaultPoolModeRetryCount,
		},
		{
			name: "supports_float64_from_json_credentials",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": float64(5),
				},
			},
			expected: 5,
		},
		{
			name: "supports_json_number",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": json.Number("4"),
				},
			},
			expected: 4,
		},
		{
			name: "supports_string_value",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": "2",
				},
			},
			expected: 2,
		},
		{
			name: "negative_value_is_clamped_to_zero",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": -1,
				},
			},
			expected: 0,
		},
		{
			name: "oversized_value_is_clamped_to_max",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": 99,
				},
			},
			expected: MaxPoolModeRetryCount,
		},
		{
			name: "invalid_value_falls_back_to_default",
			provider: &Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": "oops",
				},
			},
			expected: DefaultPoolModeRetryCount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.provider.GetPoolModeRetryCount())
		})
	}
}
