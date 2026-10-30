package provider

import (
	"strings"

	protocolcore "github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// protocol_target.go resolves configured protocol slots, legacy variants, and
// platform defaults for one selected operation. Platform declarations identify
// records with mode-specific text endpoints. A recognized operation overrides
// the stored legacy variant; a present unified protocol key selects adaptive
// lookup even when the enabled set is empty. Protocol admission is a separate
// decision made before execution.
//
// Adaptive lookup checks the requested protocol slot and permits legacy
// base_url as a Chat fallback. Fixed Messages records retain their stored
// root. Model-list lookup recognizes migrated Messages roots, maps declared
// built-in addresses to Chat defaults, and strips a custom relay's final
// anthropic segment. Operation state remains separate from the borrowed
// provider record so concurrent attempts cannot overwrite shared
// configuration.

// ProtocolTarget combines borrowed provider configuration with one operation's
// selected text protocol for endpoint and variant resolution.
type ProtocolTarget struct {
	// Record remains borrowed during endpoint lookup. Callers keep its credentials
	// and platform stable while resolving an operation's URLs and variant.
	*Record
	// Protocol overrides the stored variant for recognized text identifiers with
	// a nonnil record. Empty or other identifiers defer to ConfiguredAPIProtocol.
	Protocol protocolcore.ProtocolID
}

// ConfiguredAPIProtocol resolves legacy endpoint selection for platforms with
// text defaults. Nil records, platforms without declarations, and unrecognized
// variants use Chat. A present upstream_protocols key selects adaptive lookup,
// including an empty or malformed enabled set.
//
// With no unified key, trimmed Messages, Chat, and adaptive values are
// accepted directly. Responses requires the platform's native API-key
// capability; unsupported Responses input resolves to Chat. This method
// chooses endpoint lookup behavior and leaves enabled-protocol admission to
// the caller.
func (r *Record) ConfiguredAPIProtocol() string {
	if r == nil || !r.HasProtocolEndpointDefaults() {
		return APIProtocolChatCompletions
	}
	// A unified set separates protocol admission from endpoint selection. Key
	// presence selects adaptive lookup even when parsing that selection would yield empty.
	if _, unified := r.Credentials[UpstreamProtocolsKey]; unified {
		return APIProtocolAdaptive
	}
	switch strings.TrimSpace(r.GetCredential("api_protocol")) {
	case APIProtocolAdaptive:
		return APIProtocolAdaptive
	case APIProtocolAnthropic:
		return APIProtocolAnthropic
	case APIProtocolResponses:
		// Legacy Responses selection uses the same platform predicate as forwarding.
		// Platforms lacking that endpoint fall through to the default Chat variant.
		if r.SupportsAPIKeyResponses() {
			return APIProtocolResponses
		}
	case APIProtocolChatCompletions:
		return APIProtocolChatCompletions
	}
	return APIProtocolChatCompletions
}

// BaseURL selects the endpoint for the operation's protocol and provider. An
// absent protocol selects Chat. Callers validate the returned address before
// connecting; protocol admission and Gemini version suffix handling remain with
// the request builder consuming this base. OpenCode selects its mode and exact
// protocol slot here. Other providers retain their platform-specific lookup,
// including the Kimi, Zhipu, and DeepSeek distinction between fixed legacy
// bases and adaptive slots. Endpoint selection does not enable a protocol or
// apply request-body storage policy; callers establish an admitted route
// separately before dispatch.
func (a ProtocolTarget) BaseURL() string {
	if a.Record == nil {
		return ""
	}
	selected := a.Protocol
	if selected == "" {
		selected = protocolcore.ProtocolOpenAIChatCompletions
	}
	if a.IsOpenCode() {
		return a.OpenCodeBaseURL(selected)
	}
	switch selected {
	case protocolcore.ProtocolSystemOne:
		return a.GetJevBaseURL()
	case protocolcore.ProtocolGeminiGenerateContent:
		return a.GetGeminiBaseURL("https://generativelanguage.googleapis.com")
	case protocolcore.ProtocolAnthropicMessages:
		if a.HasProtocolEndpointDefaults() {
			return a.GetAnthropicProtocolBaseURL()
		}
		return a.GetBaseURL()
	case protocolcore.ProtocolOpenAIResponses:
		if _, unified := a.Credentials[UpstreamProtocolsKey]; a.UsesResponsesEndpointSlots() && (unified || a.ConfiguredAPIProtocol() == APIProtocolAdaptive) {
			return a.GetTextAPIKeyProtocolBaseURL(APIProtocolResponses)
		}
	}
	return a.GetOpenAIBaseURL()
}

// GetOpenAIBaseURL returns the provider's ordinary OpenAI-compatible base. The
// record must be nonnil. Adaptive Kimi, Zhipu, and DeepSeek configuration
// selects its Chat slot; callers needing a selected protocol's endpoint use
// BaseURL before validation.
func (a ProtocolTarget) GetOpenAIBaseURL() string {
	return a.OpenAIBaseURL(a.IsAdaptiveAPIProtocol())
}

// GetAPIProtocol selects a recognized operation text variant ahead of stored
// configuration for a nonnil record. Messages, Responses, and Chat identifiers
// override the record's variant even on another platform; other identifiers
// defer to ConfiguredAPIProtocol.
//
// A nil record resolves to Chat, while a unified Kimi, Zhipu, and DeepSeek
// record with no recognized operation selection resolves to adaptive. This
// helper does not validate native membership or execution support. Callers
// supply a separately admitted route before treating its selected variant as an
// executable endpoint.
func (a ProtocolTarget) GetAPIProtocol() string {
	if a.Record != nil && a.Protocol != "" {
		switch a.Protocol {
		case protocolcore.ProtocolAnthropicMessages:
			return APIProtocolAnthropic
		case protocolcore.ProtocolOpenAIResponses:
			return APIProtocolResponses
		case protocolcore.ProtocolOpenAIChatCompletions:
			return APIProtocolChatCompletions
		}
	}

	return a.ConfiguredAPIProtocol()
}

// UsesStatelessResponses reports whether the operation uses the platform's
// stateless Responses policy. Nil or stateful platforms return false.
// Responses and adaptive variants qualify even when an enabled selection is
// empty; route admission validates the actual protocol separately.
func (a ProtocolTarget) UsesStatelessResponses() bool {
	if a.Record == nil || !a.RequiresStatelessResponses() {
		return false
	}
	switch a.GetAPIProtocol() {
	case APIProtocolResponses, APIProtocolAdaptive:
		return true
	default:
		return false
	}
}

// UsesResponsesEndpointSlots reports whether native Responses uses declared
// protocol-specific roots for this operation. Endpoint availability is checked
// independently of continuation policy, so a stateful platform can use the same
// slot resolver. Nil, unsupported, and non-Responses fixed variants return false.
func (a ProtocolTarget) UsesResponsesEndpointSlots() bool {
	if a.Record == nil || !a.HasProtocolEndpointDefaults() || !a.SupportsAPIKeyResponses() {
		return false
	}
	switch a.GetAPIProtocol() {
	case APIProtocolResponses, APIProtocolAdaptive:
		return true
	default:
		return false
	}
}

// RequiresResponsesClientToolEncoding selects the platform's client-tool envelope
// conversion for a native Responses operation. Nil, unsupported, and other
// operation variants return false; request execution separately checks API-key
// authentication, Compact mode, and whether the body contains client tools.
func (a ProtocolTarget) RequiresResponsesClientToolEncoding() bool {
	if a.Record == nil || !a.UsesResponsesEndpointSlots() {
		return false
	}
	return a.rules().text.responsesClientTools
}

// IsAdaptiveAPIProtocol reports whether variant resolution selects adaptive
// endpoint lookup. Nil records resolve to Chat and return false. A recognized
// operation text protocol overrides an adaptive stored configuration through
// GetAPIProtocol before this comparison.
func (a ProtocolTarget) IsAdaptiveAPIProtocol() bool {
	return a.GetAPIProtocol() == APIProtocolAdaptive
}

// GetTextAPIKeyProtocolBaseURL returns a configured or default Kimi, Zhipu, and
// DeepSeek address for a supported text protocol. Other platforms and nil
// records return empty. A unified key or adaptive operation first checks that
// exact protocol slot for a nonblank string, then Chat alone accepts a nonblank
// legacy base_url. Chosen custom strings are trimmed.
//
// Other cases use DefaultTextAPIKeyProtocolBaseURL's platform and provider-mode
// defaults, which return empty for unknown variants unless the earlier slot
// supplied an address. Fixed legacy lookup bypasses slots here;
// GetAnthropicProtocolBaseURL handles its stored Messages base separately. This
// helper selects an address and leaves URL and protocol admission validation to
// execution callers.
func (a ProtocolTarget) GetTextAPIKeyProtocolBaseURL(protocol string) string {
	if a.Record == nil || !a.HasProtocolEndpointDefaults() {
		return ""
	}
	if _, unified := a.Credentials[UpstreamProtocolsKey]; unified || a.ConfiguredAPIProtocol() == APIProtocolAdaptive {
		if baseURL := a.protocolSlotBaseURL(protocol); baseURL != "" {
			return baseURL
		}
		if protocol == APIProtocolChatCompletions {
			if baseURL := strings.TrimSpace(a.GetCredential("base_url")); baseURL != "" {
				return baseURL
			}
		}
	}
	// Missing usable slots fall back to platform and mode defaults. Legacy base_url
	// is a Chat fallback only, so another protocol does not inherit that relay address.
	return a.DefaultTextAPIKeyProtocolBaseURL(protocol)
}

// IsAnthropicProtocol reports whether variant resolution selects fixed Messages.
// Adaptive lookup returns false and is checked separately by endpoint callers.
// Nil records resolve to Chat, while a recognized operation Messages identifier
// takes precedence over stored configuration.
func (a ProtocolTarget) IsAnthropicProtocol() bool {
	return a.GetAPIProtocol() == APIProtocolAnthropic
}

// GetAnthropicProtocolBaseURL returns the Messages address for fixed Messages
// or adaptive lookup. Nil records and other resolved variants return empty. A
// present unified key or adaptive variant delegates to Kimi, Zhipu, and
// DeepSeek slot lookup, which also returns empty for records from other platforms.
//
// Fixed Messages lookup on API-key and upstream records prefers a trimmed
// base_url, including operation-selected Messages on a legacy record. Supported
// Kimi, Zhipu, and DeepSeek platforms then use their Messages defaults with
// Kimi's mode distinction. This keeps a legacy single-protocol relay address
// separate from unified slots. The helper performs no URL validation or
// enabled-selection check; execution callers establish those requirements
// before sending requests.
func (a ProtocolTarget) GetAnthropicProtocolBaseURL() string {
	if a.Record == nil || (!a.IsAnthropicProtocol() && !a.IsAdaptiveAPIProtocol()) {
		return ""
	}
	if _, unified := a.Credentials[UpstreamProtocolsKey]; unified || a.ConfiguredAPIProtocol() == APIProtocolAdaptive {
		return a.GetTextAPIKeyProtocolBaseURL(APIProtocolAnthropic)
	}
	// A fixed Messages lookup uses the record's single custom endpoint in base_url.
	// Prefer that value before platform defaults because it can identify a relay prefix.
	if a.Type == capability.ProviderTypeAPIKey || a.Type == capability.ProviderTypeUpstream {
		if baseURL := strings.TrimSpace(a.GetCredential("base_url")); baseURL != "" {
			return baseURL
		}
	}
	return a.DefaultTextAPIKeyProtocolBaseURL(APIProtocolAnthropic)
}

// GetOpenAIFormatBaseURL returns the model-list and OpenAI-format address for
// the selected provider, or empty for a nil record. Kimi, Zhipu, and DeepSeek
// Messages-derived addresses use platform Chat defaults for built-in endpoints.
// Custom bases delegate terminal /anthropic path removal to
// StripAnthropicPathSuffix.
//
// On unified Kimi, Zhipu, and DeepSeek records, raw slot strings identify a
// migrated Messages base: Chat is empty, Messages is nonempty, and Messages
// exactly equals base_url. These comparisons precede trimming and override the
// operation's Messages flag. Other configurations delegate to GetOpenAIBaseURL.
// Path stripping uses decoded URL paths and clears RawPath when removing the
// suffix, so escaping can change. Parse failures return trimmed input.
// Connecting callers validate the resulting address before use.
func (a ProtocolTarget) GetOpenAIFormatBaseURL() string {
	if a.Record == nil {
		return ""
	}

	// Migration retains base_url after moving a fixed Messages address into its
	// slot. Recognize that exact raw equality before deriving the model-list address;
	// a nonempty Chat slot selects ordinary Chat-base lookup even on a Messages operation.
	anthropicBase := a.IsAnthropicProtocol()
	if _, unified := a.Credentials[UpstreamProtocolsKey]; unified && a.HasProtocolEndpointDefaults() {
		urls, _ := a.Credentials["api_base_urls"].(map[string]any)
		chat, _ := urls[APIProtocolChatCompletions].(string)
		messages, _ := urls[APIProtocolAnthropic].(string)
		anthropicBase = chat == "" && messages != "" && messages == a.GetCredential("base_url")
	}
	if !anthropicBase {
		return a.GetOpenAIBaseURL()
	}

	if baseURL := strings.TrimSpace(a.GetCredential("base_url")); baseURL != "" {
		// Custom relay prefixes can expose sibling Messages and OpenAI routes. Strip
		// the terminal Messages segment through the URL helper before model lookup;
		// that helper owns decoding, path escaping, and trailing-slash handling.
		if !IsDefaultTextAPIKeyMessagesBaseURL(baseURL) {
			return StripAnthropicPathSuffix(baseURL)
		}
	}
	if a.HasProtocolEndpointDefaults() {
		return a.DefaultTextAPIKeyProtocolBaseURL(APIProtocolChatCompletions)
	}
	return a.GetOpenAIBaseURL()
}

// SupportsHTTPResponsesContinuation reports whether the selected route may carry
// stored Responses identifiers over HTTP. OpenCode requires an admitted native
// Responses route and API-key credentials; route admission and response-owner
// validation remain obligations of the caller before dispatch. An unset OpenAI
// route keeps the legacy API-key default for callers without an admitted plan.
func (a ProtocolTarget) SupportsHTTPResponsesContinuation() bool {
	if a.Record == nil || a.Type != capability.ProviderTypeAPIKey {
		return false
	}
	if a.IsOpenAIApiKey() {
		return a.Protocol == "" || a.Protocol == protocolcore.ProtocolOpenAIResponses
	}
	return a.IsOpenCode() && a.Protocol == protocolcore.ProtocolOpenAIResponses
}
