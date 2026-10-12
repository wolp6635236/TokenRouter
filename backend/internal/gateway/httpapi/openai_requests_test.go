package httpapi

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/egress"
)

func TestOpenAIValidateUpstreamBaseURLDisabledRequiresHTTPS(t *testing.T) {
	cfg := &responsesFixtureOptions{Request: OpenAIRequestOptions{URLPolicy: egress.OperatorURLPolicy{Enabled: false}}}
	svc := newResponsesFixture(responsesFixtureInputs{options: cfg})

	if _, err := svc.Requests.ValidateBaseURL("http://not-https.example.com"); err == nil {
		t.Fatalf("expected http to be rejected when allow_insecure_http is false")
	}
	normalized, err := svc.Requests.ValidateBaseURL("https://example.com")
	if err != nil {
		t.Fatalf("expected https to be allowed when allowlist disabled, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected raw url passthrough, got %q", normalized)
	}
}

func TestOpenAIValidateUpstreamBaseURLDisabledAllowsHTTP(t *testing.T) {
	cfg := &responsesFixtureOptions{Request: OpenAIRequestOptions{URLPolicy: egress.OperatorURLPolicy{
		Enabled:           false,
		AllowInsecureHTTP: true,
	}}}
	svc := newResponsesFixture(responsesFixtureInputs{options: cfg})

	normalized, err := svc.Requests.ValidateBaseURL("http://not-https.example.com")
	if err != nil {
		t.Fatalf("expected http allowed when allow_insecure_http is true, got %v", err)
	}
	if normalized != "http://not-https.example.com" {
		t.Fatalf("expected raw url passthrough, got %q", normalized)
	}
}

func TestOpenAIValidateUpstreamBaseURLEnabledEnforcesAllowlist(t *testing.T) {
	cfg := &responsesFixtureOptions{Request: OpenAIRequestOptions{URLPolicy: egress.OperatorURLPolicy{
		Enabled:       true,
		UpstreamHosts: []string{"example.com"},
	}}}
	svc := newResponsesFixture(responsesFixtureInputs{options: cfg})

	if _, err := svc.Requests.ValidateBaseURL("https://example.com"); err != nil {
		t.Fatalf("expected allowlisted host to pass, got %v", err)
	}
	if _, err := svc.Requests.ValidateBaseURL("https://evil.com"); err == nil {
		t.Fatalf("expected non-allowlisted host to fail")
	}
}
