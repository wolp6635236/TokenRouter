package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func TestEnsureQoderCNMachineCredentialsRemovesLegacyFields(t *testing.T) {
	provider := &providercore.Record{Credentials: map[string]any{
		"site":                 "cn",
		"security_oauth_token": "cosy-token",
		"machine_id":           "machine-cn",
		"machine_token":        "legacy-machine-token",
		"machine_type":         "legacy-machine-type",
	}}

	ensureQoderMachineCredentials(provider)

	require.Equal(t, "machine-cn", provider.GetCredential("machine_id"))
	require.NotContains(t, provider.Credentials, "machine_token")
	require.NotContains(t, provider.Credentials, "machine_type")
}
