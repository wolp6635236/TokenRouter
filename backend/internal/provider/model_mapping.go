package provider

import (
	"maps"
	"strings"
)

// model_mapping.go resolves configured provider model mappings and platform
// defaults for forwarding.
// Provider mappings use configured string targets as a complete replacement for
// platform defaults. Antigravity supplies defaults when credentials contain no
// usable string mapping, including a present map whose values all have other
// types. A configured empty string is still a usable entry, so it suppresses
// default selection even though forwarding retains the requested name for that
// target.
//
// Ordinary forwarding checks the supplied spelling before a trimmed retry and
// applies one mapping hop. Compact forwarding trims its input first and reads a
// separate mapping. Returned mapping maps belong to the caller; factories can
// return shared defaults because ResolveModelMapping copies the selected entries.

// ModelMappingDefaults supplies platform route candidates and optional default
// mappings used by provider model selection.
type ModelMappingDefaults struct {
	// Models supplies finite route candidates and Spark quota models. A nil function
	// leaves platform enumeration empty and gives Spark shadows an empty scope.
	Models func(*Record) []string
	// Antigravity supplies defaults for Antigravity records without usable saved
	// mappings. Explicit string mappings bypass it, its returned map is copied
	// before use, and nil leaves those records with no mapping.
	Antigravity func() map[string]string
}

// ResolveCompactForwardModel returns the trimmed Compact mapping target or the
// trimmed input when no usable target matches. Nil records and whitespace-only
// input return the trimmed input; Compact rules execute one hop.
func ResolveCompactForwardModel(value *Record, model string) string {
	model = strings.TrimSpace(model)
	if model == "" || value == nil {
		return model
	}
	mapped, matched := value.ResolveCompactMappedModel(model)
	// An empty Compact target supplies no outbound model. Keep the normalized
	// request so callers can continue ordinary forwarding with that model.
	if matched && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	return model
}

// ResolveForwardMappedModel returns one mapped target, trimmed for forwarding.
// A nil record returns an empty string. Missing or whitespace-only targets retain
// requested exactly as supplied, including its surrounding whitespace.
func ResolveForwardMappedModel(value *Record, requested string, defaults ModelMappingDefaults) string {
	if value == nil {
		return ""
	}
	mapped, matched := ResolveMappedModel(ResolveModelMapping(value, defaults), requested)
	return ForwardMappedTarget(requested, mapped, matched)
}

// ForwardMappedTarget turns one mapping result into the model to forward.
// A miss or a whitespace-only target returns requested exactly as supplied, so
// platform normalization receives the caller's model instead of an empty
// value; a usable target is returned trimmed.
func ForwardMappedTarget(requested, mapped string, matched bool) string {
	if !matched || strings.TrimSpace(mapped) == "" {
		return requested
	}
	return strings.TrimSpace(mapped)
}

// ResolveModelMapping returns caller-owned string mappings for a nonnil record.
// Antigravity uses a copied default map when credentials, the mapping container,
// or all string entries are absent, and a nil default factory yields no mapping
// on those paths; other platforms return nil.
//
// Any string-valued entry selects the entire configured map, including an empty
// target. Nonstring entries are skipped, keys and targets retain their spelling,
// and configured mappings are never augmented with platform defaults. The Models
// callback is used by scope and candidate selection, not this mapping lookup.
func ResolveModelMapping(a *Record, defaults ModelMappingDefaults) map[string]string {
	// Saved JSON mappings arrive as map[string]any. Other container types have no
	// usable entries here and follow the same default selection as missing maps.
	rawMapping, _ := a.Credentials["model_mapping"].(map[string]any)
	if a.Credentials == nil {
		if a.Platform == PlatformAntigravity {
			return defaults.antigravityMapping()
		}
		return nil
	}
	if len(rawMapping) == 0 {
		if a.Platform == PlatformAntigravity {
			return defaults.antigravityMapping()
		}
		return nil
	}

	result := make(map[string]string)
	for k, v := range rawMapping {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	if len(result) > 0 {
		return result
	}

	// A populated raw map can contain only nonstring values. Treat that unusable
	// configuration like an absent mapping so Antigravity still receives defaults.
	if a.Platform == PlatformAntigravity {
		return defaults.antigravityMapping()
	}
	return nil
}

// antigravityMapping returns a copied Antigravity default map, or nil when the
// caller supplied no default factory, such as a snapshot of saved mappings only.
func (d ModelMappingDefaults) antigravityMapping() map[string]string {
	if d.Antigravity == nil {
		return nil
	}
	return maps.Clone(d.Antigravity())
}
