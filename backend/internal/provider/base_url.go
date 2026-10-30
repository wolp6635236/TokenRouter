package provider

import (
	"strings"
)

// base_url.go resolves OpenAI-compatible Chat roots from stored slots, legacy
// credentials, and platform endpoint declarations. Endpoint-slot platforms may
// configure Chat independently of their legacy base. Unified or adaptive
// lookup reads that slot first, then compatible credential types can use the
// saved base_url before the access mode supplies its default.
//
// OpenAI retains its dedicated official default and other executors retain
// their own lookup functions. Slot presence, provider type, and mode are
// evaluated separately so imported records keep their established endpoint
// behavior. This resolver reads configuration without changing it and does not
// validate network destinations; each outbound request applies the egress
// policy to the selected address.

// OpenAIBaseURL resolves stored Chat roots before declared platform defaults.
// Unified or adaptive endpoint declarations prefer their Chat slot; legacy
// API-key and upstream records then use base_url. OpenAI keeps its ordinary
// official default, and unsupported platforms return empty for their own resolver.
//
// Slot selection and record type are evaluated separately so imported records
// retain their established read behavior. An empty slot can still inherit the
// legacy Chat root, while an absent saved root selects the mode's declared
// endpoint. This function returns trimmed configuration without validating its
// destination; outbound execution applies that policy before connecting.
func (r *Record) OpenAIBaseURL(adaptive bool) string {
	if !r.IsOpenAI() && !r.HasProtocolEndpointDefaults() {
		return ""
	}
	if _, unified := r.Credentials[UpstreamProtocolsKey]; r.HasProtocolEndpointDefaults() && (unified || adaptive) {
		if baseURL := r.protocolSlotBaseURL(APIProtocolChatCompletions); baseURL != "" {
			return baseURL
		}
	}
	if r.Type == ProviderTypeAPIKey || r.Type == ProviderTypeUpstream {
		if baseURL := strings.TrimSpace(r.GetCredential("base_url")); baseURL != "" {
			return baseURL
		}
	}
	if r.HasProtocolEndpointDefaults() {
		return r.DefaultTextAPIKeyProtocolBaseURL(APIProtocolChatCompletions)
	}
	return "https://api.openai.com"
}

// protocolSlotBaseURL returns the trimmed address saved in one api_base_urls
// slot. Missing maps, missing slots, non-string values, and blank strings all
// return empty, letting callers fall back to their platform defaults.
func (r *Record) protocolSlotBaseURL(slot string) string {
	slots, _ := r.Credentials["api_base_urls"].(map[string]any)
	value, _ := slots[slot].(string)
	return strings.TrimSpace(value)
}
