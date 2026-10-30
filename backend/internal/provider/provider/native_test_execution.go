package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	egressprovider "github.com/TokenFlux/TokenRouter/internal/egress/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	openaiprotocol "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/protocol/systemone"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini"
	"github.com/TokenFlux/TokenRouter/internal/upstream/jev"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// native_test_execution.go builds and exchanges native protocol probes whose
// model and wire protocol a platform target has already fixed. Kimi, Zhipu, and
// DeepSeek adaptive Messages and Responses coverage and every OpenCode protocol
// use this one executor through NativeProtocolTest, which supplies attributes,
// transport, TLS profiles, URL validation, and the optional provider error
// store.
//
// A target describes each exchange with a nativeProbe: the resolved route, a
// validated protocol root, a diagnostic label, and a header callback that
// installs platform authentication after protocol defaults. Payloads pass the
// final model directly, so an alias target cannot become another mapping input.
// A 401 reply records provider error state when a store is configured, and the
// protocol's shared stream reader translates successful replies into test
// events. SystemOne uses its buffered decision exchange instead and reports
// response identity and usage validity.

// NativeProtocolTest holds protocol probe dependencies used by independently
// registered platform targets. Those targets supply model defaults, selected
// protocols, endpoint roots, and authentication before a shared exchange runs.
type NativeProtocolTest struct {
	// Attributes resolves the final model for routed group probes. A nil callback
	// skips profile lookup, leaving declared output and sampling permissions unknown.
	Attributes ModelAttributeLookup
	Transport  QoderTransport // provider-aware native probe sender
	// Profiles optionally resolves native request fingerprints. A missing service
	// sends no selected TLS profile through the shared transport.
	Profiles    *egressprovider.TLSProfiles
	ValidateURL func(string) (string, error) // native upstream root validator
	// Store optionally records authentication failures from fixed Messages probes
	// and from every shared native exchange, including OpenCode probes. A nil
	// store skips these best-effort state writes.
	Store interface {
		// SetError records a native probe's provider authentication failure. The
		// executor ignores write errors while returning the upstream status and
		// available body as its test failure.
		SetError(context.Context, int64, string) error
	}
	// Responses supplies the shared OpenAI executor for prepared Kimi and
	// DeepSeek Responses targets. Configure it before that route is selected.
	Responses *OpenAIProviderTest
}

// nativeProbe describes one streaming exchange prepared by a platform target.
type nativeProbe struct {
	route   provider.ResolvedModelRoute // final model and wire protocol
	baseURL string                      // validated protocol root URL
	// label names the endpoint in request and status failures, for example
	// "Adaptive Anthropic endpoint" in "<label> returned 401: <body>".
	label string
	// applyHeaders installs the platform's final authentication and caller
	// identity after protocol defaults have initialized the writable header map.
	applyHeaders func(http.Header)
}

// probePayload builds streaming protocol JSON for the final model and prompt.
// The kind selects Gemini's text or image payload; other protocols send text.
// Routed probes drop sampling fields that the model's attributes forbid, and
// Responses requests to Kimi and DeepSeek endpoints become stateless.
// Unsupported protocols return an error before any transport call.
func (s *NativeProtocolTest) probePayload(run *TestRun, value *provider.Record, route provider.ResolvedModelRoute, prompt, kind string) ([]byte, error) {
	var object map[string]any
	switch route.Protocol {
	case protocol.ProtocolGeminiGenerateContent:
		return geminiTestPayload(route.Model, prompt, kind), nil
	case protocol.ProtocolAnthropicMessages:
		var err error
		object, err = anthropic.TestPayloadWithPrompt(route.Model, prompt)
		if err != nil {
			return nil, err
		}
	case protocol.ProtocolOpenAIChatCompletions:
		object = openai.TestChatCompletionsPayload(route.Model, prompt)
	case protocol.ProtocolOpenAIResponses:
		object = openai.TestResponsesPayload(route.Model, prompt, false)
		// This probe supplies its own user prompt. Codex's internal system
		// instructions belong to its account endpoint and would alter this input.
		delete(object, "instructions")
	default:
		return nil, fmt.Errorf("unsupported native probe protocol %s", route.Protocol)
	}
	if err := applyProbeSampling(run, s.Attributes, value, route.Model, string(route.Protocol), object); err != nil {
		return nil, err
	}
	body, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	if route.Protocol == protocol.ProtocolOpenAIResponses && (provider.ProtocolTarget{Record: value}).UsesStatelessResponses() {
		// Kimi and DeepSeek Responses use fresh stateless requests. Set store
		// false and remove any continuation identifier before sending this
		// probe through the platform endpoint; other platforms keep the
		// builder's request fields.
		body = openaiprotocol.StatelessResponsesRequest(body)
	}
	return body, nil
}

