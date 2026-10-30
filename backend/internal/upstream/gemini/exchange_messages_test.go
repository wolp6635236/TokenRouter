package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// TestExchangeCancellationStopsBackoff exercises all response representations
// after a transport failure. Cancelling the shared request must end the wait
// promptly and prevent another dispatch within the remaining local budget.
func TestExchangeCancellationStopsBackoff(t *testing.T) {
	for name, exchange := range map[string]func(context.Context, ExchangeOptions) (ExchangeResult, error){
		"messages": ExchangeMessages, "native": ExchangeNative, "openai": ExchangeOpenAI,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			options := exchangeTestOptions()
			options.MaxRetries = 5
			options.Do = func(*http.Request) (*http.Response, error) {
				calls++
				// Schedule cancellation after the exchange enters its wait, exercising
				// interruption rather than only its check before request construction.
				time.AfterFunc(10*time.Millisecond, cancel)
				return nil, io.ErrUnexpectedEOF
			}
			started := time.Now()
			_, err := exchange(ctx, options)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 1, calls)
			require.Less(t, time.Since(started), 500*time.Millisecond)
		})
	}
}

// TestExternalRetryRetainsBoundedSignatureRepair rejects two Messages payloads
// with signature errors before an ordinary third-request server failure. External
// ordinary retry ownership must retain both protocol repair stages without
// granting additional retries for an unrelated server failure.
func TestExternalRetryRetainsBoundedSignatureRepair(t *testing.T) {
	options := exchangeTestOptions()
	options.ExternalRetry = true
	options.MaxRetries = 5
	options.ShouldRetry = func(int) bool { return true }
	calls, replacements := 0, 0
	options.Do = func(*http.Request) (*http.Response, error) {
		calls++
		status, body := 400, `{"error":{"message":"thought_signature invalid"}}`
		if calls == 3 {
			status, body = 503, `{"error":{"message":"busy"}}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	options.FilterThinking = func() []byte { return []byte(`{}`) }
	options.FilterTools = func() []byte { return []byte(`{}`) }
	options.ConvertRetry = func(body []byte) ([]byte, error) { return body, nil }
	options.ReplaceBody = func([]byte) { replacements++ }
	result, err := ExchangeMessages(context.Background(), options)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, result.Response.Body.Close())
	}()
	require.Equal(t, 3, calls)
	require.Equal(t, 2, replacements)
	require.Equal(t, 503, result.Response.StatusCode)
}

// exchangeTestOptions supplies deterministic error readers and inert callbacks
// for exchange tests. Individual cases replace transport and retry inputs;
// returned response bodies remain owned by the test invoking the exchange.
func exchangeTestOptions() ExchangeOptions {
	return ExchangeOptions{
		Build: func(ctx context.Context) (*http.Request, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.invalid", nil)
			return req, "x-request-id", err
		},
		Sanitize: func(value string) string { return value },
		Observe:  func(ExchangeNotice) {},
		SetError: func(int, string, string) {},
		Message:  func(body []byte) string { return string(body) },
		Detail:   func(body []byte) string { return string(body) },
		ReadError: func(resp *http.Response) []byte {
			body, _ := io.ReadAll(resp.Body)
			return body
		},
		CheckPolicy: func(_ context.Context, resp *http.Response) (bool, *http.Response) { return false, resp },
	}
}

// TestExchangeTransportDeadlineKeepsCallerLive models an upstream header timeout
// while the request context remains active. All representations retry Google
// exchanges and classify external retry failures through the caller's mapper.
func TestExchangeTransportDeadlineKeepsCallerLive(t *testing.T) {
	for name, exchange := range map[string]func(context.Context, ExchangeOptions) (ExchangeResult, error){
		"messages": ExchangeMessages, "native": ExchangeNative, "openai": ExchangeOpenAI,
	} {
		for _, external := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/external=%t", name, external), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx := context.Background()
					calls, observed := 0, 0
					mapped := errors.New("classified transport failure")
					options := exchangeTestOptions()
					options.MaxRetries = 2
					options.ExternalRetry = external
					options.Do = func(*http.Request) (*http.Response, error) {
						calls++
						return nil, fmt.Errorf("response header timeout: %w", context.DeadlineExceeded)
					}
					options.Observe = func(ExchangeNotice) { observed++ }
					options.FinalError = func(message string) error {
						require.Contains(t, message, "response header timeout")
						return mapped
					}
					_, err := exchange(ctx, options)
					require.ErrorIs(t, err, mapped)
					require.NoError(t, ctx.Err())
					expected := 2
					if external {
						expected = 1
					}
					require.Equal(t, expected, calls)
					require.Equal(t, expected, observed)
				})
			})
		}
	}
}
