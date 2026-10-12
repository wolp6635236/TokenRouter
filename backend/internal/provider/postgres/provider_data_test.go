package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	dbent "github.com/TokenFlux/TokenRouter/ent"
)

// TestProviderEntityProjectionIsolatesNestedCredentials 检查 Ent 行转换出的记录持有独立凭据副本，修改副本后源行保持不变。
func TestProviderEntityProjectionIsolatesNestedCredentials(t *testing.T) {
	entity := &dbent.Provider{ID: 1, Credentials: map[string]any{"extension": map[string]any{"value": "original"}}}
	projected := RecordFromEntity(entity)
	copy, ok := projected.Credentials["extension"].(map[string]any)
	require.True(t, ok)
	copy["value"] = "modified"
	original, ok := entity.Credentials["extension"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "original", original["value"])
}
