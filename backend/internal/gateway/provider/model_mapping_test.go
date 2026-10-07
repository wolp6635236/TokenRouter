package provider

import (
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestResolveOpenAIForwardModel(t *testing.T) {
	tests := []struct {
		name                        string
		provider                    *providercore.Record
		requestedModel              string
		messagesDispatchMappedModel string
		expectedModel               string
	}{
		{
			name:                        "uses messages dispatch model for known claude family",
			provider:                    &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel:              "claude-opus-4-6",
			messagesDispatchMappedModel: "gpt-4o-mini",
			expectedModel:               "gpt-4o-mini",
		},
		{
			name:                        "uses exact messages dispatch model for unknown claude family",
			provider:                    &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel:              "claude-fable-5",
			messagesDispatchMappedModel: " gpt-5.6-sol ",
			expectedModel:               "gpt-5.6-sol",
		},
		{
			name:                        "nil provider uses messages dispatch model",
			requestedModel:              "claude-fable-5",
			messagesDispatchMappedModel: "gpt-5.6-sol",
			expectedModel:               "gpt-5.6-sol",
		},
		{
			name:           "nil provider without messages dispatch keeps requested model",
			requestedModel: "claude-fable-5",
			expectedModel:  "claude-fable-5",
		},
		{
			name:           "ordinary unknown gpt model has no messages dispatch fallback",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "gpt6",
			expectedModel:  "gpt6",
		},
		{
			name: "provider exact mapping runs after messages dispatch model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.6-sol": "gpt-5.5",
				},
			}},

			requestedModel:              "claude-fable-5",
			messagesDispatchMappedModel: "gpt-5.6-sol",
			expectedModel:               "gpt-5.5",
		},
		{
			name: "provider wildcard mapping runs after messages dispatch model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-*": "gpt-5.4",
				},
			}},

			requestedModel:              "claude-fable-5",
			messagesDispatchMappedModel: "gpt-5.6-sol",
			expectedModel:               "gpt-5.4",
		},
		{
			name: "provider passthrough mapping runs after messages dispatch model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.6-sol": "gpt-5.6-sol",
				},
			}},

			requestedModel:              "claude-fable-5",
			messagesDispatchMappedModel: "gpt-5.6-sol",
			expectedModel:               "gpt-5.6-sol",
		},
		{
			name:           "ordinary codex spark request keeps requested model",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "gpt-5.3-codex-spark",
			expectedModel:  "gpt-5.3-codex-spark",
		},
		{
			name:           "ordinary gpt-5.5 request keeps requested model",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "gpt-5.5",
			expectedModel:  "gpt-5.5",
		},
		{
			name:           "ordinary gpt-5.5-pro request keeps requested model",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "gpt-5.5-pro",
			expectedModel:  "gpt-5.5-pro",
		},
		{
			name:           "ordinary compact-spelled gpt5.5 request keeps requested model",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "gpt5.5",
			expectedModel:  "gpt5.5",
		},
		{
			name:           "ordinary namespaced gpt-5.5 request keeps requested model",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "openai/gpt-5.5",
			expectedModel:  "openai/gpt-5.5",
		},
		{
			name:           "ordinary compact gpt-5.5 request keeps requested model",
			provider:       &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel: "gpt-5.5-openai-compact",
			expectedModel:  "gpt-5.5-openai-compact",
		},
		{
			name:                        "whitespace-only messages dispatch model is ignored",
			provider:                    &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			requestedModel:              "gpt-5.5",
			messagesDispatchMappedModel: "  ",
			expectedModel:               "gpt-5.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (ModelPolicy{Record: tt.provider}).ForwardModel(tt.requestedModel, tt.messagesDispatchMappedModel); got != tt.expectedModel {
				t.Fatalf("resolveOpenAIForwardModel(...) = %q, want %q", got, tt.expectedModel)
			}
		})
	}
}

