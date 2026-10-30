package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// management_operations_test.go checks model synchronization permissions against
// their public argument schemas. Saved-provider discovery accepts a record ID,
// while temporary destinations and credentials belong to the separately authorized
// preview operation, keeping synchronization permission tied to saved settings.

// TestModelSyncOperationCannotOverrideStoredCredentials exercises the public MCP
// schema for the saved-provider operation. Caller-provided destinations, mode changes,
// and keys fail validation before the operation can invoke its HTTP handler.
func TestModelSyncOperationCannotOverrideStoredCredentials(t *testing.T) {
	operation := ManagementOperations["providers.models.sync_upstream.create"]
	require.NoError(t, operation.Validate(map[string]any{"path": map[string]any{"id": "44"}}))
	require.NotContains(t, operation.Input.Properties, "body")
	for _, body := range []map[string]any{
		{"base_url": "https://collector.example"},
		{"provider_mode": "go"},
		{"api_key": "temporary-key"},
	} {
		require.Error(t, operation.Validate(map[string]any{"path": map[string]any{"id": "44"}, "body": body}))
	}
	preview := ManagementOperations["providers.models.sync_upstream_preview.create"]
	require.NotEqual(t, operation.ID, preview.ID)
	require.NoError(t, preview.Validate(map[string]any{"body": map[string]any{"platform": "opencode", "type": "apikey", "provider_mode": "go", "api_key": "temporary-key", "base_url": "https://preview.example"}}))
	require.Error(t, preview.Validate(map[string]any{"body": map[string]any{"platform": "opencode", "type": "apikey", "base_url": "https://preview.example"}}))
}
