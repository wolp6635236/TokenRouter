package provider

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func TestClassifyOpenAIAPIKeyHealthFailureExclusions(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		eligible bool
	}{
		{name: "provider attributed 502", err: &forwardcore.UpstreamFailoverError{StatusCode: http.StatusBadGateway}, eligible: true},
		{name: "request scoped capacity", err: &forwardcore.UpstreamFailoverError{StatusCode: 529, RequestScopedTransient: true}},
		{name: "provider scoped overload", err: &forwardcore.UpstreamFailoverError{StatusCode: 529, Scope: forwardcore.GatewayFailureScopeShared}},
		{name: "dedicated same provider retry", err: &forwardcore.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, RetryableOnSameProvider: true}},
		{name: "credential disable path", err: &forwardcore.UpstreamFailoverError{StatusCode: http.StatusUnauthorized, Stage: forwardcore.GatewayFailureStageProviderAuth, Scope: forwardcore.GatewayFailureScopeProvider}},
		{name: "client request", err: &forwardcore.UpstreamFailoverError{StatusCode: http.StatusBadRequest}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, eligible := ClassifyOpenAIAPIKeyHealthFailure(tt.err)
			require.Equal(t, tt.eligible, eligible)
		})
	}
}

func TestOpenAI429RetryDelayHonorsBoundedRetryAfter(t *testing.T) {
	deadline := time.Now().Add(providercore.RuntimeRetryWindow)
	require.Equal(t, openAIOAuth429RetryDelay, OpenAI429RetryDelay(nil, deadline))
	require.Equal(t, openAIOAuth429MaxRetryDelay, OpenAI429RetryDelay(http.Header{"Retry-After": []string{"90"}}, deadline))
}
