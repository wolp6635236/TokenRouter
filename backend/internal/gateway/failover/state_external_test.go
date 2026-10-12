package failover_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/failover"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
)

func TestOpenAIFirstOutputFailoverStopsAfterOneProviderSwitch(t *testing.T) {
	failoverErr := &forwardcore.UpstreamFailoverError{SafeToFailoverAfterWrite: true}
	count := 0

	require.False(t, failover.FirstOutputExhausted(failoverErr.SafeToFailoverAfterWrite, &count))
	require.Equal(t, 1, count)
	require.True(t, failover.FirstOutputExhausted(failoverErr.SafeToFailoverAfterWrite, &count))
	require.Equal(t, 1, count)
}
