package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// management_operations_test.go checks model configuration fields exposed to MCP.
// Management operations derive input schemas from their HTTP body types, so
// references must remain discoverable in create, update, and draft preview
// schemas whenever the corresponding HTTP field is accepted.

// TestReferenceManagementSchemas exposes reference_model to MCP configuration writers.
// Price and attribute operations derive their schemas from HTTP request types,
// so clients can discover and submit the same model reference field.
func TestReferenceManagementSchemas(t *testing.T) {
	for _, id := range []string{"pricing.configs.create", "pricing.configs.update", "model_attributes.configs.create", "model_attributes.configs.update", "model_attributes.preview.create"} {
		body, err := json.Marshal(ManagementOperations[id].Input)
		require.NoError(t, err)
		require.Contains(t, string(body), `"reference_model"`, id)
	}
}
