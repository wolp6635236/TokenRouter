package provider_test

import (
	"encoding/json"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGetPoolModeRetryStatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		provider *providercore.Record
		expected []int
	}{
		{
			name:     "nil_provider_returns_nil",
			provider: nil,
			expected: nil,
		},
		{
			name: "nil_credentials_returns_nil",
			provider: &providercore.Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
			},
			expected: nil,
		},
		{
			name: "missing_key_returns_nil",
			provider: &providercore.Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformOpenAI,
				Credentials: map[string]any{"pool_mode": true},
			},
			expected: nil,
		},
		{
			name: "empty_slice_is_preserved",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{},
				},
			},
			expected: []int{},
		},
		{
			name: "float64_values_from_json_are_normalized",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(429), float64(401), float64(403)},
				},
			},
			expected: []int{401, 403, 429},
		},
		{
			name: "json_number_values_supported",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{json.Number("502"), json.Number("503")},
				},
			},
			expected: []int{502, 503},
		},
		{
			name: "string_values_supported",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{"520", "529"},
				},
			},
			expected: []int{520, 529},
		},
		{
			name: "duplicates_are_deduped",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(429), float64(429), float64(401)},
				},
			},
			expected: []int{401, 429},
		},
		{
			name: "out_of_range_values_dropped",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(99), float64(600), float64(429)},
				},
			},
			expected: []int{429},
		},
		{
			name: "invalid_string_dropped",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{"oops", float64(429)},
				},
			},
			expected: []int{429},
		},
		{
			name: "non_array_value_returns_nil",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": "not-an-array",
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.provider.GetPoolModeRetryStatusCodes())
		})
	}
}

func TestIsPoolModeRetryableStatus_Provider(t *testing.T) {
	tests := []struct {
		name       string
		provider   *providercore.Record
		statusCode int
		expected   bool
	}{
		{
			name:       "nil_provider_falls_back_to_default_401",
			provider:   nil,
			statusCode: 401,
			expected:   true,
		},
		{
			name:       "nil_provider_falls_back_to_default_500",
			provider:   nil,
			statusCode: 500,
			expected:   false,
		},
		{
			name: "unconfigured_uses_default_403",
			provider: &providercore.Record{
				Credentials: map[string]any{"pool_mode": true},
			},
			statusCode: 403,
			expected:   true,
		},
		{
			name: "unconfigured_uses_default_502_false",
			provider: &providercore.Record{
				Credentials: map[string]any{"pool_mode": true},
			},
			statusCode: 502,
			expected:   false,
		},
		{
			name: "configured_list_overrides_default_401_dropped",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(502), float64(503)},
				},
			},
			statusCode: 401,
			expected:   false,
		},
		{
			name: "configured_list_overrides_default_502_added",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(502), float64(503)},
				},
			},
			statusCode: 502,
			expected:   true,
		},
		{
			name: "empty_list_disables_all_default_codes",
			provider: &providercore.Record{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{},
				},
			},
			statusCode: 429,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.provider.IsPoolModeRetryableStatus(tt.statusCode))
		})
	}
}
