package forward

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamFailoverErrorNextProviderActionPreservesLegacyRetry(t *testing.T) {
	t.Parallel()

	require.True(t, (&UpstreamFailoverError{}).ShouldRetryNextProvider())
	require.True(t, (&UpstreamFailoverError{NextProviderAction: NextProviderRetry}).ShouldRetryNextProvider())
	require.False(t, (&UpstreamFailoverError{NextProviderAction: NextProviderStop}).ShouldRetryNextProvider())
}
