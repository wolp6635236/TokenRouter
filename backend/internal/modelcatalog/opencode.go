package modelcatalog

import (
	"encoding/json"
	"strings"
	"sync"
)

// opencode.go indexes the encodings and canonical identities of OpenCode model
// offerings. Service offerings retain public attribute references separately
// from prices because an intermediary's encoding determines its request
// endpoint independently of the author metadata used for model attributes.
// Each mode retains exact model IDs; the same model can use different encodings
// on payg and Go. Missing canonical declarations use an exact, unique public
// model ID, and ambiguous author matches leave the identity unknown.
//
// The embedded publication provides deterministic startup defaults. A live
// model synchronization can obtain declarations from another catalog body and
// save them with the provider, allowing its administrator to review route
// changes before they affect inference. Returned protocol maps are detached
// from the retained publication so request-specific overrides cannot mutate
// defaults. Canonical identities are read from the live catalog view during
// attribute resolution, so a refreshed publication replaces bundled ones.

// embeddedOpenCodeOfferings parses the bundled publication once and retains
// immutable mode tables; OpenCodeProtocols returns independent copies.
var embeddedOpenCodeOfferings = sync.OnceValue(func() map[string]map[string]OpenCodeOffering {
	body, err := Offline()
	if err != nil {
		// An unreadable bundled catalog provides no verified routes. Unknown models
		// then require administrator overrides instead of choosing a guessed endpoint.
		return nil
	}
	return ParseOpenCodeOfferings(body)
})

// OpenCodeOffering retains one service model's encoding and public metadata identity.
// The map key remains the outbound model name; CanonicalModelID selects author
// attributes independently of service prices and administrator protocol overrides.
type OpenCodeOffering struct {
	Protocol         string // declared upstream wire encoding
	CanonicalModelID string // public author metadata identity
}

// OpenCodeProtocols returns copied protocol defaults for one bundled service
// mode. Unknown modes return an empty map. Defaults come from the bundled
// models.dev publication and can be replaced by saved provider declarations.
func OpenCodeProtocols(mode string) map[string]string {
	return offeringProtocols(embeddedOpenCodeOfferings()[mode])
}

// OpenCodeProtocols copies one service mode's protocol suggestions from this
// publication. Unknown modes return an empty map; the catalog keeps its own tables.
func (c *Catalog) OpenCodeProtocols(mode string) map[string]string {
	return offeringProtocols(c.OpenCodeOfferings[mode])
}

// offeringProtocols copies the declared protocol of each offering into a new
// map. Offerings with an unrecognized SDK are omitted, so neither defaults nor
// synchronization suggestions can invent an execution protocol for them.
func offeringProtocols(offerings map[string]OpenCodeOffering) map[string]string {
	result := make(map[string]string, len(offerings))
	for id, offering := range offerings {
		if offering.Protocol != "" {
			result[id] = offering.Protocol
		}
	}
	return result
}

// ParseOpenCodeOfferings extracts service model encodings and canonical author
// identities from one catalog publication. Invalid JSON returns nil; model SDK
// declarations override the provider SDK, and decision entries select SystemOne.
//
// Canonical identities survive unknown encodings because metadata and execution
// eligibility answer separate questions. A manual protocol override can make
// such an offering executable while its author metadata remains available.
// Missing canonical declarations use an exact model component from a unique
// public author entry. Explicit declarations take precedence, and ambiguous or
// absent public matches stay unknown. Prices stay in the catalog's independently
// filtered price index, and every returned mode map belongs to the caller.
func ParseOpenCodeOfferings(body []byte) map[string]map[string]OpenCodeOffering {
	var source struct {
		Models    map[string]json.RawMessage `json:"models"` // qualified public model records
		Providers map[string]struct {
			NPM    string `json:"npm"` // provider default SDK
			Models map[string]struct {
				Type      string `json:"type"`               // declared model operation
				Canonical string `json:"canonical_model_id"` // public author metadata identity
				Provider  struct {
					NPM string `json:"npm"`
				} `json:"provider"` // per-model SDK override
			} `json:"models"` // exact model identity records
		} `json:"providers"` // service-specific model offerings
	}
	if json.Unmarshal(body, &source) != nil {
		return nil
	}
	// Older offerings omit canonical references even when an exact public model
	// exists. Keep only unique author matches so shared names cannot choose a vendor.
	publicModels := make(map[string]string, len(source.Models))
	for canonical := range source.Models {
		author, model, qualified := strings.Cut(canonical, "/")
		if !qualified || author == "" || model == "" {
			continue
		}
		if _, exists := publicModels[model]; exists {
			publicModels[model] = ""
			continue
		}
		publicModels[model] = canonical
	}
	result := make(map[string]map[string]OpenCodeOffering, 2)
	for mode, name := range map[string]string{"payg": "opencode", "go": "opencode-go"} {
		service := source.Providers[name]
		routes := make(map[string]OpenCodeOffering)
		for id, model := range service.Models {
			sdk := model.Provider.NPM
			if sdk == "" {
				sdk = service.NPM
			}
			var target string
			switch sdk {
			case "@ai-sdk/openai":
				target = "openai_responses"
			case "@ai-sdk/openai-compatible":
				target = "openai_chat_completions"
			case "@ai-sdk/anthropic":
				target = "anthropic_messages"
			case "@ai-sdk/google":
				target = "gemini_generate_content"
			}
			// Decision entries share a provider-level SDK in older publications, but
			// their declared operation requires the independent SystemOne request shape.
			if model.Type == "decision" {
				target = "systemone"
			}
			canonical := model.Canonical
			if canonical == "" {
				canonical = publicModels[id]
			}
			routes[id] = OpenCodeOffering{Protocol: target, CanonicalModelID: canonical}
		}
		result[mode] = routes
	}
	return result
}
