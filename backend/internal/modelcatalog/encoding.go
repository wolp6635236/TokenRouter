package modelcatalog

import (
	"slices"
	"strings"
)

// encoding.go selects model payload constraints from a model's author identity
// and the request protocol. An OpenCode offering can use a different outbound
// name from its canonical model, so its retained AttributeModel supplies the
// author and model used here. Missing or unrecognized author qualification
// leaves those model rules unavailable. Native services retain their existing
// service recognition and complete model normalization.
//
// Sampling conditions, thinking representations, and prohibited request fields
// accompany the model across supported protocols. Authentication restrictions,
// service output limits, site-specific behavior, and media policies remain in
// platform.go. Request-state defaults use the same identity as the encoding
// rules so an alias with omitted thinking controls behaves like its author
// model. The original Target continues to identify the service and outbound name.

// applyModelEncoding adds model payload rules before service and administrator
// patches. OpenCode requires a qualified canonical author and a supported text
// protocol; other routes retain their native service's model rules, including
// metadata queries whose protocol has not been selected yet.
//
// Conditions describe display capabilities until ForRequest evaluates a request.
// This method keeps authentication and endpoint policies outside the model rule
// selection, so a canonical OpenAI identity cannot acquire Codex OAuth field
// removal or Compact effort aliases through an intermediary.
func (r *Resolution) applyModelEncoding(patch *Attributes, conditions ParameterConditions) {
	t := r.Target
	author, model := t.modelEncodingIdentity()
	// Intermediary encodings are selected explicitly; model constraints cannot be
	// inferred for a different wire shape or a route with no selected protocol.
	if t.usesCanonicalIdentity() && !slices.Contains([]string{"openai_chat_completions", "openai_responses", "anthropic_messages"}, t.UpstreamProtocol) {
		return
	}
	switch author {
	case "openai":
		// OpenAI rules describe Chat and Responses payloads. A Messages route has
		// no registered representation for these model controls.
		if t.UpstreamProtocol == "anthropic_messages" {
			return
		}
		switch model {
		case "gpt-3.5-turbo", "gpt-4", "gpt-4-turbo", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "gpt-4o", "gpt-4o-2024-05-13", "gpt-4o-2024-08-06", "gpt-4o-2024-11-20", "gpt-4o-mini", "gpt-5.1", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5.1-codex-max", "gpt-5.2", "gpt-5.2-chat-latest", "gpt-5.2-pro", "gpt-5.2-codex":
			// These registered identities declare verbosity unavailable. The
			// OpenAI forwarding transformer reads this flag when removing
			// text.verbosity from their request bodies.
			r.Rules.Verbosity = valuePointer(false)
		}
		if (t.ServiceID() == "openai" || t.usesCanonicalIdentity()) && r.Attributes.Temperature != nil && !*r.Attributes.Temperature {
			// OpenAI model metadata with unavailable temperature also restricts
			// top_p. Both author routes and qualified intermediary identities
			// therefore remove the same sampling fields from their requests.
			patch.TopP = valuePointer(false)
		}
		if (t.ServiceID() == "openai" || t.usesCanonicalIdentity()) && slices.Contains([]string{"gpt-5.2", "gpt-5.4"}, model) {
			// These registered sampling controls depend on none effort. Keep
			// their request condition so ForRequest evaluates the same model
			// with each supplied reasoning state.
			conditions["temperature"] = ParameterCondition{Efforts: []string{"none"}}
			conditions["top_p"] = ParameterCondition{Efforts: []string{"none"}}
			patch.TopP = valuePointer(true)
		}
		if t.UpstreamProtocol == "openai_chat_completions" && (t.ServiceID() == "openai" || t.usesCanonicalIdentity()) {
			// Tool capability can differ between Chat and Responses for one
			// model. Apply the registered Chat restriction here so route
			// attributes retain that protocol distinction.
			switch model {
			case "gpt-6-astra", "gpt-6.1-sol":
				patch.ToolCall = valuePointer(false)
			case "gpt-6-sol", "gpt-6-luna":
				conditions["tool_call"] = ParameterCondition{Efforts: []string{"none"}}
			}
		}
	case "grok":
		// Grok field restrictions are registered for OpenAI-shaped requests; a
		// Messages route does not establish the same payload field ownership.
		if t.UpstreamProtocol == "anthropic_messages" {
			return
		}
		// Grok model-specific removals accompany its control declarations. The
		// forwarding parameter pass consumes DropFields even when the base
		// catalog contains other capability metadata.
		if slices.Contains([]string{"grok-composer", "grok-composer-2.5-fast", "composer-2.5"}, model) {
			patch.ReasoningOptions = valuePointer([]ReasoningOption{})
		}
		if strings.HasPrefix(model, "grok-4.20") {
			r.Rules.DropFields = append(r.Rules.DropFields, "logprobs", "top_logprobs")
		}
		if model == "grok-4.5" {
			r.Rules.DropFields = append(r.Rules.DropFields, "presence_penalty", "presencePenalty", "frequency_penalty", "frequencyPenalty", "stop")
		}
	case "deepseek":
		// DeepSeek sampling rules cover Chat and Messages requests. Responses
		// overrides lack a registered model encoding for these conditional controls.
		if t.usesCanonicalIdentity() && t.UpstreamProtocol == "openai_responses" {
			return
		}
		if slices.Contains([]string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-flash", "deepseek-v4-flash-vision-exp"}, model) {
			// These sampling controls select opposite thinking states. Keep
			// both conditions so ForRequest disables whichever parameter
			// conflicts with the request's evaluated thinking choice.
			conditions["temperature"] = ParameterCondition{Thinking: valuePointer(false)}
			conditions["top_p"] = ParameterCondition{Thinking: valuePointer(true)}
			patch.TopP = valuePointer(true)
		}
	}
}

