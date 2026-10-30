package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// system_usage_transport_test.go exercises real local HTTP responses through
// the internal accounting transport. Fixtures verify trailing usage collection,
// protocol cache normalization, independent attempt identities, scope exclusion,
// and automatic spending limits without contacting a paid model provider.

// systemUsageHTTP sends fixture requests through a local server's HTTP client.
type systemUsageHTTP struct {
	client *http.Client // local fixture HTTP client
}

// Do uses the local fixture client to execute the supplied request. Proxy and
// provider parameters are ignored; callers close any returned response body
// after reading the fixture data or completing their assertions.
func (s systemUsageHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return s.client.Do(req)
}

// DoWithTLS preserves the transport interface while using the same local client.
// TLS-profile arguments are unused in these HTTP fixtures, and callers retain
// ownership of every returned response body until closure.
func (s systemUsageHTTP) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxy, id, concurrency)
}

// TestSystemUsageTransportProtocols sends each supported accounting envelope
// through an actual local HTTP exchange. It verifies disjoint token buckets,
// explicit missing usage, independent attempts, and idempotent response closure.
func TestSystemUsageTransportProtocols(t *testing.T) {
	for _, test := range []struct {
		name     string // fixture scenario identity
		platform string // provider protocol family
		path     string // model endpoint path
		body     string // local upstream response
		stream   bool   // request streaming flag
		input    int    // expected uncached input tokens
		output   int    // expected output tokens
		cache    int    // expected reused input tokens
		state    string // expected usage completeness
	}{
		{"responses", "openai", "/v1/responses", "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":3,\"input_tokens_details\":{\"cached_tokens\":4}}}}\n\n", true, 8, 3, 4, "complete"},
		{"chat trailer", "openai", "/v1/chat/completions", "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n", true, 12, 3, 0, "complete"},
		{"messages", "anthropic", "/v1/messages", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":0,\"cache_read_input_tokens\":4}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\ndata: {\"type\":\"message_stop\"}\n\n", true, 8, 3, 4, "complete"},
		{"gemini", "gemini", "/v1/models/test:streamGenerateContent", "data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"cachedContentTokenCount\":4,\"candidatesTokenCount\":2,\"thoughtsTokenCount\":1}}\n\n", true, 8, 3, 4, "complete"},
		{"messages fast", "anthropic", "/v1/messages", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":3,\"speed\":\"fast\"}}}\n\ndata: {\"type\":\"message_stop\"}\n\n", true, 8, 3, 0, "complete"},
		{"bedrock", "anthropic", "/model/test/invoke", `{"type":"message","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":8,"output_tokens":3,"cache_read_input_tokens":4}}`, false, 8, 3, 4, "complete"},
		{"systemone", "jev", "/v1/systemone", `{"model":"test","answers":{},"usage":{"input_tokens":8,"output_tokens":3}}`, false, 8, 3, 0, "complete"},
		{"image", "openai", "/v1/images/generations", `{"data":[{"b64_json":"fixture"}],"usage":{"input_tokens":8,"output_tokens":3,"output_tokens_details":{"image_tokens":3}}}`, false, 8, 3, 0, "complete"},
		{"compact", "openai", "/v1/responses/compact", `{"output":[],"usage":{"input_tokens":8,"output_tokens":3}}`, false, 8, 3, 0, "complete"},
		{"negative usage", "openai", "/v1/responses", "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":-1,\"output_tokens\":3}}}\n\n", true, 0, 0, 0, "missing"},
		{"partial", "anthropic", "/v1/messages", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":0}}}\n\n", true, 8, 0, 0, "partial"},
		{"vertex messages", "anthropic", "/v1/projects/test/locations/global/publishers/anthropic/models/test:streamRawPredict", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":3}}}\n\ndata: {\"type\":\"message_stop\"}\n\n", true, 8, 3, 0, "complete"},
		{"qoder", "qoder", "/v1/chat/completions", "data: {\"body\":\"{\\\"usage\\\":{\\\"prompt_tokens\\\":8,\\\"completion_tokens\\\":3}}\"}\n\ndata: {\"body\":\"[DONE]\"}\n\n", true, 8, 3, 0, "complete"},
		{"untrusted metadata", "openai", "/v1/responses", "data: {\"type\":\"response.completed\",\"response\":{\"model\":{\"text\":\"confidential-marker\"},\"service_tier\":{\"text\":\"confidential-marker\"},\"usage\":{\"input_tokens\":8,\"output_tokens\":3}}}\n\n", true, 8, 3, 0, "complete"},
		{"json error for stream", "openai", "/v1/responses", `{"error":{"message":"rejected"},"usage":{"input_tokens":8,"output_tokens":0}}`, true, 8, 0, 0, "complete"},
		{"missing", "openai", "/v1/responses", "data: {\"type\":\"response.completed\",\"response\":{}}\n\n", true, 0, 0, 0, "missing"},
		{"explicit zero", "openai", "/v1/responses", "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n", true, 0, 0, 0, "complete"},
		{"chat cache write", "openai", "/v1/chat/completions", `{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"cache_creation_input_tokens":4}}`, false, 8, 3, 0, "complete"},
		{"responses cache write", "openai", "/v1/responses", `{"output":[],"usage":{"input_tokens":12,"output_tokens":3,"cache_creation_input_tokens":4}}`, false, 8, 3, 0, "complete"},
		{"messages error cache", "anthropic", "/v1/messages", `{"error":{},"usage":{"input_tokens":8,"output_tokens":3,"cache_read_input_tokens":4}}`, false, 8, 3, 4, "complete"},
		{"sparse chat usage", "openai", "/v1/chat/completions", "data: {\"usage\":{\"prompt_tokens\":12,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\ndata: {\"usage\":{\"completion_tokens\":3}}\n\ndata: [DONE]\n\n", true, 8, 3, 4, "complete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				if test.name == "json error for stream" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
				}
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			var rows []*usage.UsageLog
			transport := &SystemUsageTransport{Base: systemUsageHTTP{server.Client()}, Read: func(context.Context, int64) (*provider.Record, error) {
				return &provider.Record{ID: 7, Platform: test.platform, Type: provider.ProviderTypeAPIKey}, nil
			}, Recorder: &usage.SystemRecorder{Save: func(_ context.Context, row *usage.UsageLog, _ string) error { rows = append(rows, row); return nil }}}
			ctx := usage.WithSystemRequest(t.Context(), usage.SystemRequest{Source: usage.SourceProviderTest, ParentRequestID: "run", RequestedModel: "requested-alias", MappedModel: "group-alias"})
			for range 2 {
				requestBody := `{"model":"test"}`
				if test.stream {
					requestBody = `{"model":"test","stream":true}`
				}
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+test.path, strings.NewReader(requestBody))
				require.NoError(t, err)
				response, err := transport.Do(request, "", 7, 1)
				require.NoError(t, err)
				// Closing an otherwise unread successful reply exercises the accounting
				// drain used when preview parsers stop before a usage-only trailer.
				require.NoError(t, response.Body.Close())
				require.NoError(t, response.Body.Close())
			}
			require.Len(t, rows, 2)
			require.NotEqual(t, rows[0].RequestID, rows[1].RequestID)
			for _, row := range rows {
				require.Equal(t, "requested-alias", row.Model)
				require.Equal(t, "requested-alias", row.RequestedModel)
				require.Equal(t, "test", *row.UpstreamModel)
				require.Equal(t, "group-alias", row.SystemUsage.MappedModel)
				require.Equal(t, test.input, row.InputTokens)
				require.Equal(t, test.output, row.OutputTokens)
				require.Equal(t, test.cache, row.CacheReadTokens)
				require.Equal(t, test.state, row.SystemUsage.UsageState)
				if strings.HasSuffix(test.name, "cache write") {
					require.Equal(t, 4, row.CacheCreationTokens)
				}
				if test.name == "json error for stream" {
					require.Equal(t, "failed", row.SystemUsage.Outcome)
				}
				if test.name == "untrusted metadata" {
					require.Nil(t, row.UpstreamResponseModel)
					require.Nil(t, row.ServiceTier)
					encoded, err := json.Marshal(row)
					require.NoError(t, err)
					require.NotContains(t, string(encoded), "confidential-marker")
				}
				if test.name == "messages fast" {
					require.NotNil(t, row.ServiceTier)
					require.Equal(t, "priority", *row.ServiceTier)
				}
				require.Equal(t, "run", row.SystemUsage.ParentRequestID)
				require.Zero(t, row.UserID)
				require.Zero(t, row.APIKeyID)
				require.Zero(t, row.ActualCost)
			}
		})
	}
}

