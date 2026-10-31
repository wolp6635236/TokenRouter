package mediaentry

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/systemone"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	wire "github.com/TokenFlux/TokenRouter/internal/protocol/systemone"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// A SystemOne pool retry stays on one selected provider and holds its lease
// through the successful answer. This scenario joins HTTP classification with
// the media adapter and decision loop, observing two exchanges, one completion
// capture and one final lease release.

// systemOnePoolTransport scripts an initial service error followed by served
// decisions.
type systemOnePoolTransport struct {
	httpclient.UpstreamTransport               // unused transport operations
	calls                        int           // serial exchange call count
	bodies                       [][]byte      // captured request payload copies
	headers                      []http.Header // captured outbound header copies
}

// systemOnePoolRun counts completion and lease release around the real HTTP
// adapter.
type systemOnePoolRun struct {
	*systemOneRun // real decision adapter
	// completed and released are counted by callbacks in the same serial Run.
	// The test inspects them after execution returns to distinguish completion
	// capture from final provider-lease release.
	// completed: captured completion call count; released: provider release call count.
	completed, released int
	// fallback replaces the original selection after that provider is excluded.
	// Nil leaves the original same-provider fixture behavior available to older tests.
	fallback *gatewayprovider.ExecutionProvider
}

// Do returns HTTP 503 for the first fixture exchange and a valid decision for
// every later call. It captures payloads and headers for serial assertions,
// creates a fresh response body, and increments its unsynchronized call count.
func (p *systemOnePoolTransport) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	p.bodies = append(p.bodies, payload)
	p.headers = append(p.headers, req.Header.Clone())
	p.calls++
	status := http.StatusOK
	body := `{"model":"jev-1.13.0","answers":{"available":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":0}}`
	if p.calls == 1 {
		status = http.StatusServiceUnavailable
		body = `{"error":{"message":"busy"}}`
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

// Select returns the installed provider unless its exclusion selects a fallback.
// The optional fallback lets image scenarios exercise provider replacement;
// fixtures without it retain the original selected provider for pool retries.
func (p *systemOnePoolRun) Select(_ context.Context, excluded map[int64]struct{}) (provider.ProviderSnapshot, error) {
	if _, found := excluded[p.selection.Provider.Record.ID]; found && p.fallback != nil {
		p.selection = &gatewayprovider.SelectionResult{Provider: p.fallback}
	}
	return p.selection.Provider.Record.RoutingSnapshot(), nil
}

// Acquire supplies one fixture release callback and reports successful
// admission. The callback increments the serial release counter, letting the
// scenario inspect release after both pool exchanges have finished.
func (p *systemOnePoolRun) Acquire(context.Context) (func(), bool) {
	return func() { p.released++ }, true
}

// Report accepts fixture scheduling feedback while leaving its counters
// unchanged.
func (p *systemOnePoolRun) Report(context.Context, systemone.Outcome) {}

// Switch accepts fixture provider-switch bookkeeping without a scheduler binding.
// Selection and exclusions remain controlled by the real decision loop, while
// this fixture observes transport calls and lease release rather than scheduler counters.
func (p *systemOnePoolRun) Switch() {}

// Complete increments the serial count of captured served decision
// completions.
func (p *systemOnePoolRun) Complete(context.Context, upstream.AttemptResult) { p.completed++ }

// TestSystemOnePoolRetryExecution returns a retryable 503 followed by a valid
// decision with no provider-switch allowance. It checks two exchanges share
// one lease, one completion and the delivered vendor-version answer.
func TestSystemOnePoolRetryExecution(t *testing.T) {
	transport := &systemOnePoolTransport{}
	prices := testkit.ResolverWithCards(t, testkit.Calculator(nil, map[string]*pricing.ModelPricing{"jev-latest": {InputPricePerToken: 4.2e-8}}), nil)
	executor := &gatewayhttp.SystemOneExecutor{Transport: transport, Pricing: &admission.ModelPricing{Resolver: prices}}
	runtime := New(Bindings{CheckFunding: func(context.Context, *apikey.APIKey, *billing.UserSubscription, string, bool) error { return nil }, Platform: PlatformPorts{SystemOne: executor.Forward}})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, gatewayhttp.EndpointSystemOne, nil)
	key := &apikey.APIKey{ID: 1, User: &apikey.User{ID: 1}}
	c.Set(string(keyhttp.ContextKeyAPIKey), key)
	selected := gatewayprovider.NewExecutionProvider(&provider.Record{ID: 1, Platform: provider.PlatformJev, Type: provider.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "test", "pool_mode": true, "pool_mode_retry_count": 2, "pool_mode_retry_status_codes": []any{503}}})
	run := &systemOnePoolRun{systemOneRun: &systemOneRun{h: runtime, c: c, key: key, input: gatewayhttp.AuxiliaryHTTPInput{Model: "jev-latest", Body: []byte(`{"model":"jev-latest","state":"available","questions":{"available":{"type":"noul","instructions":"Available?"}}}`)}, selection: &gatewayprovider.SelectionResult{Provider: selected}}}
	require.NoError(t, systemone.Run(c.Request.Context(), 0, run))
	require.Equal(t, 2, transport.calls)
	require.Equal(t, 1, run.completed)
	require.Equal(t, 1, run.released)
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"model":"jev-1.13.0"`)
}

// TestSystemOneMultipartRetryMapping exercises group and provider model mappings
// across pool retries and provider switches. Both attempts retain the original
// image bytes and opt-in, and only the final served answer qualifies for completion.
func TestSystemOneMultipartRetryMapping(t *testing.T) {
	var original bytes.Buffer
	writer := multipart.NewWriter(&original)
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="request"`}, "Content-Type": {"application/json"}})
	require.NoError(t, err)
	_, err = io.WriteString(part, `{"model":"client-alias","state":"<photo>","questions":{"available":{"type":"noul","instructions":"Available?"}},"assets":{"<photo>":{"part":"photo"}}}`)
	require.NoError(t, err)
	part, err = writer.CreateFormFile("photo", "input.jpg")
	require.NoError(t, err)
	image := []byte{255, 216, 255, 0, 128, 5}
	_, err = part.Write(image)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	for _, pool := range []bool{true, false} {
		transport := &systemOnePoolTransport{}
		prices := testkit.ResolverWithCards(t, testkit.Calculator(nil, map[string]*pricing.ModelPricing{"jev-latest": {InputPricePerToken: 4.2e-8}}), nil)
		executor := &gatewayhttp.SystemOneExecutor{Transport: transport, Pricing: &admission.ModelPricing{Resolver: prices}}
		runtime := New(Bindings{CheckFunding: func(context.Context, *apikey.APIKey, *billing.UserSubscription, string, bool) error { return nil }, Platform: PlatformPorts{SystemOne: executor.Forward}})
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, gatewayhttp.EndpointSystemOne, nil)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		c.Request.Header.Set(wire.ImagesHeader, "true")
		key := &apikey.APIKey{ID: 1, User: &apikey.User{ID: 1}}
		c.Set(string(keyhttp.ContextKeyAPIKey), key)
		selected := gatewayprovider.NewExecutionProvider(&provider.Record{ID: 1, Platform: provider.PlatformJev, Type: provider.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "test", "pool_mode": pool, "pool_mode_retry_count": 1, "pool_mode_retry_status_codes": []any{503}, "model_mapping": map[string]any{"jev-latest": "provider-one"}}})
		fallback := gatewayprovider.NewExecutionProvider(&provider.Record{ID: 2, Platform: provider.PlatformJev, Type: provider.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "test", "model_mapping": map[string]any{"jev-latest": "provider-two"}}})
		run := &systemOnePoolRun{systemOneRun: &systemOneRun{h: runtime, c: c, key: key, input: gatewayhttp.AuxiliaryHTTPInput{Model: "client-alias", Mapping: routing.GroupMappingResult{Mapped: true, MappedModel: "jev-latest"}, Body: original.Bytes()}, selection: &gatewayprovider.SelectionResult{Provider: selected}}, fallback: fallback}
		require.NoError(t, systemone.Run(c.Request.Context(), 1, run))
		require.Equal(t, 2, transport.calls)
		require.Equal(t, 1, run.completed)
		for index, payload := range transport.bodies {
			parsed, err := wire.ParsePayload(payload, transport.headers[index].Get("Content-Type"))
			require.NoError(t, err)
			want := "provider-one"
			if !pool && index == 1 {
				want = "provider-two"
			}
			require.Equal(t, want, parsed.Model)
			require.Equal(t, "true", transport.headers[index].Get(wire.ImagesHeader))
			require.True(t, bytes.Contains(payload, image))
		}
		parsed, err := wire.ParsePayload(original.Bytes(), writer.FormDataContentType())
		require.NoError(t, err)
		require.Equal(t, "client-alias", parsed.Model)
	}
}
