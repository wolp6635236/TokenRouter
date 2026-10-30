package provider

import (
	"context"
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// testing_protocol.go describes the entry protocols supported by group probes
// and checks their resolved provider routes against existing test executors.
// Group routing chooses the upstream protocol before testing begins. A test
// retains both identities so attribute rules can match the client entry while
// request construction uses the selected upstream endpoint.
//
// The zero route retains ordinary provider-test inference. A populated route
// requires a supported entry, an upstream protocol enabled for the probe's
// model, and an executor for that exact combination. These checks also run
// after loading credentials, preventing a changed provider configuration from
// silently selecting its default endpoint between scheduling and execution.

// TestProtocolRoute identifies the client entry and resolved upstream test endpoint.
type TestProtocolRoute struct {
	ClientProtocol   capability.ProtocolID // selected group entry protocol
	UpstreamProtocol capability.ProtocolID // resolved provider execution protocol
}

// testProtocolRouteKey separates test route metadata from other context values.
type testProtocolRouteKey struct{}

// GroupProbeProtocols returns the supported group probe entries in catalogue order.
// Callers may modify the returned slice. Compact and operations needing inputs
// beyond the probe prompt remain outside this list.
func GroupProbeProtocols() []capability.ProtocolID {
	return []capability.ProtocolID{
		capability.ProtocolAnthropicMessages,
		capability.ProtocolOpenAIResponses,
		capability.ProtocolOpenAIChatCompletions,
		capability.ProtocolGeminiGenerateContent,
		capability.ProtocolSystemOne,
		capability.ProtocolImagesGenerations,
	}
}

// SupportsProvider reports whether an existing test executor can execute this
// route for model, the name the probe sends before provider mapping. Both
// protocol identities must be present, and the snapshot's one-hop model
// resolution must select exactly the upstream protocol. A different resolved
// endpoint fails rather than changing the endpoint selected by group routing.
//
// Resolution narrows enabled protocols to the final model's declared protocol
// when the snapshot carries a per-model table, so a model declaring Gemini
// accepts a Chat entry only through conversion to Gemini. Executor restrictions
// are narrower than gateway capabilities: Grok tests currently implement
// Responses and image generation, for example. Keeping that distinction here
// lets selection skip such candidates before sending requests, while execution
// repeats the same check against the loaded provider snapshot.
func (r TestProtocolRoute) SupportsProvider(value ProviderSnapshot, model string) bool {
	if !slices.Contains(GroupProbeProtocols(), r.ClientProtocol) || r.UpstreamProtocol == "" {
		return false
	}
	resolved, ok := value.ResolveModelRoute(model, r.ClientProtocol,
		map[capability.ProtocolID][]capability.ProtocolID{r.ClientProtocol: {r.UpstreamProtocol}})
	if !ok || resolved.Protocol != r.UpstreamProtocol {
		return false
	}
	if platformRuleRegistry[value.Platform].text != nil {
		return r.UpstreamProtocol == capability.ProtocolAnthropicMessages ||
			r.UpstreamProtocol == capability.ProtocolOpenAIResponses ||
			r.UpstreamProtocol == capability.ProtocolOpenAIChatCompletions
	}
	switch value.Platform {
	case PlatformOpenAI:
		return r.UpstreamProtocol == capability.ProtocolOpenAIResponses ||
			r.UpstreamProtocol == capability.ProtocolOpenAIChatCompletions ||
			r.UpstreamProtocol == capability.ProtocolImagesGenerations
	case PlatformAnthropic:
		return r.UpstreamProtocol == capability.ProtocolAnthropicMessages
	case PlatformGemini, PlatformAntigravity:
		return r.UpstreamProtocol == capability.ProtocolGeminiGenerateContent
	case PlatformQoder:
		return r.UpstreamProtocol == capability.ProtocolQoderChat
	case PlatformGrok:
		return r.UpstreamProtocol == capability.ProtocolOpenAIResponses ||
			r.UpstreamProtocol == capability.ProtocolImagesGenerations
	case PlatformJev:
		return r.UpstreamProtocol == capability.ProtocolSystemOne
	case PlatformOpenCode:
		// The OpenCode executor implements every protocol an OpenCode provider can
		// enable, so the resolved route needs no further executor restriction.
		return true
	default:
		return false
	}
}

// TestProtocolRouteFromContext returns the selected test route or its zero value.
// The zero value permits existing endpoint inference. TestService supplies a
// populated route only after validating both protocol identities against the target.
func TestProtocolRouteFromContext(ctx context.Context) TestProtocolRoute {
	route, _ := ctx.Value(testProtocolRouteKey{}).(TestProtocolRoute)
	return route
}

// WithTestProtocolRoute retains a validated probe route for attribute lookups.
// Callers validate both protocol identities before using this context. The
// derived context retains the parent's cancellation and all other request values.
func WithTestProtocolRoute(ctx context.Context, route TestProtocolRoute) context.Context {
	return context.WithValue(ctx, testProtocolRouteKey{}, route)
}