func TestResolveOpenAICompactForwardModel(t *testing.T) {
	tests := []struct {
		name          string
		provider      *providercore.Record
		model         string
		expectedModel string
	}{
		{
			name:          "nil provider keeps original model",
			provider:      nil,
			model:         "gpt-5.4",
			expectedModel: "gpt-5.4",
		},
		{
			name:          "missing compact mapping keeps original model",
			provider:      &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}},
			model:         "gpt-5.4",
			expectedModel: "gpt-5.4",
		},
		{
			name: "exact compact mapping overrides model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
				"compact_model_mapping": map[string]any{
					"gpt-5.4": "gpt-5.4-openai-compact",
				},
			}},

			model:         "gpt-5.4",
			expectedModel: "gpt-5.4-openai-compact",
		},
		{
			name: "wildcard compact mapping overrides model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
				"compact_model_mapping": map[string]any{
					"gpt-5.*": "gpt-5-openai-compact",
				},
			}},

			model:         "gpt-5.4",
			expectedModel: "gpt-5-openai-compact",
		},
		{
			name: "passthrough compact mapping remains unchanged",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
				"compact_model_mapping": map[string]any{
					"gpt-5.4": "gpt-5.4",
				},
			}},

			model:         "gpt-5.4",
			expectedModel: "gpt-5.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providercore.ResolveCompactForwardModel(tt.provider, tt.model); got != tt.expectedModel {
				t.Fatalf("resolveOpenAICompactForwardModel(...) = %q, want %q", got, tt.expectedModel)
			}
		})
	}
}

func TestResolveOpenAIForwardMappedModels_CompactMappingPrecedence(t *testing.T) {
	conflictingMappings := map[string]any{
		"model_mapping":         map[string]any{"gpt-5.5": "gpt-5.4"},
		"compact_model_mapping": map[string]any{"gpt-5.5": "gpt-5.5-openai-compact"},
	}
	mappedOnlyCompact := map[string]any{
		"model_mapping":         map[string]any{"gpt-5.5": "gpt-5.4"},
		"compact_model_mapping": map[string]any{"gpt-5.4": "gpt-5.4-openai-compact"},
	}
	tests := []struct {
		name           string
		provider       *providercore.Record
		requireCompact bool
		wantBilling    string
		wantUpstream   string
	}{
		{
			name: "compact uses client-visible model before ordinary mapping",
			provider: &providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
				Credentials: conflictingMappings,
			},
			requireCompact: true,
			wantBilling:    "gpt-5.4",
			wantUpstream:   "gpt-5.5-openai-compact",
		},
		{
			name: "non-compact uses ordinary mapping",
			provider: &providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
				Credentials: conflictingMappings,
			},
			wantBilling:  "gpt-5.4",
			wantUpstream: "gpt-5.4",
		},
		{
			name: "compact falls back to ordinary mapped model",
			provider: &providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
				Credentials: mappedOnlyCompact,
			},
			requireCompact: true,
			wantBilling:    "gpt-5.4",
			wantUpstream:   "gpt-5.4-openai-compact",
		},
		{
			name: "passthrough preserves explicit ordinary mapping",
			provider: &providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
				Credentials: conflictingMappings, Extra: map[string]any{"openai_passthrough": true},
			},
			requireCompact: true,
			wantBilling:    "gpt-5.4",
			wantUpstream:   "gpt-5.5-openai-compact",
		},
		{
			name: "raw chat fallback never applies compact mapping",
			provider: &providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey,
				Credentials: conflictingMappings, Extra: map[string]any{"openai_text_route_mode": "force_chat_completions"},
			},
			requireCompact: true,
			wantBilling:    "gpt-5.4",
			wantUpstream:   "gpt-5.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			billing, upstream := (ModelPolicy{Record: tt.provider}).ForwardMappedModels("gpt-5.5", tt.requireCompact)
			if billing != tt.wantBilling {
				t.Fatalf("billing model = %q, want %q", billing, tt.wantBilling)
			}
			if upstream != tt.wantUpstream {
				t.Fatalf("upstream model = %q, want %q", upstream, tt.wantUpstream)
			}
			if scheduler := (ModelPolicy{Record: tt.provider}).OpenAIUpstream("gpt-5.5", tt.requireCompact); scheduler != upstream {
				t.Fatalf("scheduler model %q disagrees with Forward model %q", scheduler, upstream)
			}
		})
	}
}

