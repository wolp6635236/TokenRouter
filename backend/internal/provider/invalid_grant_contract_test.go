package provider_test

import (
	"errors"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/stretchr/testify/require"
)

func TestIsInvalidGrantError(t *testing.T) {
	require.True(t, providercore.IsInvalidGrantError(errors.New("invalid_grant: token revoked")))
	require.True(t, providercore.IsInvalidGrantError(errors.New("INVALID_GRANT")))
	require.False(t, providercore.IsInvalidGrantError(errors.New("invalid_client")))
	require.False(t, providercore.IsInvalidGrantError(nil))
}
