package provider_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/stretchr/testify/require"
)

func TestGetBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		provider providercore.Record
		expected string
	}{
		{
			name: "non-apikey type returns empty",
			provider: providercore.Record{
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAnthropic,
			},
			expected: "",
		},
		{
			name: "apikey without base_url returns default anthropic",
			provider: providercore.Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformAnthropic,
				Credentials: map[string]any{},
			},
			expected: "https://api.anthropic.com",
		},
		{
			name: "apikey with custom base_url",
			provider: providercore.Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformAnthropic,
				Credentials: map[string]any{"base_url": "https://custom.example.com"},
			},
			expected: "https://custom.example.com",
		},
		{
			name: "antigravity non-apikey returns empty",
			provider: providercore.Record{
				Type:        capability.ProviderTypeOAuth,
				Platform:    capability.PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com"},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.provider.GetBaseURL()
			if result != tt.expected {
				t.Errorf("GetBaseURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetGeminiBaseURL(t *testing.T) {
	const defaultGeminiURL = "https://generativelanguage.googleapis.com"

	tests := []struct {
		name     string
		provider providercore.Record
		expected string
	}{
		{
			name: "apikey without base_url returns default",
			provider: providercore.Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformGemini,
				Credentials: map[string]any{},
			},
			expected: defaultGeminiURL,
		},
		{
			name: "apikey with custom base_url",
			provider: providercore.Record{
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformGemini,
				Credentials: map[string]any{"base_url": "https://custom-gemini.example.com"},
			},
			expected: "https://custom-gemini.example.com",
		},
		{
			name: "antigravity oauth does NOT append /antigravity",
			provider: providercore.Record{
				Type:        capability.ProviderTypeOAuth,
				Platform:    capability.PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com"},
			},
			expected: "https://upstream.example.com",
		},
		{
			name: "oauth without base_url returns default",
			provider: providercore.Record{
				Type:        capability.ProviderTypeOAuth,
				Platform:    capability.PlatformAntigravity,
				Credentials: map[string]any{},
			},
			expected: defaultGeminiURL,
		},
		{
			name: "nil credentials returns default",
			provider: providercore.Record{
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformGemini,
			},
			expected: defaultGeminiURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.provider.GetGeminiBaseURL(defaultGeminiURL)
			if result != tt.expected {
				t.Errorf("GetGeminiBaseURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestHasGeminiThirdPartyBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		provider providercore.Record
		expected bool
	}{
		{
			name: "custom Gemini-compatible endpoint",
			provider: providercore.Record{
				Platform: capability.PlatformGemini,
				Type:     capability.ProviderTypeAPIKey,
				Credentials: map[string]any{
					providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
					"base_url": "https://provider.example.test/v1beta",
				},
			},
			expected: true,
		},
		{
			name: "missing base URL",
			provider: providercore.Record{
				Platform: capability.PlatformGemini,
				Type:     capability.ProviderTypeAPIKey,
				Credentials: map[string]any{
					providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
				},
			},
			expected: false,
		},
		{
			name: "official Gemini endpoint",
			provider: providercore.Record{
				Platform: capability.PlatformGemini,
				Type:     capability.ProviderTypeAPIKey,
				Credentials: map[string]any{
					providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
					"base_url": "https://generativelanguage.googleapis.com/v1beta",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.provider.HasGeminiThirdPartyBaseURL())
		})
	}
}

func TestBuildProviderForCreateRequiresCustomGeminiThirdPartyBaseURL(t *testing.T) {
	invalidInput := &providercore.CreateProviderInput{
		Name:     "third-party Gemini",
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
			"base_url": "https://generativelanguage.googleapis.com",
		},
	}

	_, err := providercore.BuildProviderForCreate(invalidInput, nil, providercore.CreationOptions{Now: time.Now, LoadLocation: time.LoadLocation, NewSeed: uuid.NewString})
	require.Error(t, err)
	require.Equal(t, "GEMINI_THIRD_PARTY_BASE_URL_REQUIRED", apperror.Reason(err))

	validInput := &providercore.CreateProviderInput{
		Name:     "third-party Gemini",
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
			"base_url": "https://provider.example.test",
		},
	}
	provider, err := providercore.BuildProviderForCreate(validInput, nil, providercore.CreationOptions{Now: time.Now, LoadLocation: time.LoadLocation, NewSeed: uuid.NewString})
	require.NoError(t, err)
	require.NotNil(t, provider)
}
