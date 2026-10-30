package provider

import (
	"context"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// text_apikey_test_target.go dispatches prepared Kimi, Zhipu, and DeepSeek
// probes to adaptive coverage, shared Responses execution, or the fixed route.
// Every path receives one run carrying its group, automatic flag, and
// User-Agent, and the target combines platform failures with retained sink
// errors before returning.

// textAPIKeyTestTarget retains Kimi, Zhipu, and DeepSeek probe dependencies and
// the record selected for execution.
type textAPIKeyTestTarget struct {
	defaultModel string              // platform-selected default test model
	executor     *NativeProtocolTest // shared Kimi/Zhipu/DeepSeek probe dependencies
	// record retains the provider's authentication and selected protocols. Keep it
	// stable until the prepared probe and information reads finish.
	record *provider.Record
}

// Target binds a provider record and its platform-selected default model to
// the shared protocol executor. The returned handle borrows the record;
// callers keep its credentials, protocol selection, and endpoint configuration
// stable until execution finishes.
func (s *NativeProtocolTest) Target(value *provider.Record, defaultModel string) provider.TestTarget {
	return textAPIKeyTestTarget{executor: s, record: value, defaultModel: defaultModel}
}

// Information returns the Kimi, Zhipu, and DeepSeek record's routing snapshot
// and configured API protocol.
func (t textAPIKeyTestTarget) Information() provider.TestTargetInfo {
	return provider.TestTargetInfo{ProviderSnapshot: t.record.RoutingSnapshot(), APIProtocol: (provider.ProtocolTarget{Record: t.record}).GetAPIProtocol()}
}

// Execute routes the prepared Kimi, Zhipu, and DeepSeek request through
// adaptive, shared Responses, or fixed execution. An explicit adaptive protocol
// narrows coverage; the run carries automatic identity and returns platform and
// retained sink failures together. A routed group probe executes only its
// resolved upstream protocol, regardless of the record's inferred standalone
// test family.
func (t textAPIKeyTestTarget) Execute(ctx context.Context, request provider.PreparedTestRequest, sink provider.TestEventSink) error {
	run := NewTestRun(ctx, make(http.Header), sink)
	run.GroupID = request.GroupID
	run.Automatic = request.Automatic
	run.Headers.Set("User-Agent", request.UserAgent)
	defer run.Cancel()
	var err error
	if request.ProtocolRoute.UpstreamProtocol != "" {
		// Group routing already chose one enabled endpoint. Narrow adaptive execution
		// to that endpoint so the record's other protocols cannot add requests.
		return run.Result(t.executor.ExecuteAdaptive(run, t.record, request.Model, request.Prompt, t.defaultModel, request.ProtocolRoute.UpstreamProtocol))
	}
	switch request.Route {
	case provider.TestRouteTextAPIKeyAdaptive:
		// Adaptive preparation distinguishes an omitted protocol from a selected one.
		// Pass that selection through so the executor can reject disabled protocols
		// before any upstream request.
		err = t.executor.ExecuteAdaptive(run, t.record, request.Model, request.Prompt, t.defaultModel, provider.TestProtocolID(request.Protocol))
	case provider.TestRouteTextAPIKeyResponses:
		err = t.executor.Responses.Execute(run, t.record, request.Model, request.Prompt, request.Mode, request.TestType)
	default:
		err = t.executor.Execute(run, t.record, request.Model, request.Prompt, t.defaultModel)
	}
	return run.Result(err)
}
