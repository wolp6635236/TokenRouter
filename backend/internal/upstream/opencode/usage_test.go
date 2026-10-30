package opencode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/upstream/usagecontract"
)

// TestGoUsageQueryAuthenticatesAndPreservesWindows uses a local HTTP server to
// exercise the fixed path, bearer credential, and all three returned windows.
// Overshoot remains observable and its displayed remainder stops at zero.
func TestGoUsageQueryAuthenticatesAndPreservesWindows(t *testing.T) {
	payload := `{"usage":{"rolling":{"status":"ok","percent":0,"resetsAt":"2026-10-26T10:00:00Z"},"weekly":{"status":"rate-limited","percent":110,"resetsAt":"2026-10-28T10:00:00Z"},"monthly":{"status":"ok","percent":25,"resetsAt":"2026-11-01T10:00:00Z"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/zen/go/v1/usage", r.URL.Path)
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	input := &usagecontract.Request{BaseURL: server.URL, APIKey: "test", Do: server.Client().Do, Context: func(ctx context.Context) context.Context { return ctx }, ApplyHeaders: func(http.Header) {}}
	result, err := (&GoUsageAdapter{}).Query(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, result.Limits, 3)
	require.Equal(t, 0.0, *result.Limits[0].Used)
	require.Equal(t, 110.0, *result.Limits[1].Used)
	require.Equal(t, 0.0, *result.Limits[1].Remaining)
	require.Equal(t, "rate-limited", result.Limits[1].Status)
	for _, broken := range []string{`{}`, strings.Replace(payload, `"percent":0`, `"percent":null`, 1), strings.Replace(payload, `"percent":0`, `"percent":-1`, 1), strings.Replace(payload, "2026-11-01T10:00:00Z", "bad", 1)} {
		_, err := ParseGoUsage([]byte(broken))
		require.Error(t, err)
	}
}

// TestGoUsageQueryRejectsStatusAndCancellation feeds unsuccessful HTTP responses
// and a canceled operation through the real adapter. Neither case can be
// interpreted as a successfully observed zero-use allowance.
func TestGoUsageQueryRejectsStatusAndCancellation(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			input := &usagecontract.Request{BaseURL: "https://opencode.ai", APIKey: "test", Context: func(ctx context.Context) context.Context { return ctx }, ApplyHeaders: func(http.Header) {}, Do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}}
			_, err := (&GoUsageAdapter{}).Query(t.Context(), input)
			require.Error(t, err)
		})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	input := &usagecontract.Request{BaseURL: "https://opencode.ai", APIKey: "test", Context: func(ctx context.Context) context.Context { return ctx }, ApplyHeaders: func(http.Header) {}, Do: func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() }}
	_, err := (&GoUsageAdapter{}).Query(ctx, input)
	require.Error(t, err)
}
