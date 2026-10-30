package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// text_apikey_test_execution.go constructs and executes fixed-protocol Kimi,
// Zhipu, and DeepSeek API-key probes with a mapped test model. Kimi, Zhipu, and
// DeepSeek API-key probes derive a platform default model when the supplied
// name is blank, then apply one provider mapping. The selected group route or
// record protocol selects Messages, Responses, or Chat request construction and
// matching stream parsing, with allowed header overrides applied after
// authentication.

// Execute runs a fixed native-protocol probe for an API-key record. The
// platform target supplies defaultModel for a blank request model; one
// provider mapping follows that selection. Missing keys or models fail before
// an upstream request.
//
// The selected operation resolves its own endpoint and sampling attributes.
// Messages, Responses, and Chat use their existing payloads and stream
// readers, while platform continuation policy controls Responses storage
// fields. The executor owns response-body cleanup and preserves the original
// run's cancellation and event sink.
func (s *NativeProtocolTest) Execute(
	c *TestRun,
	value *providercore.Record,
	modelID string,
	prompt string,
	defaultModel string,
) error {
	if value == nil || value.Type != capability.ProviderTypeAPIKey {
		return (TestStreamOutput{}).Error(c, "Text protocol tests require an API Key provider")
	}
	apiKey := strings.TrimSpace(value.GetCredential("api_key"))
	if apiKey == "" {
		return (TestStreamOutput{}).Error(c, "No API key available")
	}
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = defaultModel
	}
	testModelID = mappedTestModel(value, testModelID)
	if testModelID == "" {
		return (TestStreamOutput{}).Error(c, "No test model available")
	}

	ctx := c.Context
	protocol := (providercore.ProtocolTarget{Record: value, Protocol: c.ProtocolRoute.UpstreamProtocol}).GetAPIProtocol()
	if c.ProtocolRoute.ClientProtocol != "" {
		if _, err := resolveProbeType(ctx, s.Attributes, value, c.GroupID, testModelID, string(c.ProtocolRoute.UpstreamProtocol), providercore.ProviderTestTypeText); err != nil {
			return (TestStreamOutput{}).Error(c, err.Error())
		}
	}
	var (
		apiURL  string
		payload any
	)
	switch protocol {
	case providercore.APIProtocolAnthropic:
		baseURL, err := s.ValidateURL((providercore.ProtocolTarget{Record: value}).GetAnthropicProtocolBaseURL())
		if err != nil {
			return (TestStreamOutput{}).Error(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
		}
		if hint := messagesBaseURLMisconfigHint(baseURL); hint != "" {
			return (TestStreamOutput{}).Error(c, hint)
		}
		apiURL = httpclient.BuildOpenAIEndpointURL(baseURL, "/v1/messages")
		payload, err = claude.TestPayloadWithPrompt(testModelID, prompt)
		if err != nil {
			return (TestStreamOutput{}).Error(c, "Failed to create test payload")
		}
	case providercore.APIProtocolResponses:
		baseURL, err := s.ValidateURL((providercore.ProtocolTarget{Record: value, Protocol: c.ProtocolRoute.UpstreamProtocol}).GetOpenAIBaseURL())
		if err != nil {
			return (TestStreamOutput{}).Error(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
		}
		apiURL = responsesProbeURL(value.Platform, baseURL)
		responsesPayload := openai.TestResponsesPayload(testModelID, prompt, false)
		// Kimi and DeepSeek Responses probes are stateless connection checks.
		// Disable response storage in the outgoing native payload before it
		// reaches the platform endpoint.
		responsesPayload["store"] = false
		payload = responsesPayload
	default:
		baseURL, err := s.ValidateURL((providercore.ProtocolTarget{Record: value, Protocol: c.ProtocolRoute.UpstreamProtocol}).GetOpenAIBaseURL())
		if err != nil {
			return (TestStreamOutput{}).Error(c, fmt.Sprintf("Invalid base URL: %s", err.Error()))
		}
		apiURL = httpclient.BuildOpenAIEndpointURL(baseURL, "/v1/chat/completions")
		payload = openai.TestChatCompletionsPayload(testModelID, prompt)
	}
	if object, ok := payload.(map[string]any); ok {
		if err := applyProbeSampling(c, s.Attributes, value, testModelID, string(c.ProtocolRoute.UpstreamProtocol), object); err != nil {
			return (TestStreamOutput{}).Error(c, err.Error())
		}
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return (TestStreamOutput{}).Error(c, "Failed to create test payload")
	}

	c.Begin(true)
	(TestStreamOutput{}).SendEvent(c, providercore.TestEvent{Type: "test_start", Model: testModelID})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return (TestStreamOutput{}).Error(c, "Failed to create request")
	}
	req = req.WithContext(upstream.WithHTTPUpstreamProfile(req.Context(), upstream.HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if protocol == providercore.APIProtocolAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
		claude.SetAPIKeyAuthHeader(req.Header, value.GetAnthropicAPIKeyAuthScheme() == providercore.AnthropicAPIKeyAuthSchemeAuthorizationBearer, apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	ApplyProviderHeaderOverrides(value, req.Header)

	resp, err := s.send(req, value)
	if err != nil {
		return (TestStreamOutput{}).Error(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Keep available non-OK response bytes for probe diagnostics even if reading
		// fails. The selected endpoint's status remains the reported failure.
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))
		if (protocol == providercore.APIProtocolAnthropic && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden)) && s.Store != nil {
			// Record native Messages authentication rejection when storage is
			// configured. A failed state write does not replace the upstream failure
			// already being returned by the probe.
			_ = s.Store.SetError(ctx, value.ID, errMsg)
		}
		return (TestStreamOutput{}).Error(c, errMsg)
	}
	switch protocol {
	case providercore.APIProtocolAnthropic:
		return (TestStreamOutput{}).Anthropic(c, resp.Body)
	case providercore.APIProtocolResponses:
		return (TestStreamOutput{}).Responses(c, resp.Body)
	default:
		return (TestStreamOutput{}).ChatCompletions(c, resp.Body)
	}
}

// executeChat delegates adaptive Chat coverage to the fixed Execute path. That
// path supplies model defaults, authentication, allowed header overrides,
// proxy, and TLS options; adaptive callers suppress its intermediate completion
// event.
func (s *NativeProtocolTest) executeChat(
	c *TestRun,
	value *providercore.Record,
	modelID string,
	prompt string,
	defaultModel string,
) error {
	return s.Execute(c, value, modelID, prompt, defaultModel)
}

// responsesProbeURL returns the platform-specific Responses endpoint under the
// supplied root. DeepSeek uses the unversioned responses suffix, while other
// platforms use v1; endpoint construction retains supported version and path
// prefixes.
func responsesProbeURL(platform, base string) string {
	return httpclient.BuildOpenAIEndpointURL(base, providercore.ResponsesOperationPath(platform))
}