// TestSystemUsageTransportSpendingAdmission exhausts a provider's local limit
// before automatic work. It verifies no automatic exchange occurs, while manual
// diagnostics remain callable and non-model requests do not produce expense rows.
func TestSystemUsageTransportSpendingAdmission(t *testing.T) {
	calls, records := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; _, _ = io.WriteString(w, `{}`) }))
	t.Cleanup(server.Close)
	transport := &SystemUsageTransport{Base: systemUsageHTTP{server.Client()}, Read: func(context.Context, int64) (*provider.Record, error) {
		return &provider.Record{ID: 7, Platform: provider.PlatformOpenAI, Type: provider.ProviderTypeAPIKey, Extra: map[string]any{"quota_limit": 1.0, "quota_used": 1.0}}, nil
	}, Recorder: &usage.SystemRecorder{Save: func(context.Context, *usage.UsageLog, string) error { records++; return nil }}}
	for _, automatic := range []bool{true, false} {
		ctx := usage.WithSystemRequest(t.Context(), usage.SystemRequest{Source: usage.SourceProviderTest, ParentRequestID: "run", Automatic: automatic})
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"test"}`))
		require.NoError(t, err)
		response, err := transport.Do(request, "", 7, 1)
		if automatic {
			require.ErrorContains(t, err, "spending limit")
			require.Zero(t, calls)
		} else {
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
		}
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/v1/models", nil)
	require.NoError(t, err)
	response, err := transport.Do(request, "", 7, 1)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 2, calls)
	require.Equal(t, 1, records)
}

// TestSystemUsageRecordingUsesLifecycle submits one completed response into a
// deferred application task. Closing the preview must not wait for accounting,
// and later cancellation must not erase the task's already observed usage.
func TestSystemUsageRecordingUsesLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":3}}}\n\n")
	}))
	t.Cleanup(server.Close)
	var pending func()
	writes := 0
	operator := int64(41)
	transport := &SystemUsageTransport{Base: systemUsageHTTP{server.Client()}, Read: func(context.Context, int64) (*provider.Record, error) {
		return &provider.Record{ID: 7, Platform: provider.PlatformOpenAI, Type: provider.ProviderTypeAPIKey}, nil
	}, Background: func(fn func()) bool { pending = fn; return true }, Recorder: &usage.SystemRecorder{Save: func(ctx context.Context, row *usage.UsageLog, _ string) error {
		require.NoError(t, ctx.Err())
		require.Equal(t, 8, row.InputTokens)
		require.NotNil(t, row.SystemUsage.OperatorUserID)
		require.Equal(t, int64(41), *row.SystemUsage.OperatorUserID)
		writes++
		return nil
	}}}
	ctx, cancel := context.WithCancel(usage.WithSystemRequest(t.Context(), usage.SystemRequest{Source: usage.SourceProviderTest, ParentRequestID: "run", OperatorUserID: &operator}))
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"test"}`))
	require.NoError(t, err)
	response, err := transport.Do(request, "", 7, 1)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Zero(t, writes)
	require.NotNil(t, pending)
	cancel()
	operator = 99
	pending()
	require.Equal(t, 1, writes)
}
