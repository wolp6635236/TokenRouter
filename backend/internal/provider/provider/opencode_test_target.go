package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/jev"
	"github.com/TokenFlux/TokenRouter/internal/upstream/opencode"
)

// opencode_test_target.go resolves and validates one OpenCode probe route
// before protocol execution. The loaded provider snapshot supplies both the
// one-hop model mapping and that final model's enabled protocol. Explicit
// selections and group routes must agree with the selected protocol, so
// validation and outbound model identity describe the same request.
//
// The target owns a single TestRun and OpenCode endpoint and authentication
// preparation. Streaming protocols then use the native exchange shared with
// Kimi, Zhipu, and DeepSeek adaptive probes, including its payload builders,
// stream readers, and provider error-state write after a 401; SystemOne uses
// the decision exchange shared with Jev. Group and client-entry identity remain
// available to attribute lookups, while each request's cancellation and sink
// failures belong to the original run.

// openCodeTestTarget retains shared probe dependencies and one provider configuration.
type openCodeTestTarget struct {
	executor *NativeProtocolTest // shared native probe executor
	record   *provider.Record    // retained provider configuration
}

// Information returns OpenCode identity and its configured protocol selection.
func (t openCodeTestTarget) Information() provider.TestTargetInfo {
	return provider.TestTargetInfo{ProviderSnapshot: t.record.RoutingSnapshot()}
}

// Execute resolves one model/protocol pair and executes it through one TestRun.
// The loaded record stays stable throughout synchronous execution. Missing models,
// disabled protocols, and conflicting explicit selections emit an error before
// endpoint validation or credential transmission.
//
// Mapping reads the request model exactly once, including wildcard aliases. A
// mapped target such as b from a to b remains b even when b also maps to c.
// The selected group entry survives protocol conversion in the run's context,
// while the prepared native probe carries the final model and upstream protocol.
func (t openCodeTestTarget) Execute(ctx context.Context, request provider.PreparedTestRequest, sink provider.TestEventSink) error {
	headers := make(http.Header)
	headers.Set("User-Agent", request.UserAgent)
	run := NewTestRun(ctx, headers, sink)
	run.GroupID, run.Automatic = request.GroupID, request.Automatic
	defer run.Cancel()
	output := TestStreamOutput{}
	source := request.ProtocolRoute.ClientProtocol
	var fallbacks map[capability.ProtocolID][]capability.ProtocolID
	if source != "" {
		// Restrict conversion to the endpoint chosen during scheduling so refreshed
		// provider data cannot silently select a different protocol for this probe.
		fallbacks = map[capability.ProtocolID][]capability.ProtocolID{source: {request.ProtocolRoute.UpstreamProtocol}}
	}
	snapshot := t.record.RoutingSnapshot()
	resolved, ok := snapshot.ResolveModelRoute(strings.TrimSpace(request.Model), source, fallbacks)
	if !ok {
		return output.Error(run, fmt.Sprintf("OpenCode model %q has no enabled protocol; sync models or configure its protocol", request.Model))
	}
	if request.Protocol != "" && provider.TestProtocolID(request.Protocol) != resolved.Protocol {
		return output.Error(run, fmt.Sprintf("OpenCode model %q uses %s", resolved.Model, resolved.Protocol))
	}
	if request.ProtocolRoute.UpstreamProtocol != "" && request.ProtocolRoute.UpstreamProtocol != resolved.Protocol {
		return output.Error(run, "OpenCode model protocol changed")
	}
	if (request.TestType == provider.ProviderTestTypeDecision || len(request.SystemOne) > 0) && resolved.Protocol != capability.ProtocolSystemOne {
		return output.Error(run, "Decision tests require a SystemOne model")
	}
	if t.record.Type != provider.ProviderTypeAPIKey || strings.TrimSpace(t.record.GetCredential("api_key")) == "" {
		return output.Error(run, "OpenCode probes require an API key")
	}
	base, err := t.executor.ValidateURL((provider.ProtocolTarget{Record: t.record, Protocol: resolved.Protocol}).BaseURL())
	if err != nil {
		return output.Error(run, "Invalid OpenCode base URL: "+err.Error())
	}
	run.ProtocolRoute = request.ProtocolRoute
	if run.ProtocolRoute.ClientProtocol == "" {
		run.ProtocolRoute = provider.TestProtocolRoute{ClientProtocol: resolved.Protocol, UpstreamProtocol: resolved.Protocol}
	}
	run.Context = provider.WithTestProtocolRoute(run.Context, run.ProtocolRoute)
	probe := nativeProbe{
		route: resolved, baseURL: base, label: "Native probe",
		applyHeaders: func(headers http.Header) {
			ApplyProviderHeaderOverrides(t.record, headers)
			opencode.ApplyHeaders(headers, t.record.GetCredential("api_key"), run.GetHeader)
		},
	}
	return run.Result(t.execute(run, request, probe))
}

// execute selects the probe kind, builds its payload, and runs the exchange for
// the resolved protocol. The caller owns the TestRun and supplies a validated
// root and header callback. Non-Gemini streaming protocols accept text probes
// only, and attribute or payload failures emit an error before output begins.
//
// One start event commits before the request, and a cancelled run returns its
// context error without contacting upstream. SystemOne skips output-type
// inference because its decision body defines the probe; attribute failures for
// that route still stop execution.
func (t openCodeTestTarget) execute(run *TestRun, request provider.PreparedTestRequest, probe nativeProbe) error {
	output := TestStreamOutput{}
	route := probe.route
	var body []byte
	var err error
	if route.Protocol == capability.ProtocolSystemOne {
		if t.executor.Attributes != nil {
			if _, err := t.executor.Attributes(run.Context, t.record, run.GroupID, route.Model, string(route.Protocol)); err != nil {
				return output.Error(run, err.Error())
			}
		}
		body, err = systemOneProbeBody(route.Model, request.Prompt, request.SystemOne)
	} else {
		kind := request.TestType
		if route.Protocol != capability.ProtocolGeminiGenerateContent {
			if kind == provider.ProviderTestTypeImage {
				return output.Error(run, "Selected native protocol supports text probes")
			}
			kind = provider.ProviderTestTypeText
		}
		kind, err = resolveProbeType(run.Context, t.executor.Attributes, t.record, run.GroupID, route.Model, string(route.Protocol), kind)
		if err != nil {
			return output.Error(run, err.Error())
		}
		body, err = t.executor.probePayload(run, t.record, route, request.Prompt, kind)
	}
	if err != nil {
		return output.Error(run, err.Error())
	}
	run.Begin(true)
	output.SendEvent(run, provider.TestEvent{Type: "test_start", Model: route.Model})
	// A sink can cancel during Begin or test_start. Observe that cancellation
	// before starting an upstream request whose output cannot reach the caller.
	if err := run.Context.Err(); err != nil {
		return err
	}
	if route.Protocol == capability.ProtocolSystemOne {
		return executeSystemOneProbe(run, &jev.Target{
			ImagesOptIn: request.SystemOneImages,
			ProviderID:  t.record.ID, URL: jev.EndpointURL(probe.baseURL, "systemone"), Model: route.Model,
			ApplyHeaders: probe.applyHeaders,
			Do:           func(req *http.Request) (*http.Response, error) { return t.executor.send(req, t.record) },
		}, body)
	}
	return t.executor.exchange(run, t.record, probe, body)
}