// applyMessagesEncoding adds signatures and thinking representations after the
// service-specific rules. OpenCode requires the declared canonical author to
// own the signature family, and other platforms retain their Messages model
// recognition for compatible services and hosted variants. The model name
// remains the canonical name throughout these checks, so an intermediary alias
// cannot lose the author model's adaptive mode. Service rules run first because
// compatible Messages encoding may refine their default thinking representation.
func (r *Resolution) applyMessagesEncoding(conditions ParameterConditions) {
	t := r.Target
	if t.UpstreamProtocol != "anthropic_messages" {
		return
	}
	author, model := t.modelEncodingIdentity()
	signature := thinkingSignatureMode(model)
	if t.usesCanonicalIdentity() {
		switch author {
		case "anthropic":
			if signature != "anthropic" {
				return
			}
		case "deepseek", "kimi", "zhipu", "minimax", "alibaba":
			if signature != "passback" {
				return
			}
		default:
			return
		}
	}
	// Claude sampling and adaptive thinking belong to its Messages encoding;
	// intermediaries carrying that model need the same payload constraints as author endpoints.
	if strings.HasPrefix(model, "claude-") {
		conditions["temperature"] = ParameterCondition{Thinking: valuePointer(false)}
		r.Rules.ThinkingMode = claudeThinkingMode(model, r.Attributes)
	}
	if signature != "" {
		r.Rules.SignatureMode = signature
	}
	// MiniMax Messages uses adaptive thinking while its catalog attributes retain
	// the supported controls available to conversion and request parameter validation.
	if strings.HasPrefix(model, "minimax-m") {
		r.Rules.ThinkingMode = "adaptive"
	}
}

// modelEncodingIdentity returns the author family and model used for payload
// rules and request defaults. Targets whose rules come from canonical references
// use the qualified AttributeModel; unknown authors and missing qualifications
// return empty values.
//
// Native routes retain service-based selection, Google resource normalization,
// and service-prefix removal. Qualification is interpreted locally and never
// changes Target.Platform, catalog service identity, or the outbound Model.
// These returned names select model rules only, so callers must continue using
// the original target for authentication, quotas, addresses, and media policy.
func (t Target) modelEncodingIdentity() (string, string) {
	model := normalize(t.Model)
	if t.usesCanonicalIdentity() {
		// A qualified reference supplies author evidence independently of the
		// service alias. Reject incomplete and nested references before choosing rules.
		author, name, qualified := strings.Cut(normalize(t.AttributeModel), "/")
		family := serviceRuleFamily(author)
		if !qualified || name == "" || strings.Contains(name, "/") || family == "" {
			return "", ""
		}
		return family, name
	}
	if t.Platform == "gemini" || t.Platform == "antigravity" {
		model = googleResourceModel(model)
	}
	if t.ServiceID() != "" {
		model = strings.TrimPrefix(model, t.ServiceID()+"/")
	}
	return t.servicePlatform(), model
}