// exchange sends one prepared streaming probe and parses its protocol's reply.
// The caller owns the TestRun, has already emitted any start event, and
// supplies a final route, validated root, and header callback. Response bodies
// close before this method returns.
//
// Transport failures after caller cancellation return the context error. A
// non-OK reply becomes an error event carrying the label, status, and any body
// prefix; a 401 also attempts a provider error-state write when Store is set.
// Messages emits test_complete after its reader succeeds, matching the other
// readers, and adaptive callers suppress those intermediate completions.
func (s *NativeProtocolTest) exchange(run *TestRun, value *provider.Record, probe nativeProbe, body []byte) error {
	output := TestStreamOutput{}
	endpoint, err := nativeProbeEndpoint(value.Platform, probe.route, probe.baseURL)
	if err != nil {
		return output.Error(run, err.Error())
	}
	req, err := http.NewRequestWithContext(run.Context, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return output.Error(run, fmt.Sprintf("Failed to create %s request: %s", probe.label, err.Error()))
	}
	if probe.route.Protocol == protocol.ProtocolOpenAIResponses {
		// Gateway Responses forwarding uses the OpenAI transport profile. Probes
		// select the same response-header wait so a test reflects that timing.
		req = req.WithContext(upstream.WithHTTPUpstreamProfile(req.Context(), upstream.HTTPUpstreamProfileOpenAI))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if probe.route.Protocol == protocol.ProtocolAnthropicMessages {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	probe.applyHeaders(req.Header)
	resp, err := s.send(req, value)
	if err != nil {
		if run.Context.Err() != nil {
			return run.Context.Err()
		}
		return output.Error(run, fmt.Sprintf("%s request failed: %s", probe.label, err.Error()))
	}
	// Close successful and rejected responses here because all protocol readers
	// borrow the body, including readers that return after an early terminal event.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Retain any diagnostic prefix returned before a body read failure so the
		// original HTTP rejection remains the primary explanation for this probe.
		body, _ := io.ReadAll(resp.Body)
		message := fmt.Sprintf("%s returned %d: %s", probe.label, resp.StatusCode, body)
		if resp.StatusCode == http.StatusUnauthorized && s.Store != nil {
			// The endpoint has already rejected the credential. Attempt the
			// provider-state write, then keep the upstream diagnostic when storage
			// itself reports an error.
			_ = s.Store.SetError(run.Context, value.ID, message)
		}
		return output.Error(run, message)
	}
	switch probe.route.Protocol {
	case protocol.ProtocolAnthropicMessages:
		if err := output.AdaptiveAnthropic(run, resp.Body); err != nil {
			return err
		}
		output.SendEvent(run, provider.TestEvent{Type: "test_complete", Success: true})
		return nil
	case protocol.ProtocolOpenAIResponses:
		return output.Responses(run, resp.Body)
	case protocol.ProtocolOpenAIChatCompletions:
		return output.ChatCompletions(run, resp.Body)
	case protocol.ProtocolGeminiGenerateContent:
		return output.Gemini(run, resp.Body)
	default:
		return output.Error(run, "Unsupported native probe protocol")
	}
}

// nativeProbeEndpoint builds the selected protocol's streaming request URL.
// The caller supplies a validated root and final model. DeepSeek Responses use
// the platform's unversioned path, and Gemini model actions validate the model
// using the shared AI Studio URL builder.
func nativeProbeEndpoint(platform string, route provider.ResolvedModelRoute, baseURL string) (string, error) {
	switch route.Protocol {
	case protocol.ProtocolAnthropicMessages:
		return httpclient.BuildOpenAIEndpointURL(baseURL, "/v1/messages"), nil
	case protocol.ProtocolOpenAIResponses:
		return responsesProbeURL(platform, baseURL), nil
	case protocol.ProtocolOpenAIChatCompletions:
		return httpclient.BuildOpenAIEndpointURL(baseURL, "/v1/chat/completions"), nil
	case protocol.ProtocolGeminiGenerateContent:
		return gemini.BuildGeminiAIStudioModelActionURL(baseURL, route.Model, "streamGenerateContent", true)
	default:
		return "", fmt.Errorf("unsupported streaming probe protocol %s", route.Protocol)
	}
}

// send applies the provider's loaded proxy and TLS selection to one prepared
// request. A missing loaded proxy relation sends directly, and a missing
// profile service sends no selected fingerprint. The caller owns the returned
// response body; there is one transport attempt per invocation.
func (s *NativeProtocolTest) send(req *http.Request, value *provider.Record) (*http.Response, error) {
	proxy := ""
	if value.ProxyID != nil && value.Proxy != nil {
		proxy = value.Proxy.URL()
	}
	var profile *tlsfingerprint.Profile
	if s.Profiles != nil {
		profile = s.Profiles.ResolveRequestTLS(egress.TLSSelection{Enabled: value.IsTLSFingerprintEnabled(), DirectProfileID: value.GetTLSFingerprintProfileID()})
	}
	return s.Transport.DoWithTLS(req, proxy, value.ID, value.Concurrency, profile)
}

// systemOneProbeBody builds a minimal decision or validates supplied structured input.
// Supplied state, questions, and extensions survive replacement of the model.
// Blank prompts use the protocol builder's availability question when structured
// input is absent. The caller supplies an already mapped model, which replaces
// any model in the supplied object before validation. This lets manual decision
// forms reuse their question body across several provider models while retaining
// the fields that describe each question and its evaluation state.
func systemOneProbeBody(model, prompt string, structured json.RawMessage) ([]byte, error) {
	if len(structured) == 0 {
		return systemone.ProbeBody(model, prompt), nil
	}
	// Supplied decisions can contain state and extensions unknown to the probe.
	// Replace just the model before protocol validation to retain those caller fields.
	body, err := systemone.ReplaceModel(structured, model)
	if err != nil {
		return nil, fmt.Errorf("invalid SystemOne test request: %w", err)
	}
	if _, err := systemone.ParseRequest(body); err != nil {
		return nil, err
	}
	return body, nil
}

// executeSystemOneProbe exchanges one prepared decision and emits its response.
// The target owns endpoint, authentication, and transport selection. The shared
// protocol executor closes its response and reports usage validity independently.
func executeSystemOneProbe(run *TestRun, target *jev.Target, body []byte) error {
	output := TestStreamOutput{}
	result, err := (jev.Executor{}).Execute(run.Context, upstream.AttemptInput{Protocol: protocol.ProtocolSystemOne, Body: body, Target: target}, nil)
	if err != nil {
		if run.Context.Err() != nil {
			return run.Context.Err()
		}
		return output.Error(run, "SystemOne test failed: "+err.Error())
	}
	output.SendEvent(run, provider.TestEvent{Type: "content", Text: string(result.MediaBody)})
	// Completion reports elapsed upstream execution in milliseconds alongside the
	// separately validated usage, so unknown billing evidence still permits visible answers.
	data := map[string]any{"duration_ms": result.Duration.Milliseconds(), "usage_valid": result.HasUsage, "response": json.RawMessage(result.MediaBody)}
	output.SendEvent(run, provider.TestEvent{Type: "test_complete", Model: result.UpstreamResponseModel, Success: true, Data: data})
	return nil
}