func TestCanonicalOpenAIProviderSchedulingModelMatchesForwardSemantics(t *testing.T) {
	tests := []struct {
		name     string
		provider *providercore.Record
		model    string
		want     string
	}{
		{
			name:     "OpenAI OAuth preserves bare GPT-5.6 identity",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "gpt-5.6",
			want:     "gpt-5.6",
		},
		{
			name: "OpenAI passthrough preserves explicit ordinary provider mapping",
			provider: &providercore.Record{
				LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
				Credentials: map[string]any{"model_mapping": map[string]any{"public": "private", "private": "must-not-map-again"}},
				Extra:       map[string]any{"openai_passthrough": true},
			},
			model: "public",
			want:  "private",
		},
		{
			name:     "Grok OAuth does not inherit OpenAI Codex aliases",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth},
			model:    "gpt-5.6",
			want:     "gpt-5.6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (ModelPolicy{Record: tt.provider}).CanonicalSchedulingModel(tt.model); got != tt.want {
				t.Fatalf("canonical scheduling model = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOpenAIErrorSchedulingModelPrefersActualUpstreamModel(t *testing.T) {
	if got := ErrorSchedulingModel("gpt-5.4", "gpt-5.5-openai-compact"); got != "gpt-5.5-openai-compact" {
		t.Fatalf("error scheduling model = %q, want compact upstream model", got)
	}
	if got := ErrorSchedulingModel("gpt-5.4", ""); got != "gpt-5.4" {
		t.Fatalf("empty upstream fallback = %q, want billing model", got)
	}
}

// TestOpenAIUpstreamPreservesModelID 检查最终请求使用完整模型名称。
func TestOpenAIUpstreamPreservesModelID(t *testing.T) {
	cases := map[string]string{
		"gpt-5.3-codex-spark":       "gpt-5.3-codex-spark",
		"gpt-5.3-codex-spark-high":  "gpt-5.3-codex-spark-high",
		"gpt-5.3-codex-spark-xhigh": "gpt-5.3-codex-spark-xhigh",
		"gpt-5.3":                   "gpt-5.3",
		"gpt-image-2":               "gpt-image-2",
		"gpt-5.4-nano":              "gpt-5.4-nano",
		"gpt-5.4-nano-high":         "gpt-5.4-nano-high",
		"gpt6":                      "gpt6",
		"claude-opus-4-6":           "claude-opus-4-6",
	}

	for input, expected := range cases {
		if got := (ModelPolicy{}).OpenAIUpstream(input, false); got != expected {
			t.Fatalf("OpenAIUpstream(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestNormalizeOpenAIModelForUpstream(t *testing.T) {
	tests := []struct {
		name     string
		provider *providercore.Record
		model    string
		want     string
	}{
		{
			name:     "nil provider only trims whitespace",
			provider: nil,
			model:    " gpt-5.6 ",
			want:     "gpt-5.6",
		},
		{
			name:     "oauth preserves bare GPT-5.6",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "gpt-5.6",
			want:     "gpt-5.6",
		},
		{
			name:     "oauth preserves unregistered provider-prefixed GPT-5.6",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "openai/gpt-5.6",
			want:     "openai/gpt-5.6",
		},
		{
			name:     "oauth preserves unknown non codex model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "gemini-3-flash-preview",
			want:     "gemini-3-flash-preview",
		},
		{
			name:     "oauth preserves invalid gpt model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "gpt6",
			want:     "gpt6",
		},
		{
			name:     "oauth normalizes known codex alias",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "gpt-5.4-high",
			want:     "gpt-5.4-high",
		},
		{
			name:     "oauth preserves GPT-5.5 Pro model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "openai/gpt-5.5-pro",
			want:     "openai/gpt-5.5-pro",
		},
		{
			name:     "oauth preserves codex auto review model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
			model:    "codex-auto-review",
			want:     "codex-auto-review",
		},
		{
			name:     "apikey preserves official bare GPT-5.6 alias",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
			model:    "gpt-5.6",
			want:     "gpt-5.6",
		},
		{
			name:     "apikey preserves custom compatible model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
			model:    "gemini-3-flash-preview",
			want:     "gemini-3-flash-preview",
		},
		{
			name:     "apikey preserves official non codex model",
			provider: &providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
			model:    "gpt-4.1",
			want:     "gpt-4.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (ModelPolicy{Record: tt.provider}).NormalizeOpenAI(tt.model); got != tt.want {
				t.Fatalf("normalizeOpenAIModelForUpstream(...) = %q, want %q", got, tt.want)
			}
		})
	}
}
