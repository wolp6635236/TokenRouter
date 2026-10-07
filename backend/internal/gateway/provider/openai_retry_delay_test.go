package provider

import (
	"net/http"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestOpenAI429RetryDelayHonorsBoundedRetryAfter(t *testing.T) {
	deadline := time.Now().Add(providercore.RuntimeRetryWindow)
	require.Equal(t, openAIOAuth429RetryDelay, OpenAI429RetryDelay(nil, deadline))
	require.Equal(t, openAIOAuth429MaxRetryDelay, OpenAI429RetryDelay(http.Header{"Retry-After": []string{"90"}}, deadline))
}
